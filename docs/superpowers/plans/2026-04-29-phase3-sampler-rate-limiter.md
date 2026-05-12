# Phase 3: Sampler / Rate Limiter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional, zero-overhead-when-disabled token bucket rate limiter to prevent log flooding under high load. Per-severity policy: ERROR/FATAL bypass; DEBUG/INFO/WARNING subject to limit.

**Architecture:** Lock-free token bucket using atomic operations. Tokens refill based on elapsed time. Dropped entries counted in `Stats.Dropped` and surfaced via backpressure summary.

**Tech Stack:** Go 1.21+, `atomic.Int64`, time-based token refill

---

## File Structure

| File | Responsibility |
|------|----------------|
| `sampler.go` | `sampler` struct with token bucket, `allow()`, `allowSeverity()` methods, per-severity policy |
| `sampler_test.go` | Unit tests: basic allow/deny, token refill, concurrent access, bypass for Error/Fatal, zero overhead when nil |
| `mlog_flags.go` | Add `-log_rate_limit` flag (0 = disabled) |
| `logsink.go` | Integrate sampler into `textPrintf` path before formatting |
| `mlog.go` | Ensure `Stats.Dropped` aggregates sampler drops |

---

## Task 1: Create Sampler Core with Per-Severity Policy

**Files:**
- Create: `sampler.go`

- [ ] **Step 1: Create `sampler.go`**

```go
package mlog

import (
	"sync/atomic"
	"time"
)

// sampler is a lock-free token bucket rate limiter for log entries.
// Zero overhead when disabled (nil sampler checked before use).
type sampler struct {
	tokens     atomic.Int64 // Current tokens available
	maxTokens  int64        // Bucket capacity
	refillRate int64        // Tokens added per second
	lastRefill atomic.Int64 // Unix nanoseconds of last refill
}

// newSampler creates a sampler with the given rate (logs per second) and max burst.
func newSampler(ratePerSec int64, maxBurst int64) *sampler {
	s := &sampler{
		maxTokens:  maxBurst,
		refillRate: ratePerSec,
	}
	s.tokens.Store(maxBurst)
	s.lastRefill.Store(time.Now().UnixNano())
	return s
}

// refill adds tokens based on elapsed time.
func (s *sampler) refill() {
	now := time.Now().UnixNano()
	last := s.lastRefill.Load()
	elapsed := now - last

	if elapsed <= 0 {
		return
	}

	newTokens := elapsed * s.refillRate / 1e9
	if newTokens <= 0 {
		return
	}

	if s.lastRefill.CompareAndSwap(last, now) {
		for {
			current := s.tokens.Load()
			target := current + newTokens
			if target > s.maxTokens {
				target = s.maxTokens
			}
			if s.tokens.CompareAndSwap(current, target) {
				break
			}
		}
	}
}

// allow returns true if the log entry should be allowed through.
func (s *sampler) allow() bool {
	if s == nil {
		return true // sampler disabled, zero overhead
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

// allowSeverity returns true if the given severity should be allowed.
// Error and Fatal always bypass the rate limiter.
func (s *sampler) allowSeverity(sev Severity) bool {
	if sev >= Severity_Error {
		return true
	}
	return s.allow()
}
```

- [ ] **Step 2: Write failing sampler tests**

Create `sampler_test.go`:

```go
package mlog

import (
	"sync"
	"testing"
	"time"
)

func TestSamplerAllow(t *testing.T) {
	// Sampler with rate 10/sec, burst 2
	s := newSampler(10, 2)

	// Should allow first 2 (burst capacity)
	if !s.allow() {
		t.Fatal("first allow should succeed")
	}
	if !s.allow() {
		t.Fatal("second allow should succeed")
	}
	// Third should fail (no tokens)
	if s.allow() {
		t.Fatal("third allow should fail with empty bucket")
	}
}

func TestSamplerRefill(t *testing.T) {
	// Sampler with rate 100/sec, burst 1
	s := newSampler(100, 1)

	// Consume the single token
	if !s.allow() {
		t.Fatal("first allow should succeed")
	}
	if s.allow() {
		t.Fatal("second allow should fail")
	}

	// Wait for refill
	time.Sleep(20 * time.Millisecond)

	// Should have refilled
	if !s.allow() {
		t.Fatal("allow after refill should succeed")
	}
}

func TestSamplerConcurrent(t *testing.T) {
	s := newSampler(1000, 100)

	var wg sync.WaitGroup
	allowed := atomic.Int64{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.allow() {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	// Should allow up to burst (100) plus some refilled during test
	if allowed.Load() > 100+10 {
		t.Fatalf("allowed %d, expected at most ~110", allowed.Load())
	}
}

func TestSamplerSeverityBypass(t *testing.T) {
	s := newSampler(1, 1)

	// Consume the only token
	if !s.allow() {
		t.Fatal("first allow should succeed")
	}

	// Info should be denied (no tokens)
	if s.allowSeverity(Severity_Info) {
		t.Fatal("Info should be denied when bucket empty")
	}
	// Error should bypass
	if !s.allowSeverity(Severity_Error) {
		t.Fatal("Error should bypass rate limit")
	}
	// Fatal should bypass
	if !s.allowSeverity(Severity_Fatal) {
		t.Fatal("Fatal should bypass rate limit")
	}
}

func TestSamplerNil(t *testing.T) {
	var s *sampler
	if !s.allow() {
		t.Fatal("nil sampler should always allow")
	}
	if !s.allowSeverity(Severity_Info) {
		t.Fatal("nil sampler should always allowSeverity")
	}
}
```

Run: `go test -run TestSampler -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add sampler.go sampler_test.go
git commit -m "feat(sampler): implement token bucket rate limiter with per-severity policy (v2)

Add lock-free sampler with atomic token refill based on elapsed time.
Per-severity policy: ERROR/FATAL bypass; DEBUG/INFO/WARNING subject
to limit. Zero-allocation allow() check. Nil sampler has zero
overhead. Includes tests for allow/deny, refill, concurrent access,
and severity bypass."
```

---

## Task 2: Add Flag and Integrate Sampler

**Files:**
- Modify: `mlog_flags.go`
- Modify: `logsink.go`
- Modify: `mlog_file.go`

- [ ] **Step 1: Add rate limit flag**

In `mlog_flags.go`, add after existing flags:

```go
var (
	logRateLimit = flag.Int("log_rate_limit", 0, "Maximum log entries per second (0 = disabled)")
)
```

- [ ] **Step 2: Add sampler initialization**

In `mlog_file.go`, add package-level variable and init:

```go
var (
	logSampler atomic.Value // *sampler, nil when disabled
)

func getSampler() *sampler {
	if s := logSampler.Load(); s != nil {
		return s.(*sampler)
	}
	return nil
}

func initSampler() {
	if *logRateLimit > 0 {
		logSampler.Store(newSampler(int64(*logRateLimit), int64(*logRateLimit)))
	}
}
```

Call `initSampler()` at end of `init()` in `mlog_file.go`.

- [ ] **Step 3: Integrate sampler into textPrintf**

In `logsink.go`, inside `textPrintf`, after the sinks are determined (around line 180), add:

```go
	// Check rate limiter for Debug/Info/Warning only
	if s := getSampler(); s != nil {
		if !s.allowSeverity(m.Severity) {
			atomic.AddInt64(&Stats.Dropped.lines, 1)
			atomic.AddInt64(&Stats.Dropped.bytes, 0)
			return 0, nil
		}
	}
```

- [ ] **Step 4: Write integration test**

Add to `sampler_test.go`:

```go
func TestSamplerIntegration(t *testing.T) {
	// Save and restore original sampler
	orig := logSampler.Load()
	defer logSampler.Store(orig)

	// Set a sampler with rate 2/sec
	logSampler.Store(newSampler(2, 2))

	// First two should succeed
	if !getSampler().allow() {
		t.Fatal("first allow should succeed")
	}
	if !getSampler().allow() {
		t.Fatal("second allow should succeed")
	}
	// Third should fail
	if getSampler().allow() {
		t.Fatal("third allow should fail")
	}
}
```

Run: `go test -run TestSamplerIntegration -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add mlog_flags.go logsink.go mlog_file.go sampler_test.go
git commit -m "feat(sampler): integrate rate limiter into log path (v2)

Add -log_rate_limit flag. Rate limiter checks in textPrintf before
formatting. Non-Fatal severity subject to limit; Fatal always allowed.
Dropped entries counted in Stats.Dropped. Zero overhead when disabled
(nil sampler check). Per-severity policy: ERROR/FATAL bypass."
```

---

## Task 3: Benchmarks and Final Verification

**Files:**
- Modify: `sampler_test.go`

- [ ] **Step 1: Add benchmarks**

Add to `sampler_test.go`:

```go
func BenchmarkSamplerDisabled(b *testing.B) {
	// Ensure sampler is nil (disabled)
	logSampler.Store((*sampler)(nil))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getSampler()
	}
}

func BenchmarkSamplerEnabled(b *testing.B) {
	logSampler.Store(newSampler(10000, 10000))
	s := getSampler()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.allow()
	}
}

func BenchmarkSamplerSeverityCheck(b *testing.B) {
	logSampler.Store(newSampler(10000, 10000))
	s := getSampler()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.allowSeverity(Severity_Info)
	}
}
```

Run: `go test -bench=BenchmarkSampler -benchmem -run=^$`
Expected: 
- `BenchmarkSamplerDisabled` should show 0 allocs/op and very fast
- `BenchmarkSamplerEnabled` should show ~1 alloc/op (for refill) and fast
- `BenchmarkSamplerSeverityCheck` should show 0 allocs/op (bypass path)

- [ ] **Step 2: Run full test suite with race detector**

Run: `go test ./... -race -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add sampler_test.go
git commit -m "test(perf): add Phase 3 benchmarks (v2)

Add benchmarks for disabled, enabled, and severity-check paths.
Verify zero allocations when sampler is nil. Finalize Phase 3."
```

---

## Self-Review

**1. Spec coverage:**
- [x] Token bucket rate limiter with atomic operations - Task 1
- [x] `-log_rate_limit` flag - Task 2
- [x] Per-severity policy (Error/Fatal bypass) - Task 1
- [x] Stats.Dropped counting - Task 2
- [x] Zero overhead when disabled - Task 1, 3
- [x] Backpressure integration - Task 2

**2. Placeholder scan:**
- No "TBD", "TODO", or "implement later" found
- All test code is complete with actual assertions
- All implementation code is complete

**3. Type consistency:**
- `sampler` fields match usage in `allow()` and `allowSeverity()`
- `logSampler` is `atomic.Value` storing `*sampler`
- `Stats.Dropped` matches Phase 2 addition
- `refillRate` consistently named across struct and init

**No gaps found. Plan is complete.**
