# Phase 4: Structured Logging + Zero-Alloc Hot Path Design

## Goal

Add structured logging capabilities to mlog with zero-allocation hot path, pluggable encoders (JSON/logfmt/text), and type-safe API. Existing API (Infof/Errorf/etc.) remains unchanged. New API coexists alongside.

## Background

mlog currently uses `fmt.Sprintf` formatting with `...any` variadic args, causing interface boxing on every call. Zap demonstrates that tagged union `Field` types + pooled `Entry` structs achieve near-zero allocation. This phase brings that capability to mlog without breaking the existing glog-style API.

## Architecture Overview

```
旧 API (Infof/Errorf/...)
  -> textPrintf (hand-written header + fmt.Fprintf)
  -> TextSink.Emit(meta, []byte)
  -> ring buffer (logEntry{data: []byte})
  -> async writer -> bufio -> file

新 API (S().Info/Error/...)
  -> structuredLog(severity, msg, fields)
  -> StructuredSink.Emit(meta, *Entry)
  -> ring buffer (logEntry{entry: *Entry})
  -> async writer -> encoder.EncodeEntry(entry) -> []byte -> bufio -> file
```

Two paths share the same ring buffer + async writer + file sink infrastructure.

## Core Types

### Field (tagged union, zero boxing)

```go
type FieldType uint8

const (
    FieldTypeUnknown FieldType = iota
    FieldTypeInt64
    FieldTypeFloat64
    FieldTypeString
    FieldTypeBool
    FieldTypeDuration
    FieldTypeErr
    FieldTypeAny
)

type Field struct {
    Key       string
    Type      FieldType
    Integer   int64    // int64, bool(0/1), duration(nanos), float64(bits)
    String    string
    Interface any      // only FieldTypeErr and FieldTypeAny
}
```

Constructor functions (all inline):

- `Int(key string, val int) Field` — stores in Integer
- `Int64(key string, val int64) Field` — stores in Integer
- `Float64(key string, val float64) Field` — stores via `math.Float64bits` in Integer
- `String(key, val string) Field` — stores in String, zero alloc
- `Bool(key string, val bool) Field` — stores 0/1 in Integer
- `Duration(key string, val time.Duration) Field` — stores nanos in Integer
- `Err(err error) Field` — stores in Interface with key "error"
- `Any(key string, val any) Field` — stores in Interface (slow path, uses reflection)

### Entry

```go
type Entry struct {
    Severity Severity
    Time     time.Time
    Message  string
    Fields   []Field    // nil = no structured fields (old API path)
    File     string
    Line     int
    Funcname string
    Thread   int64
    Stack    *Stack
}
```

Pooled via `sync.Pool`. Fields slice pre-allocated with capacity 16, reused on return.

## Encoder Interface

```go
type Encoder interface {
    EncodeEntry(entry *Entry) []byte
    Clone() Encoder
}
```

### Built-in Encoders

1. **TextEncoder** (default) — Output matches existing textPrintf format exactly. Fields appended as `key=value` pairs in logfmt style after the message. When Fields is nil (old API), identical to current output.

2. **JSONEncoder** — JSON Lines output: `{"ts":"...","level":"info","caller":"main.go:42","msg":"...","key":42}`. Hand-written JSON assembly (no `encoding/json` overhead). Buffer from `sync.Pool`.

3. **LogfmtEncoder** — logfmt output: `ts=... level=info caller=main.go:42 msg=... key=42`.

### Encoder Selection

```go
var activeEncoder atomic.Value // stores Encoder instance

func SetEncoder(enc Encoder)
func getEncoder() Encoder // returns TextEncoder by default
```

Flag: `-log_encoder` with values `text` (default), `json`, `logfmt`.

## New API

```go
func S() *StructuredLogger  // entry point

type StructuredLogger struct {
    fields []Field  // pre-set fields from With()
}

func (s *StructuredLogger) Info(msg string, fields ...Field)
func (s *StructuredLogger) Warning(msg string, fields ...Field)
func (s *StructuredLogger) Error(msg string, fields ...Field)
func (s *StructuredLogger) Fatal(msg string, fields ...Field)
func (s *StructuredLogger) With(fields ...Field) *StructuredLogger
```

Usage:

```go
mlog.S().Info("request completed",
    mlog.String("method", "GET"),
    mlog.Int("status", 200),
    mlog.Duration("latency", elapsed),
)

reqLogger := mlog.S().With(mlog.String("request_id", id))
reqLogger.Info("handling request")
```

## Zero-Alloc Hot Path

Hot path allocations for `S().Info(msg, fields...)`:

1. `...Field` slice header — 1 allocation (unavoidable, 24 bytes)
2. Entry from pool — 0 alloc (pool hit)
3. Severity check — 0 alloc (atomic load)
4. Ring buffer push — 0 alloc (CAS)
5. Writer goroutine wake — 0 alloc (channel send, capacity 1)

Writer goroutine (off hot path):
1. `encoder.EncodeEntry()` — 0 alloc (buffer from pool)
2. `bufio.Write()` — 0 alloc (pre-allocated buffer)
3. Entry returned to pool — 0 alloc

## Integration with Existing Pipeline

### logEntry Extension

```go
type logEntry struct {
    data   []byte   // old API: pre-formatted text (non-nil)
    entry  *Entry   // new API: unencoded Entry (data is nil)
    meta   *LogsinkMeta
    ack    chan struct{}
    refCnt atomic.Int32
}
```

### asyncWriter.writeBatch() Modification

```go
func (bw *batchWriter) writeBatch(entries []*logEntry) {
    for _, e := range entries {
        var data []byte
        if e.data != nil {
            data = e.data // old API: already formatted
        } else if e.entry != nil {
            data = getEncoder().EncodeEntry(e.entry) // new API: encode now
        }
        bw.buf.Write(data)
        // ... existing refCount/ack logic unchanged
    }
}
```

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `field.go` | Field type, tagged union, constructors |
| `entry.go` | Entry struct, sync.Pool |
| `encoder.go` | Encoder interface, getEncoder(), SetEncoder() |
| `encoder_text.go` | TextEncoder implementation |
| `encoder_json.go` | JSONEncoder implementation |
| `encoder_logfmt.go` | LogfmtEncoder implementation |
| `structured.go` | StructuredLogger, S(), With(), structuredLog() |
| `field_test.go` | Field constructor tests |
| `encoder_test.go` | Encoder output format tests |
| `structured_test.go` | New API integration tests |
| `structured_bench_test.go` | Zero-alloc benchmarks |

### Modified Files

| File | Change |
|------|--------|
| `logsink.go` | logEntry gains `entry *Entry` field |
| `async_writer.go` | writeBatch() handles data vs entry path |
| `mlog_flags.go` | Add `-log_encoder` flag |
| `mlog.go` | Add `S()` function |

### Unchanged Files

`ringbuffer.go`, `mlog_file.go`, `sampler.go`, `sync_buffer.go`, `constants.go`, `ringbuffer_test.go`, `async_writer_test.go`, `sampler_test.go`.

## Performance Targets

| Metric | Target |
|--------|--------|
| S().Info hot path allocs | <= 1 (slice header only) |
| S().Info disabled level | 0 allocs (atomic load early exit) |
| JSONEncoder throughput | > 10M fields/sec |
| TextEncoder (new API) | within 5% of existing textPrintf |
| Zero overhead when not used | S() not called = no cost |

## Out of Scope (deferred to Phase 5/6)

- Core wrapper pattern (Tee, Hook, Filter) — Phase 5
- Per-message FNV hash sampling — Phase 6
- StructuredSink interface for external consumers
- Encoder registry (custom encoder registration)
- V-level support for structured API (V(2).S().Info(...))

## Dependencies

- Go 1.21+ (generics not required, atomic.Value sufficient)
- math.Float64bits for Float64 field encoding
- No external dependencies
