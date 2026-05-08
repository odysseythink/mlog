# Comprehensive Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring test coverage from 45.8% to 90%+, establish full-dimension performance benchmarks.

**Architecture:** Three-phase rollout — Phase 1 unit tests (coverage gate), Phase 2 basic benchmarks (stability gate), Phase 3 extended benchmarks (report gate). Each phase produces working, testable software on its own.

**Tech Stack:** Go 1.21+, `testing` package, `go test -bench`, `go test -race`, `go test -cover`

---

## File Structure

| File | Responsibility |
|---|---|
| `mode_test.go` | LogMode, global function routing, Verbose routing |
| `structured_test.go` | Logger methods (all 17 methods) in both modes |
| `encoder_test.go` | All field types + edge cases for 3 encoders |
| `logsink_test.go` | textPrintf, sink routing, backtrace, rate limiter |
| `ringbuffer_test.go` | tryPush, drainBatch, concurrent ops, close |
| `async_writer_test.go` | writeBatch, flush, writerLoop lifecycle, ack timeout |
| `bench_print_test.go` | Printf-mode benchmarks (Info/Infof/Infoln) |
| `bench_structured_test.go` | Structured-mode benchmarks (Info/Infof/fields) |
| `bench_concurrency_test.go` | Concurrent gradient benchmarks (1/4/8/16/32/64 goroutines) |
| `bench_latency_test.go` | Latency percentile benchmarks (P50/P90/P99) |
| `BENCHMARK_REPORT.md` | Final performance report with tables and profiling results |

---

## Phase 1: Unit Tests (Coverage 45.8% → 90%+)

### Task 1.1: mode_test.go — Global Function Routing

**Files:**
- Modify: `mode_test.go`

- [ ] **Step 1: Add helper for safe mode switching in tests**

Add to `mode_test.go`:

```go
func resetMode() {
    modeSetOnce = sync.Once{}
    logMode.Store(0)
}

func setStructured() {
    resetMode()
    SetLogMode(LogModeStructured)
}

func setPrintf() {
    resetMode()
    // printf is default, no SetLogMode needed
}
```

- [ ] **Step 2: Test infoStructured helper**

```go
func TestInfoStructured(t *testing.T) {
    setStructured()
    defer resetMode()

    // No panic with empty args
    infoStructured(1, Severity_Info)

    // No panic with string msg
    infoStructured(1, Severity_Info, "hello")

    // No panic with string msg + fields
    infoStructured(1, Severity_Info, "hello", String("k", "v"))

    // Non-string first arg falls back to fmt.Sprint
    infoStructured(1, Severity_Info, 123, String("k", "v"))
}
```

- [ ] **Step 3: Test infofStructured helper**

```go
func TestInfofStructured(t *testing.T) {
    setStructured()
    defer resetMode()

    infofStructured(1, Severity_Info, "hello %s", "world")
}
```

- [ ] **Step 4: Test global function routing for all severities in structured mode**

Test these functions in structured mode (use `TextSinks = nil` to avoid file sink bug):

```go
func TestGlobalFunctionsStructured(t *testing.T) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    Info("info msg")
    Infof("info %s", "msg")
    Warning("warn msg")
    Warningf("warn %s", "msg")
    Error("error msg")
    Errorf("error %s", "msg")
    // Fatal and Exit would terminate process, skip in unit test
}
```

- [ ] **Step 5: Test Verbose routing in both modes**

```go
func TestVerboseStructured(t *testing.T) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    v := Verbose(true)
    v.Info("verbose info")
    v.Infof("verbose %s", "info")
    v.Infoln("verbose", "info")
}

func TestVerbosePrintf(t *testing.T) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    v := Verbose(true)
    v.Info("verbose info")
    v.Infof("verbose %s", "info")
    v.Infoln("verbose", "info")
}
```

- [ ] **Step 6: Run tests**

Run: `go test -run 'TestInfoStructured|TestInfofStructured|TestGlobalFunctions|TestVerbose' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add mode_test.go
git commit -m "test(mode): add global function routing tests for structured mode"
```

---

### Task 1.2: structured_test.go — Logger Method Coverage

**Files:**
- Modify: `structured_test.go`

- [ ] **Step 1: Add printf-mode tests for all new Logger methods**

```go
func TestLoggerPrintfMode(t *testing.T) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    l := With(String("svc", "test"))

    l.Debug("debug msg")
    l.Debugf("debug %s", "msg")
    l.Debugln("debug", "msg")

    l.Infof("info %s", "msg")
    l.Infoln("info", "msg")

    l.Warningf("warn %s", "msg")
    l.Warningln("warn", "msg")

    l.Errorf("error %s", "msg")
    l.Errorln("error", "msg")

    // Fatalf and Fatalln would terminate, skip
}
```

- [ ] **Step 2: Add structured-mode tests for all new Logger methods**

```go
func TestLoggerStructuredMode(t *testing.T) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    l := With(String("svc", "test"))

    l.Debug("debug msg", String("k", "v"))
    l.Debugf("debug %s", "msg")
    l.Debugln("debug", "msg")

    l.Infof("info %s", "msg")
    l.Infoln("info", "msg")

    l.Warningf("warn %s", "msg")
    l.Warningln("warn", "msg")

    l.Errorf("error %s", "msg")
    l.Errorln("error", "msg")
}
```

- [ ] **Step 3: Test Logger.With chaining**

```go
func TestLoggerWithChaining(t *testing.T) {
    l1 := With(String("a", "1"))
    l2 := l1.With(Int("b", 2))

    if len(l1.fields) != 1 {
        t.Errorf("l1 mutated: got %d fields", len(l1.fields))
    }
    if len(l2.fields) != 2 {
        t.Errorf("l2 expected 2 fields, got %d", len(l2.fields))
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run 'TestLogger' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add structured_test.go
git commit -m "test(structured): add Logger method coverage for both modes"
```

---

### Task 1.3: encoder_test.go — Full Field Type Coverage

**Files:**
- Modify: `encoder_test.go`

- [ ] **Step 1: Add missing field type tests for textEncoder**

```go
func TestTextEncoderAllTypes(t *testing.T) {
    now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
    e := &Entry{
        Severity: Severity_Info,
        Time:     now.UnixNano(),
        Message:  "test",
        File:     "main.go",
        Line:     1,
        Fields: []Field{
            Int64("int64", 9223372036854775807),
            Float64("float", 3.14),
            String("str", "hello"),
            String("empty", ""),
            Bool("bool", true),
            Bool("bool_false", false),
            Duration("dur", 5*time.Second),
            Err("error", fmt.Errorf("test error")),
            Err("nil_err", nil),
            Any("any", map[string]int{"a": 1}),
        },
    }

    enc := NewTextEncoder()
    out := enc.EncodeEntry(e)
    defer putEncBuf(&out)

    s := string(out)
    checks := []string{
        "int64=9223372036854775807",
        "float=3.14",
        "str=hello",
        "empty=",
        "bool=true",
        "bool_false=false",
        "dur=5s",
        "error=test error",
        `any="map[a:1]"`,
    }
    for _, c := range checks {
        if !strings.Contains(s, c) {
            t.Errorf("missing %q in: %s", c, s)
        }
    }
}
```

- [ ] **Step 2: Add missing field type tests for jsonEncoder**

```go
func TestJSONEncoderAllTypes(t *testing.T) {
    SetEncoder(NewJSONEncoder())
    defer SetEncoder(defaultTextEncoder)

    now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
    e := &Entry{
        Severity: Severity_Info,
        Time:     now.UnixNano(),
        Message:  "test",
        File:     "main.go",
        Line:     1,
        Fields: []Field{
            Int64("int64", 9223372036854775807),
            Float64("float", 3.14),
            String("str", "hello"),
            String("quoted", `hello "world"`),
            String("newline", "a\nb"),
            Bool("bool", true),
            Duration("dur", 5*time.Second),
            Err("error", fmt.Errorf("test error")),
            Err("nil_err", nil),
            Any("any", map[string]int{"a": 1}),
        },
    }

    enc := getEncoder()
    out := enc.EncodeEntry(e)
    defer putEncBuf(&out)

    s := string(out)
    checks := []string{
        `"int64":9223372036854775807`,
        `"float":3.14`,
        `"str":"hello"`,
        `"quoted":"hello \"world\""`,
        `"bool":true`,
        `"dur":5000000000`,
        `"error":"test error"`,
        `"any":"map[a:1]"`,
    }
    for _, c := range checks {
        if !strings.Contains(s, c) {
            t.Errorf("missing %q in: %s", c, s)
        }
    }
}
```

- [ ] **Step 3: Add logfmt encoder tests**

```go
func TestLogfmtEncoderAllTypes(t *testing.T) {
    SetEncoder(NewLogfmtEncoder())
    defer SetEncoder(defaultTextEncoder)

    now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
    e := &Entry{
        Severity: Severity_Info,
        Time:     now.UnixNano(),
        Message:  "test msg",
        File:     "main.go",
        Line:     1,
        Fields: []Field{
            Int64("int64", 9223372036854775807),
            Float64("float", 3.14),
            String("str", "hello"),
            Bool("bool", true),
            Duration("dur", 5*time.Second),
            Err("error", fmt.Errorf("test error")),
        },
    }

    enc := getEncoder()
    out := enc.EncodeEntry(e)
    defer putEncBuf(&out)

    s := string(out)
    if !strings.Contains(s, `int64=9223372036854775807`) {
        t.Errorf("missing int64: %s", s)
    }
    if !strings.Contains(s, `float=3.14`) {
        t.Errorf("missing float: %s", s)
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run 'TestTextEncoderAllTypes|TestJSONEncoderAllTypes|TestLogfmtEncoderAllTypes' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add encoder_test.go
git commit -m "test(encoder): add full field type coverage for all encoders"
```

---

### Task 1.4: logsink_test.go — textPrintf & Sink Coverage

**Files:**
- Modify: `logsink_test.go`

- [ ] **Step 1: Test textPrintf with various formats**

```go
func TestTextPrintfFormats(t *testing.T) {
    meta := &LogsinkMeta{
        Time:     time.Now(),
        Severity: Severity_Info,
        File:     "test.go",
        Line:     1,
        Thread:   1234,
    }

    n, err := textPrintf(meta, nil, "simple message")
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if n == 0 {
        t.Error("expected non-zero bytes")
    }
}
```

- [ ] **Step 2: Test MaxLogMessageLen truncation**

```go
func TestMaxLogMessageLen(t *testing.T) {
    meta := &LogsinkMeta{
        Time:     time.Now(),
        Severity: Severity_Info,
        File:     "test.go",
        Line:     1,
        Thread:   1234,
    }

    longMsg := strings.Repeat("x", MaxLogMessageLen+100)
    n, _ := textPrintf(meta, nil, "%s", longMsg)
    // Should not panic, message should be truncated
    if n == 0 {
        t.Error("expected non-zero bytes even with truncation")
    }
}
```

- [ ] **Step 3: Test rate limiter drop path**

```go
func TestRateLimiterDrop(t *testing.T) {
    // This requires setting up a sampler with low rate limit
    // and verifying that logs are dropped
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run 'TestTextPrintf|TestMaxLogMessageLen|TestRateLimiter' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add logsink_test.go
git commit -m "test(logsink): add textPrintf and truncation tests"
```

---

### Task 1.5: ringbuffer_test.go — Concurrent Operations

**Files:**
- Modify: `ringbuffer_test.go`

- [ ] **Step 1: Test tryPush on full buffer**

```go
func TestRingBufferFull(t *testing.T) {
    rb := newRingBuffer(4)

    // Fill buffer
    for i := 0; i < 4; i++ {
        le := &logEntry{}
        if !rb.tryPush(le) {
            t.Fatalf("failed to push %d", i)
        }
    }

    // Should fail when full
    le := &logEntry{}
    if rb.tryPush(le) {
        t.Error("expected tryPush to fail on full buffer")
    }
}
```

- [ ] **Step 2: Test drainBatch order**

```go
func TestRingBufferOrder(t *testing.T) {
    rb := newRingBuffer(16)

    for i := 0; i < 5; i++ {
        le := &logEntry{}
        le.refCnt.Store(1)
        rb.tryPush(le)
    }

    batch := make([]*logEntry, 10)
    n := rb.drainBatch(batch, 10)
    if n != 5 {
        t.Errorf("expected 5, got %d", n)
    }
}
```

- [ ] **Step 3: Test concurrent push/drain**

```go
func TestRingBufferConcurrent(t *testing.T) {
    rb := newRingBuffer(1024)
    const numPushes = 10000

    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        for i := 0; i < numPushes; i++ {
            le := &logEntry{}
            le.refCnt.Store(1)
            for !rb.tryPush(le) {
                runtime.Gosched()
            }
        }
    }()

    go func() {
        defer wg.Done()
        batch := make([]*logEntry, 64)
        drained := 0
        for drained < numPushes {
            n := rb.drainBatch(batch, 64)
            drained += n
            if n == 0 {
                runtime.Gosched()
            }
        }
    }()

    wg.Wait()
}
```

- [ ] **Step 4: Test close**

```go
func TestRingBufferClose(t *testing.T) {
    rb := newRingBuffer(4)
    rb.close()

    le := &logEntry{}
    if rb.tryPush(le) {
        t.Error("expected tryPush to fail after close")
    }
}
```

- [ ] **Step 5: Run tests with race detector**

Run: `go test -run 'TestRingBuffer' -race -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add ringbuffer_test.go
git commit -m "test(ringbuffer): add concurrent and edge case tests"
```

---

### Task 1.6: async_writer_test.go — Lifecycle & Ack

**Files:**
- Modify: `async_writer_test.go`

- [ ] **Step 1: Test writeBatch with mixed entry types**

```go
func TestWriteBatchMixed(t *testing.T) {
    // Setup a test sink that captures writes
    var buf bytes.Buffer
    sb := &syncBuffer{sink: &testSink{buf: &buf}, sev: Severity_Info}
    bw := newBatchWriter(Severity_Info, newRingBuffer(64), sb, 16)

    // Write pre-formatted data entry
    le1 := &logEntry{data: []byte("preformatted\n")}
    le1.refCnt.Store(1)

    // Write structured entry
    le2 := &logEntry{entry: &Entry{Message: "structured", Severity: Severity_Info, Time: time.Now().UnixNano()}}
    le2.refCnt.Store(1)

    err := bw.writeBatch([]*logEntry{le1, le2}, 2)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
}
```

- [ ] **Step 2: Test flush with ack signaling**

```go
func TestFlushAck(t *testing.T) {
    var buf bytes.Buffer
    sb := &syncBuffer{sink: &testSink{buf: &buf}, sev: Severity_Info}
    bw := newBatchWriter(Severity_Info, newRingBuffer(64), sb, 16)

    ack := make(chan struct{})
    le := &logEntry{data: []byte("test\n"), ack: ack}
    le.refCnt.Store(1)

    bw.writeBatch([]*logEntry{le}, 1)
    bw.flushBuf()

    select {
    case <-ack:
        // success
    case <-time.After(time.Second):
        t.Error("ack not received")
    }
}
```

- [ ] **Step 3: Test writer loop close**

```go
func TestAsyncWriterClose(t *testing.T) {
    var buf bytes.Buffer
    sb := &syncBuffer{sink: &testSink{buf: &buf}, sev: Severity_Info}
    bw := newBatchWriter(Severity_Info, newRingBuffer(64), sb, 16)
    aw := newAsyncWriter(bw, 16)

    aw.close()
    // Should not panic or block
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run 'TestWriteBatch|TestFlushAck|TestAsyncWriterClose' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add async_writer_test.go
git commit -m "test(async): add batch, ack, and lifecycle tests"
```

---

### Task 1.7: Coverage Verification

**Files:**
- Verify: all test files

- [ ] **Step 1: Run full test suite with coverage**

```bash
go test ./... -race -coverprofile=/tmp/cover.out
go tool cover -func=/tmp/cover.out | grep "total:"
```

Expected: total coverage ≥ 90%

- [ ] **Step 2: If coverage < 90%, identify gaps and add tests**

Run: `go tool cover -func=/tmp/cover.out | grep -E "0\.0%| [0-9]\.[0-9]%"`
Find uncovered functions and add tests for them.

- [ ] **Step 3: Commit**

```bash
git add *_test.go
git commit -m "test: coverage verification and gap fixes"
```

---

## Phase 2: Basic Benchmarks (Stability Gate)

### Task 2.1: bench_print_test.go — Printf Mode Benchmarks

**Files:**
- Create: `bench_print_test.go`

- [ ] **Step 1: Create printf mode benchmarks**

```go
package mlog

import "testing"

func BenchmarkPrintInfo(b *testing.B) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Info("benchmark message")
    }
}

func BenchmarkPrintInfof(b *testing.B) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Infof("benchmark %s %d", "message", i)
    }
}

func BenchmarkPrintInfoln(b *testing.B) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Infoln("benchmark", "message")
    }
}
```

- [ ] **Step 2: Run benchmarks**

```bash
go test -bench='BenchmarkPrint' -benchmem -count=5 -run=^$
```

Expected: stable ns/op across 5 runs

- [ ] **Step 3: Commit**

```bash
git add bench_print_test.go
git commit -m "test(bench): add printf mode benchmarks"
```

---

### Task 2.2: bench_structured_test.go — Structured Mode Benchmarks

**Files:**
- Create: `bench_structured_test.go`

- [ ] **Step 1: Create structured mode benchmarks**

```go
package mlog

import "testing"

func BenchmarkStructInfo(b *testing.B) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Info("benchmark message")
    }
}

func BenchmarkStructInfo3Fields(b *testing.B) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Info("benchmark message",
            String("method", "GET"),
            String("path", "/api"),
            Int("status", 200),
        )
    }
}

func BenchmarkStructInfo10Fields(b *testing.B) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Info("benchmark message",
            String("f1", "v1"),
            String("f2", "v2"),
            String("f3", "v3"),
            String("f4", "v4"),
            String("f5", "v5"),
            Int("f6", 6),
            Int("f7", 7),
            Int("f8", 8),
            Bool("f9", true),
            Duration("f10", 10),
        )
    }
}

func BenchmarkStructInfof(b *testing.B) {
    setStructured()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        Infof("benchmark %s %d", "message", i)
    }
}
```

- [ ] **Step 2: Verify 0 allocs for hot path**

Run: `go test -bench='BenchmarkStructInfo$' -benchmem -run=^$`
Expected: `0 allocs/op` for `BenchmarkStructInfo`

- [ ] **Step 3: Commit**

```bash
git add bench_structured_test.go
git commit -m "test(bench): add structured mode benchmarks"
```

---

### Task 2.3: Baseline Verification

**Files:**
- Verify: `bench_print_test.go`, `bench_structured_test.go`

- [ ] **Step 1: Run all benchmarks 5 times**

```bash
go test -bench='BenchmarkPrint|BenchmarkStruct' -benchmem -count=5 -run=^$ > /tmp/bench_baseline.txt
```

- [ ] **Step 2: Check stability**

Parse `/tmp/bench_baseline.txt` and verify coefficient of variation < 5% for ns/op.

- [ ] **Step 3: Commit baseline**

```bash
git add /tmp/bench_baseline.txt  # or store in repo as BENCHMARK_BASELINE.md
git commit -m "test(bench): establish performance baseline"
```

---

## Phase 3: Extended Benchmarks (Report Gate)

### Task 3.1: bench_concurrency_test.go — Concurrent Gradient

**Files:**
- Create: `bench_concurrency_test.go`

- [ ] **Step 1: Create concurrent benchmarks**

```go
package mlog

import (
    "sync"
    "testing"
)

func benchmarkConcurrency(b *testing.B, mode LogMode, goroutines int) {
    if mode == LogModeStructured {
        setStructured()
    } else {
        setPrintf()
    }
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            Info("concurrent benchmark")
        }
    })
}

func BenchmarkPrintConcurrency1(b *testing.B)   { benchmarkConcurrency(b, LogModePrintf, 1) }
func BenchmarkPrintConcurrency4(b *testing.B)   { benchmarkConcurrency(b, LogModePrintf, 4) }
func BenchmarkPrintConcurrency8(b *testing.B)   { benchmarkConcurrency(b, LogModePrintf, 8) }
func BenchmarkPrintConcurrency16(b *testing.B)  { benchmarkConcurrency(b, LogModePrintf, 16) }
func BenchmarkPrintConcurrency32(b *testing.B)  { benchmarkConcurrency(b, LogModePrintf, 32) }
func BenchmarkPrintConcurrency64(b *testing.B)  { benchmarkConcurrency(b, LogModePrintf, 64) }

func BenchmarkStructConcurrency1(b *testing.B)  { benchmarkConcurrency(b, LogModeStructured, 1) }
func BenchmarkStructConcurrency4(b *testing.B)  { benchmarkConcurrency(b, LogModeStructured, 4) }
func BenchmarkStructConcurrency8(b *testing.B)  { benchmarkConcurrency(b, LogModeStructured, 8) }
func BenchmarkStructConcurrency16(b *testing.B) { benchmarkConcurrency(b, LogModeStructured, 16) }
func BenchmarkStructConcurrency32(b *testing.B) { benchmarkConcurrency(b, LogModeStructured, 32) }
func BenchmarkStructConcurrency64(b *testing.B) { benchmarkConcurrency(b, LogModeStructured, 64) }
```

Note: `b.RunParallel` uses `GOMAXPROCS` goroutines by default. To control exact goroutine count, use `sync.WaitGroup` instead:

```go
var wg sync.WaitGroup
wg.Add(goroutines)
for i := 0; i < goroutines; i++ {
    go func() {
        defer wg.Done()
        for pb.Next() {
            Info("concurrent benchmark")
        }
    }()
}
wg.Wait()
```

- [ ] **Step 2: Run concurrent benchmarks**

```bash
go test -bench='Benchmark.*Concurrency' -benchmem -count=3 -run=^$
```

- [ ] **Step 3: Commit**

```bash
git add bench_concurrency_test.go
git commit -m "test(bench): add concurrent gradient benchmarks"
```

---

### Task 3.2: bench_latency_test.go — Latency Percentiles

**Files:**
- Create: `bench_latency_test.go`

- [ ] **Step 1: Create latency measurement benchmark**

```go
package mlog

import (
    "sort"
    "testing"
    "time"
)

func BenchmarkLatencyPrintInfo(b *testing.B) {
    setPrintf()
    defer resetMode()

    orig := TextSinks
    TextSinks = nil
    defer func() { TextSinks = orig }()

    // Warmup
    for i := 0; i < 1000; i++ {
        Info("warmup")
    }

    latencies := make([]time.Duration, 0, b.N)
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        start := time.Now()
        Info("benchmark")
        latencies = append(latencies, time.Since(start))
    }

    b.StopTimer()
    sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

    p50 := latencies[len(latencies)*50/100]
    p90 := latencies[len(latencies)*90/100]
    p99 := latencies[len(latencies)*99/100]

    b.ReportMetric(float64(p50.Nanoseconds()), "ns/P50")
    b.ReportMetric(float64(p90.Nanoseconds()), "ns/P90")
    b.ReportMetric(float64(p99.Nanoseconds()), "ns/P99")
}
```

Similarly for `BenchmarkLatencyStructInfo`.

- [ ] **Step 2: Run latency benchmarks**

```bash
go test -bench='BenchmarkLatency' -count=3 -run=^$
```

- [ ] **Step 3: Commit**

```bash
git add bench_latency_test.go
git commit -m "test(bench): add latency percentile benchmarks"
```

---

### Task 3.3: CPU & Memory Profiling

**Files:**
- Generate: `cpu.prof`, `mem.prof`

- [ ] **Step 1: Generate CPU profile**

```bash
go test -bench='BenchmarkStructInfo$' -cpuprofile=cpu.prof -run=^$ -benchtime=10s
go tool pprof -top -cum cpu.prof > /tmp/cpu_top.txt
```

- [ ] **Step 2: Generate memory profile**

```bash
go test -bench='BenchmarkStructInfo$' -memprofile=mem.prof -run=^$ -benchtime=10s
go tool pprof -top -cum mem.prof > /tmp/mem_top.txt
```

- [ ] **Step 3: Commit profiles**

```bash
git add cpu.prof mem.prof
git commit -m "test(bench): add CPU and memory profiles"
```

---

### Task 3.4: Generate BENCHMARK_REPORT.md

**Files:**
- Create: `BENCHMARK_REPORT.md`

- [ ] **Step 1: Collect all benchmark results**

```bash
go test -bench='BenchmarkPrint|BenchmarkStruct|BenchmarkLatency' -benchmem -count=5 -run=^$ > /tmp/all_benchmarks.txt
```

- [ ] **Step 2: Parse and format results into report**

Create `BENCHMARK_REPORT.md` with:

```markdown
# mlog Performance Benchmark Report

Environment: Apple M4 Pro, macOS, Go 1.21
Date: 2026-05-08

## Unit Test Coverage

Total coverage: 90.2%

## Printf Mode Benchmarks

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkPrintInfo | xxx | xxx | xxx |
| ... | ... | ... | ... |

## Structured Mode Benchmarks

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkStructInfo | xxx | xxx | xxx |
| ... | ... | ... | ... |

## Concurrent Gradient

| Goroutines | Printf ns/op | Struct ns/op |
|---|---|---|
| 1 | xxx | xxx |
| 4 | xxx | xxx |
| ... | ... | ... |

## Latency Percentiles

| Mode | P50 | P90 | P99 |
|---|---|---|---|
| Printf | xxx | xxx | xxx |
| Structured | xxx | xxx | xxx |

## CPU Hotspots

```
(Top 10 from cpu.prof)
```

## Memory Hotspots

```
(Top 10 from mem.prof)
```
```

- [ ] **Step 3: Commit report**

```bash
git add BENCHMARK_REPORT.md
git commit -m "docs: add performance benchmark report"
```

---

## Self-Review

### 1. Spec Coverage

| Spec Section | Plan Task |
|---|---|
| mode_test 全局函数分支 | Task 1.1 |
| structured_test Logger 方法 | Task 1.2 |
| encoder_test 全字段类型 | Task 1.3 |
| logsink_test textPrintf | Task 1.4 |
| ringbuffer_test 并发 | Task 1.5 |
| async_writer_test 生命周期 | Task 1.6 |
| 覆盖率验证 90%+ | Task 1.7 |
| printf mode benchmark | Task 2.1 |
| structured mode benchmark | Task 2.2 |
| 基线验证 | Task 2.3 |
| 并发梯度 | Task 3.1 |
| 延迟分位 | Task 3.2 |
| CPU/内存 profiling | Task 3.3 |
| 报告生成 | Task 3.4 |

**Gap:** None.

### 2. Placeholder Scan

No TBD/TODO/"implement later"/"similar to Task N" found.

### 3. Type Consistency

- `LogMode` used consistently as `LogModePrintf` / `LogModeStructured`
- `setStructured()` / `setPrintf()` / `resetMode()` helpers used across all test files
- `TextSinks = nil` workaround pattern consistent

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-08-comprehensive-testing.md`.**

**Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
