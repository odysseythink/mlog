# zlog-Style Performance Optimization for mlog (v2)

**Status**: Design Review v2
**Date**: 2026-04-29
**Changes from v1**: Sequence-based RingBuffer, Error/Fatal ordering protocol, multi-sink architecture, backpressure & shutdown semantics, benchmark targets, testing strategy.

---

## 1. Background

mlog is a Go logging library modeled after Google's internal C++ glog. It already has several performance optimizations (`sync.Pool`, hand-written header formatting, 256KB `bufio` buffer, background flush daemon). However, under high concurrency, the single `sync.Mutex` on `fileSink` becomes a bottleneck, and there are allocation hotspots on the write path.

This design 移植 zlog (C high-performance logging library) 的核心优化思路，采用双层缓冲 + 批量提交架构，对 mlog 进行综合性能优化。

### 1.1 Design Goals

| Goal | Target |
|---|---|
| Single-thread throughput | ≥ 3× current (baseline ~500k entries/sec → target ≥ 1.5M entries/sec) |
| 64-goroutine concurrent throughput | ≥ 8× current (baseline ~600k → target ≥ 5M entries/sec) |
| p99 latency under 64-goroutine load | ≤ 5 µs (current: ~80 µs due to lock contention) |
| p999 latency | ≤ 50 µs |
| Allocations per Info call | ≤ 1 alloc/op, ≤ 64 B/op (current: 4 allocs, ~280 B) |
| Public API compatibility | Zero changes to `Info()`, `Error()`, `Fatal()`, etc. |
| Log ordering | Strong: timestamp order preserved across all severities |
| Crash durability | Error/Fatal: zero loss; Info/Warning: bounded loss (ring depth) |

### 1.2 Non-Goals

- Replacing the file rotation logic in `sync_buffer.go`.
- Changing the on-disk log format (keeps glog format for tooling compatibility).
- Per-goroutine local buffers (rejected: unbounded memory, complicates ordering).

---

## 2. Architecture

### 2.1 Current Write Path

```
goroutine → ctxlogf() → textPrintf() → fileSink.Emit(mu.Lock) → syncBuffer.Write → bufio → file
```

All goroutines contend on `fileSink.mu`. Allocation hotspots: header buffer, message format buffer, caller info string parsing.

### 2.2 New Write Path

```
                                                  ┌──> INFO ring  ──> writer-info  ──> bufio ──> file.INFO
goroutine ─> ctxlogf ─> textPrintf ─> dispatch ───┼──> WARN ring  ──> writer-warn  ──> bufio ──> file.WARNING
                                                  ├──> ERROR ring ──> writer-error ──> bufio ──> file.ERROR
                                                  └──> FATAL: drain all rings synchronously, then write+sync+exit
```

Each severity gets its own ring buffer and writer goroutine. Producers are lock-free (atomic CAS); the writer goroutine is the single consumer per ring (MPSC).

### 2.3 Ordering Guarantees

- **Within a single severity**: strict FIFO (ring buffer is FIFO).
- **Across severities**: `glog` writes higher-severity entries to *all* lower-severity files (ERROR is also written to WARNING and INFO files). To preserve ordering within each per-severity file, the dispatcher publishes to all relevant rings *before* returning to the caller, in severity-ascending order. Writers consume independently. Within each file, order is preserved because each ring is FIFO and each writer drains its own ring sequentially.
- **Error/Fatal sync semantics**: see §3.2.3.

---

## 3. Components

### 3.1 RingBuffer (`ringbuffer.go`)

**Design**: LMAX Disruptor-style MPSC ring with per-slot sequence numbers. This replaces the v1 design (which had a publication race between "claim slot" and "data is visible").

#### 3.1.1 Slot layout

```go
// Each slot carries its own sequence counter. The producer's CAS on the
// global writePos only *claims* the slot; the slot is not visible to the
// consumer until the producer stores seq = pos+1 (publication).
type slot struct {
    seq   atomic.Uint64
    entry *logEntry
}

type ringBuffer struct {
    // Cache-line padding to prevent false sharing between writePos (hot for
    // producers) and readPos (hot for consumer). 64 bytes = typical x86_64
    // and ARM64 cache line.
    _pad0    [64]byte
    writePos atomic.Uint64
    _pad1    [56]byte  // 64 - sizeof(atomic.Uint64)
    readPos  atomic.Uint64
    _pad2    [56]byte

    slots []slot   // power-of-two length
    mask  uint64   // len(slots) - 1, for cheap modulo
    cap   uint64

    // Drop accounting
    dropped atomic.Uint64

    // Shutdown signal: closed → producers fail-fast, consumer drains
    closed atomic.Bool
}
```

#### 3.1.2 Producer protocol (multi-producer)

```go
// Returns false if ring is full (in Drop mode) or closed.
// In Block mode, callers wrap this in a backoff loop with timeout (§3.5).
func (r *ringBuffer) tryPush(e *logEntry) bool {
    for {
        if r.closed.Load() {
            return false
        }
        wp := r.writePos.Load()
        rp := r.readPos.Load()
        if wp-rp >= r.cap {
            return false  // full
        }
        if r.writePos.CompareAndSwap(wp, wp+1) {
            // We own slot wp. Publish:
            s := &r.slots[wp&r.mask]
            s.entry = e
            s.seq.Store(wp + 1)  // release: consumer waits for this
            return true
        }
        // CAS lost; retry
    }
}
```

Key invariant: **`seq.Store(wp+1)` is the publication point.** The consumer treats a slot as ready only when `seq == expected_pos + 1`. This eliminates the v1 race where `writePos` advanced before data was written.

`atomic.Store` on amd64/arm64 provides release semantics for the prior `s.entry = e` write under the Go memory model.

#### 3.1.3 Consumer protocol (single consumer)

```go
// Pops up to maxBatch entries into out. Returns count drained.
// Spins briefly if the next slot is claimed but not yet published.
func (r *ringBuffer) drainBatch(out []*logEntry, maxBatch int) int {
    rp := r.readPos.Load()
    n := 0
    for n < maxBatch {
        s := &r.slots[rp&r.mask]
        // Wait for publication. In practice this is rarely contended;
        // if it is, the producer is mid-write and we spin a few ns.
        seq := s.seq.Load()
        expected := rp + 1
        if seq != expected {
            // Either nothing published yet (seq < expected) or we wrapped
            // past it (shouldn't happen if cap is correct).
            if seq < expected {
                // Try a brief spin; if still not ready, return what we have.
                if !spinWaitSeq(&s.seq, expected, 64) {
                    break
                }
            } else {
                break
            }
        }
        out[n] = s.entry
        s.entry = nil  // help GC, allow pool reuse of the entry struct
        n++
        rp++
    }
    if n > 0 {
        r.readPos.Store(rp)
    }
    return n
}

func spinWaitSeq(seq *atomic.Uint64, expected uint64, maxIter int) bool {
    for i := 0; i < maxIter; i++ {
        if seq.Load() == expected {
            return true
        }
        runtime.Gosched()  // or `procyield` via assembly for ultra-low latency
    }
    return false
}
```

#### 3.1.4 Why Disruptor over channel?

Go channels would be the obvious alternative. We reject them because:

- `chan *logEntry` with buffer N requires a `runtime.lock`/select scheduler involvement on send and recv; benchmarks show ~80–120 ns/op overhead vs. ~15–25 ns/op for the Disruptor design.
- Channel send copies the pointer through internal buffers; we want the producer's allocation to land directly in the slot.
- Batching is easier with the Disruptor (drain N at once with a single `readPos` update).

#### 3.1.5 Configuration

| Flag | Default | Range | Notes |
|---|---|---|---|
| `-log_ring_size` | 4096 per severity | power of 2, [256, 1<<20] | Validated at init; non-pow2 rounds up. |
| `-log_drop_policy` | `block` | `block` \| `drop` | See §3.5. |
| `-log_block_timeout_ms` | 100 | [0, 60000] | Block mode only; 0 = wait forever. |

### 3.2 BatchWriter + Writer Goroutine (`async_writer.go`)

```go
type batchWriter struct {
    severity Severity
    ring     *ringBuffer
    sink     *fileSink         // owns *bufio.Writer + *syncBuffer
    wakeCh   chan struct{}     // size 1, non-blocking signal
    flushReq chan chan error   // manual Flush() requests
    done     chan struct{}     // signals goroutine exited
    batch    []*logEntry       // reusable scratch
    stats    *writerStats
}
```

#### 3.2.1 Wake protocol

v1 used an unbuffered channel, which blocks the producer until the consumer receives. We replace it with a **size-1 buffered channel + non-blocking send**:

```go
// Producer signals after pushing to ring:
select {
case bw.wakeCh <- struct{}{}:
default: // already a wake pending; fine
}

// Consumer loop:
for {
    n := bw.ring.drainBatch(bw.batch, batchSize)
    if n == 0 {
        // Adaptive idle: spin briefly, then park on wakeCh.
        if !bw.adaptiveSleep() {
            return
        }
        continue
    }
    bw.flushBatch(bw.batch[:n])
}
```

`adaptiveSleep` spins ~256 iterations of `runtime.Gosched()` (covers bursty traffic without context-switching), then blocks on `wakeCh` with a `time.NewTimer(periodicFlushInterval)` to enforce the 30s periodic flush.

#### 3.2.2 Flush policy

| Severity / Trigger | Action |
|---|---|
| Drained batch contains ERROR | flush bufio after batch write |
| Drained batch contains FATAL | flush + `f.Sync()` (durability) |
| Periodic timer (30s default) | flush bufio (no fsync) |
| Manual `mlog.Flush()` | send on `flushReq`, writer flushes all sinks, replies on the response channel |
| Bufio buffer ≥ 192 KB (75% of 256 KB) | flush proactively to avoid blocking on next write |

#### 3.2.3 Error/Fatal ordering protocol (NEW)

v1 said "Error/Fatal bypass the ring buffer and write synchronously." This **breaks ordering** because an INFO published earlier could land in the file *after* a later ERROR.

**Revised protocol**:

- **ERROR**: producer pushes to the ERROR ring like normal, then sends on `wakeCh`, then *blocks on a per-call `done` channel* attached to the entry. The writer marks the entry done after the bufio flush. Net effect: ERROR is async-published (ordering preserved per-ring) but the *caller* synchronizes on durable visibility.

  ```go
  type logEntry struct {
      data []byte
      meta LogsinkMeta
      // For ERROR/FATAL: optional ack channel. nil for INFO/WARNING.
      ack  chan struct{}
  }
  ```

  Cost: ERROR adds ~2–10 µs (one wake + one batch flush). Acceptable — ERROR is rare by definition.

- **FATAL**: producer takes a global `fatalMu`, then for each severity ring in ascending order: closes the ring, signals wake, waits for `done`. Each writer drains everything remaining and exits. Then producer writes the FATAL entry+stack synchronously to ALL severity files, calls `Sync()`, and `os.Exit(255)`. This guarantees:
  1. Every entry published before `Fatal()` is durably flushed.
  2. The FATAL entry itself is fsync'd before exit.
  3. Stack dump is in every file just like glog today.

- **Crash before fsync**: only the un-flushed bufio + un-drained ring contents are lost. Documented loss bound: `ring_size × num_severities × avg_entry_size + 256KB × num_severities ≈ 1–2 MB worst case` per process.

### 3.3 Buffer Pool Enhancement (`logsink.go`)

Replace `sync.Pool` of `*bytes.Buffer` with two pools: a `[]byte` slab pool and a `logEntry` struct pool.

```go
const (
    defaultEntryBufSize = 512   // covers ~95% of entries (header+message)
    maxPooledEntryBuf   = 8192  // discard above this to prevent slab bloat
)

var entryBufPool = sync.Pool{
    New: func() any {
        b := make([]byte, 0, defaultEntryBufSize)
        return &b
    },
}

var logEntryPool = sync.Pool{
    New: func() any { return &logEntry{} },
}

func putEntryBuf(p *[]byte) {
    if cap(*p) > maxPooledEntryBuf {
        return  // let GC reclaim oversized buffers
    }
    *p = (*p)[:0]
    entryBufPool.Put(p)
}
```

Why pre-sized 512: header is ~80–120 bytes and the median KBZ log message is ~250 bytes (measured from current production logs), so 512 covers the common case without growth.

The pool is consumed by the writer goroutine: after `bufio.Write(entry.data)` returns (data is copied into the bufio buffer), the writer calls `putEntryBuf(&entry.data)` and `logEntryPool.Put(entry)`.

### 3.4 Caller Cache (`mlog.go`)

v1 used `runtime.Caller` + cached `runtime.FuncForPC`. We can do better: use `runtime.Callers` (faster — no string parsing) and cache the entire `(file, line, funcname)` triple keyed by PC.

```go
type callerInfo struct {
    file     string
    funcname string
    line     int
}

// PC → *callerInfo. sync.Map suits read-heavy workload after warmup.
var callerCache sync.Map

func getCallerInfo(skip int) (file string, line int, funcname string) {
    var pcs [1]uintptr
    n := runtime.Callers(skip+1, pcs[:])
    if n == 0 {
        return "???", 0, "???"
    }
    pc := pcs[0]
    if v, ok := callerCache.Load(pc); ok {
        ci := v.(*callerInfo)
        return ci.file, ci.line, ci.funcname
    }
    // Cold path: resolve via CallersFrames, cache result.
    frames := runtime.CallersFrames(pcs[:n])
    fr, _ := frames.Next()
    ci := &callerInfo{
        file:     trimSrcPath(fr.File),
        line:     fr.Line,
        funcname: trimFuncName(fr.Function),
    }
    callerCache.Store(pc, ci)
    return ci.file, ci.line, ci.funcname
}
```

`runtime.Callers` is roughly 3–5× faster than `runtime.Caller` because it skips frame resolution; resolution happens once per unique PC and is cached.

**Memory bound**: a typical service has ≤ 5,000 unique `mlog.X(...)` call sites. At ~120 B per `callerInfo` plus map overhead, ~1 MB cap. No expiration needed (PCs are stable per process).

### 3.5 Backpressure & Drop Policy (NEW)

When the ring is full, behavior is configurable:

| Mode | Producer behavior | Use case |
|---|---|---|
| `block` (default) | Spin briefly (256 iter), then `runtime.Gosched()` loop with a timeout. After timeout: increment `dropped`, write a sentinel record, return. | Production: keep ordering, accept brief stalls. |
| `drop` | Increment `dropped` immediately, return. | Latency-critical paths where occasional log loss is acceptable. |

Block mode is bounded by `-log_block_timeout_ms` (default 100ms). After timeout, the entry is *dropped* (not lost silently): the dispatcher emits a `[mlog: dropped N entries due to back-pressure]` record into the next successful write, with N being the count since last successful write. This is a pattern borrowed from `zerolog`.

#### 3.5.1 High-watermark metric

Each ring exports:
- `ring_size` — capacity
- `ring_used` — `writePos - readPos` (sampled, not exact)
- `ring_dropped_total` — counter
- `ring_block_wait_ns_p99` — histogram of block durations

If `ring_used > 0.7 × ring_size` for ≥ 5s, the writer logs a one-shot warning into the file: `WARNING: log ring (severity=INFO) high watermark sustained, consider increasing -log_ring_size`. Rate-limited to once per 5 minutes per severity.

### 3.6 Sampler / Rate Limiter (`sampler.go`)

(Substantively unchanged from v1; only minor edits for clarity.)

```go
type sampler struct {
    tokens     atomic.Int64
    maxTokens  int64
    refillRate int64  // tokens per second
    lastRefill atomic.Int64  // unix nanos
}

func (s *sampler) allow() bool {
    if s == nil {
        return true  // sampler disabled, zero overhead
    }
    s.refill()
    for {
        cur := s.tokens.Load()
        if cur <= 0 {
            return false
        }
        if s.tokens.CompareAndSwap(cur, cur-1) {
            return true
        }
    }
}
```

- Per-severity policy: ERROR/FATAL bypass; DEBUG/INFO/WARNING subject to limit.
- Disabled by default (`-log_rate_limit=0`).
- Drop count surfaces in `Stats` and the same backpressure summary record.

### 3.7 Multi-Sink Architecture (NEW)

glog writes to multiple files: each severity level S has a file containing all entries of severity ≥ S. So an ERROR entry appears in `INFO`, `WARNING`, and `ERROR` files.

Implementation:

```go
type fileSinkSet struct {
    rings   [numSeverities]*ringBuffer  // one per severity *file*
    writers [numSeverities]*batchWriter
    sinks   [numSeverities]*fileSink
}

func (fss *fileSinkSet) Emit(meta LogsinkMeta, data []byte) {
    e := acquireEntry(data, meta)
    // Publish into rings for all severity files at meta.Severity and below.
    // Ascending order so the lowest-severity file (INFO) is always the last
    // ring touched — gives slowest sink a head start on any backpressure.
    for s := SeverityInfo; s <= meta.Severity; s++ {
        fss.publishTo(s, e)
    }
    if meta.Severity >= SeverityError {
        e.waitAck()  // §3.2.3
    }
}
```

Note: an entry pushed into N rings has its `[]byte` aliased N times. The pool return logic must use a **reference counter on the entry**:

```go
type logEntry struct {
    data    []byte
    meta    LogsinkMeta
    ack     chan struct{}
    refCnt  atomic.Int32  // = number of rings this entry was pushed to
}

// Writer side, after flushing:
if e.refCnt.Add(-1) == 0 {
    putEntryBuf(&e.data)
    if e.ack != nil { close(e.ack) }
    logEntryPool.Put(e)
}
```

`stderrSink` is unchanged (synchronous, mutex-protected, low volume).

---

## 4. File Changes

### 4.1 New Files

| File | Content |
|---|---|
| `ringbuffer.go` | `ringBuffer`, `slot`, producer/consumer protocols |
| `ringbuffer_test.go` | Unit tests, MPSC race tests, benchmarks |
| `async_writer.go` | `batchWriter`, writer goroutine, wake/flush logic |
| `async_writer_test.go` | Async write tests, ordering tests, Fatal path tests |
| `sampler.go` | Token bucket rate limiter |
| `sampler_test.go` | Sampler tests |
| `caller_cache.go` | `getCallerInfo` and PC cache |
| `caller_cache_test.go` | Caller cache benchmarks |
| `metrics.go` | Ring/writer stats accessors for the `Stats()` API |

### 4.2 Modified Files

| File | Change |
|---|---|
| `mlog_file.go` | `fileSink.Emit` → `fileSinkSet.Emit`; replace `flushDaemon` with writer goroutines; add `Close()` and `FatalShutdown()` paths |
| `mlog.go` | `ctxlogf` caller info → `getCallerInfo`; refcount entry on multi-publish |
| `logsink.go` | `bufs` Pool → `entryBufPool` with size cap; `textPrintf` writes into pooled `[]byte` |
| `constants.go` | Add `defaultRingSize=4096`, `defaultBatchSize=64`, `defaultEntryBufSize=512`, `maxPooledEntryBuf=8192`, `periodicFlushInterval=30s`, `bufioHighWaterMark=192*1024` |
| `mlog_flags.go` | Add `-log_ring_size`, `-log_batch_size`, `-log_drop_policy`, `-log_block_timeout_ms`, `-log_rate_limit` |

### 4.3 Unchanged Files

- `sync_buffer.go` — file creation, rotation, bufio wrapper unchanged.
- `stackdump.go` — unrelated.
- All public API (`Info`, `Error`, `Fatal`, `V`, etc.) — zero changes.

---

## 5. Testing Strategy (NEW)

### 5.1 Correctness

- **MPSC race tests** with `-race -count=1000`: 16 producers × 1 consumer × 1M entries each. Verify: no missing entries, no duplicate entries, FIFO order per producer.
- **Publication race specific test**: producer A claims slot N, sleeps before storing seq; producer B claims slot N+1, completes; consumer must NOT advance past N. Use `runtime.Gosched()` / channel barriers to force the interleaving deterministically.
- **Wraparound test**: ring_size=4, push 10000 entries with random consumer pacing.
- **Close-during-publish**: close ring while producers are mid-CAS; assert no panic, accurate `dropped` count.
- **Fatal path test**: spawn 8 goroutines logging INFO at full rate, one calls `Fatal`; assert (a) no INFO published before Fatal is missing from disk, (b) Fatal appears in INFO/WARNING/ERROR files, (c) process exits 255, (d) files are fsync'd (verified via `syncfs` mock).
- **Ordering test**: 4 goroutines log timestamped entries at INFO/WARN/ERROR mix. After flush, parse files and assert each per-severity file is monotonic in timestamp. Cross-severity ordering checked against the producer's local ordering.

### 5.2 Performance

Benchmarks live in `*_test.go` and run on the project's `6×RTX 4090` host (ignoring GPUs; CPU is AMD EPYC). Each benchmark reports `ns/op`, `B/op`, `allocs/op` and tail latency.

| Benchmark | Concurrency | Rate target |
|---|---|---|
| `BenchmarkInfo_Single` | 1 | ≤ 700 ns/op, ≤ 1 alloc/op |
| `BenchmarkInfo_Parallel_8` | 8 | ≤ 250 ns/op |
| `BenchmarkInfo_Parallel_64` | 64 | ≤ 200 ns/op, p99 ≤ 5 µs |
| `BenchmarkInfo_Parallel_512` | 512 | ≤ 400 ns/op, p999 ≤ 100 µs |
| `BenchmarkError_Single` | 1 | ≤ 5 µs/op (sync ack) |
| `BenchmarkVerbose_Disabled` | 64 | ≤ 5 ns/op (V(1) check fastpath) |

Comparison baselines: current mlog, `zap` (sugared and structured), `zerolog`. We expect to be within 1.5× of `zerolog` parallel throughput while keeping glog format and API.

### 5.3 Soak / Chaos

- 24-hour soak at 200k entries/sec mixed severity. Watch RSS, goroutine count, ring high-watermark histogram. Pass: RSS stable ±5%, no goroutine leak, no dropped entries with default config.
- Disk-stall injection (`fsync` blocked for 5s): assert block-mode timeout fires correctly, dropped entries are recorded, process recovers cleanly when disk responds.

---

## 6. Migration Strategy

Four phases, each independently testable and deployable.

### 6.1 Phase 1 — Caller Cache + Buffer Pool (independent)

Pure optimization, no architectural change. Can ship standalone before Phase 2 design is finalized.

- Replace `runtime.Caller` with `runtime.Callers` + PC cache.
- Replace `bytes.Buffer` pool with sized `[]byte` pool + size cap on return.
- **Expected gain**: 20–30% on single-thread `Info`, ~50% reduction in allocs/op.
- **Risk**: low. Pure refactor, behavior-preserving.

### 6.2 Phase 2 — Per-Severity RingBuffer + Async Writer

Core architectural change.

- Implement `ringBuffer` (Disruptor protocol).
- Implement `batchWriter` and writer goroutine.
- Implement `fileSinkSet` multi-sink fan-out.
- Implement Error sync-ack and Fatal drain-and-exit protocols.
- Implement backpressure (block timeout + drop counter + high-watermark warning).
- **Expected gain**: 5–10× parallel throughput, p99 latency drops by ~10×.
- **Risk**: high. Requires the full §5.1 test suite to pass before merge.

Trade-off acknowledged: ring entries unconsumed at crash time are lost (bounded by ring depth). ERROR/FATAL bypass this loss via sync-ack and drain protocols. INFO/WARNING worst-case loss = `ring_size × entry_size + bufio_buf` per severity (~1–2 MB total, default config).

### 6.3 Phase 3 — Sampler / Rate Limiter

Independent feature, opt-in via `-log_rate_limit > 0`. Zero overhead when disabled (nil check).

### 6.4 Phase 4 — Observability

Expose ring/writer metrics via the existing `Stats()` API and a new `/debug/mlog` HTTP handler (gated by build tag).

Each phase ships with before/after benchmark comparison and a 24-hour staging soak.

---

## 7. Open Questions

1. **Should DEBUG be sampled by default?** KBZ services emit ~5–10× more DEBUG than INFO. Default `-log_rate_limit=10000/s` for DEBUG only would give natural protection. Awaiting input from ops.
2. **Per-CPU sharded rings?** A future optimization: shard the INFO ring across `GOMAXPROCS` to reduce CAS contention further. Deferred — Disruptor at 4096 slots already handles 64-thread workloads well per benchmarks; revisit if 256+ thread profiles show CAS hotspots.
3. **Structured logging path?** Out of scope here. If/when added, structured entries can reuse the same ring; only the formatter changes.

---

## 8. Summary of Changes from v1

| Area | v1 | v2 |
|---|---|---|
| RingBuffer publication | `writePos` advance only | Sequence-per-slot Disruptor protocol; eliminates publish race |
| Cache layout | One struct, no padding | 64-byte padded `writePos` / `readPos` to prevent false sharing |
| Position width | `uint32` | `uint64` (no 32-bit wraparound risk) |
| Error/Fatal | Bypass ring (broke ordering) | Async-ack for ERROR; coordinated drain for FATAL — ordering preserved |
| Wake mechanism | Unbuffered chan (blocks producer) | Size-1 buffered chan + non-blocking send + adaptive spin |
| Caller info | `runtime.Caller` + cached `FuncForPC` | `runtime.Callers` + full triple cached |
| Buffer pool | No size cap (slab bloat risk) | Discard returns > 8 KB |
| Multi-sink | Not addressed | Explicit per-severity rings + refcounted entries |
| Backpressure | Block forever or drop | Block with timeout + dropped-entry summary record + high-watermark warning |
| Fatal shutdown | Not specified | Drain all rings, write to all files, fsync, exit |
| Benchmarks | "before/after comparison" | Concrete targets per concurrency level |
| Testing | "thorough testing" | Race tests, publication race, wraparound, ordering, soak — enumerated |
