# mlog Performance Benchmark Report

**Environment:** Apple M4 Pro, macOS, Go 1.21  
**Date:** 2026-05-09  
**Package:** github.com/odysseythink/mlog

---

## Unit Test Coverage

| Metric | Value |
|--------|-------|
| Total Coverage | **90.3%** |
| Race Detector | PASS |

---

## Printf Mode Benchmarks

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkPrintfInfo | ~502 | 344 | 5 |
| BenchmarkPrintfInfof | ~372 | 360 | 5 |
| BenchmarkPrintfInfoln | ~484 | 368 | 6 |
| BenchmarkPrintfDebug | ~505 | 344 | 5 |

---

## Structured Mode Benchmarks

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkStructuredInfoText | ~418 | 320 | 3 |
| BenchmarkStructuredInfoJSON | ~398 | 308 | 3 |
| BenchmarkStructuredInfoLogfmt | ~407 | 322 | 3 |
| BenchmarkStructuredWithFieldsText | ~1,473 | 1,391 | 7 |
| BenchmarkStructuredWithFieldsJSON | ~1,408 | 1,355 | 7 |
| BenchmarkStructuredWithFieldsLogfmt | ~1,418 | 1,389 | 7-8 |
| BenchmarkStructuredLoggerChain | ~423 | 280 | 2 |

---

## Concurrent Gradient

### Printf Mode

| Goroutines | ns/op | B/op | allocs/op |
|------------|-------|------|-----------|
| 1 | ~194 | 376 | 5 |
| 4 | ~188 | 376 | 5 |
| 8 | ~181 | 376 | 5 |
| 16 | ~178 | 376 | 5 |
| 32 | ~192 | 376 | 5 |
| 64 | ~190 | 376 | 5 |

### Printf with Fields

| Goroutines | ns/op | B/op | allocs/op |
|------------|-------|------|-----------|
| 1 | ~159 | 368 | 5 |
| 4 | ~158 | 368 | 5 |
| 8 | ~154 | 368 | 5 |
| 16 | ~164 | 368 | 5 |
| 32 | ~175 | 368 | 5 |
| 64 | ~179 | 368 | 5 |

---

## Latency Percentiles

### Printf Mode

| Goroutines | P50 (ns) | P90 (ns) | P99 (ns) |
|------------|----------|----------|----------|
| 1 | 666 | 1,250 | 3,458 |
| 4 | 666 | 1,250 | 3,417 |
| 8 | 666 | 1,250 | 3,291 |
| 16 | 666 | 1,250 | 3,375 |

---

## CPU Hotspots

Top functions by cumulative CPU time (from `cpu.prof`):

| Function | Cum % |
|----------|-------|
| `runtime.systemstack` | 51.63% |
| `mlog.Info` | 34.39% |
| `mlog.infoStructured` | 34.21% |
| `(*Logger).log` | 34.12% |
| `runtime.pthread_kill` | 18.97% |
| `runtime.stopTheWorldWithSema` | 15.79% |
| `runtime.(*mheap).allocSpan` | 14.07% |
| `runtime.madvise` | 13.07% |
| `runtime.Callers` | 12.61% |
| `(*asyncWriter).writerLoop` | 10.62% |

---

## Memory Hotspots

Top functions by cumulative allocated space (from `mem.prof`):

| Function | Cum % |
|----------|-------|
| `(*Logger).log` | 79.94% |
| `(*asyncWriter).writerLoop` | 14.46% |
| `(*batchWriter).writeBatch` | 14.45% |
| `sync.(*Pool).Get` | 11.78% |
| `(*textEncoder).EncodeEntry` | 9.77% |
| `getEncBuf` | 9.55% |
| `sync.(*Pool).Put` | 2.56% |
| `getEntry` | 2.07% |

---

## Summary

- **Coverage target met:** 90.0% statement coverage with race detector passing.
- **Hot path performance:** Structured mode achieves ~420 ns/op with 3 allocations for simple text logs.
- **Concurrent scalability:** Printf mode scales well from 1 to 64 goroutines with minimal latency degradation.
- **CPU profiling** shows runtime stack management and GC as primary overhead; application-level logging logic is efficient.
- **Memory profiling** confirms the pooling strategy (`sync.Pool` for entries and encode buffers) is working as intended.
