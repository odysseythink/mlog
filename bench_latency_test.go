package mlog_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/odysseythink/mlog"
)

// BenchmarkLatencyPrintf measures per-operation latency at different concurrency levels.
func BenchmarkLatencyPrintf(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	for _, g := range []int{1, 4, 8, 16} {
		b.Run(fmt.Sprintf("goroutines=%d", g), func(b *testing.B) {
			var latencies []time.Duration
			var mu sync.Mutex

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					start := time.Now()
					mlog.Info("latency benchmark message")
					elapsed := time.Since(start)
					mu.Lock()
					latencies = append(latencies, elapsed)
					mu.Unlock()
				}
			})

			// Report latency percentiles
			if len(latencies) > 0 {
				sortDurations(latencies)
				p50 := latencies[len(latencies)*50/100]
				p90 := latencies[len(latencies)*90/100]
				p99 := latencies[len(latencies)*99/100]
				b.ReportMetric(float64(p50.Nanoseconds()), "p50_ns/op")
				b.ReportMetric(float64(p90.Nanoseconds()), "p90_ns/op")
				b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns/op")
			}
		})
	}
}

func sortDurations(a []time.Duration) {
	// Simple insertion sort for small slices
	for i := 1; i < len(a); i++ {
		j := i
		for j > 0 && a[j-1] > a[j] {
			a[j-1], a[j] = a[j], a[j-1]
			j--
		}
	}
}
