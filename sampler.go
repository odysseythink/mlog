package mlog

import (
	"sync/atomic"
	"time"
)

// sampler is a lock-free token bucket rate limiter for log entries.
// Zero overhead when disabled (nil sampler checked before use).
type sampler struct {
	tokens     atomic.Int64
	maxTokens  int64
	refillRate int64
	lastRefill atomic.Int64
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
		return true
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
