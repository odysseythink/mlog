package mlog

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSamplerAllow(t *testing.T) {
	s := newSampler(10, 2)

	if !s.allow() {
		t.Fatal("first allow should succeed")
	}
	if !s.allow() {
		t.Fatal("second allow should succeed")
	}
	if s.allow() {
		t.Fatal("third allow should fail with empty bucket")
	}
}

func TestSamplerRefill(t *testing.T) {
	s := newSampler(100, 1)

	if !s.allow() {
		t.Fatal("first allow should succeed")
	}
	if s.allow() {
		t.Fatal("second allow should fail")
	}

	time.Sleep(20 * time.Millisecond)

	if !s.allow() {
		t.Fatal("allow after refill should succeed")
	}
}

func TestSamplerConcurrent(t *testing.T) {
	s := newSampler(1000, 100)

	var wg sync.WaitGroup
	var allowed atomic.Int64
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

	if allowed.Load() > 100+10 {
		t.Fatalf("allowed %d, expected at most ~110", allowed.Load())
	}
}

func TestSamplerSeverityBypass(t *testing.T) {
	s := newSampler(1, 1)

	if !s.allow() {
		t.Fatal("first allow should succeed")
	}

	if s.allowSeverity(Severity_Info) {
		t.Fatal("Info should be denied when bucket empty")
	}
	if !s.allowSeverity(Severity_Error) {
		t.Fatal("Error should bypass rate limit")
	}
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

func TestSamplerIntegration(t *testing.T) {
	orig := logSampler.Load()
	defer func() {
		if orig != nil {
			logSampler.Store(orig)
		} else {
			logSampler.Store((*sampler)(nil))
		}
	}()

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

func BenchmarkSamplerDisabled(b *testing.B) {
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
