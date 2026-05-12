package mlog_test

import (
	"fmt"
	"testing"

	"github.com/odysseythink/mlog"
)

func BenchmarkConcurrencyPrintf(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	for _, g := range []int{1, 4, 8, 16, 32, 64} {
		b.Run(fmt.Sprintf("goroutines=%d", g), func(b *testing.B) {
			b.SetParallelism(g)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					mlog.Info("concurrent benchmark message")
				}
			})
		})
	}
}

func BenchmarkConcurrencyPrintfFields(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	args := []any{
		"request handled",
		mlog.String("method", "GET"),
		mlog.Int("status", 200),
	}

	for _, g := range []int{1, 4, 8, 16, 32, 64} {
		b.Run(fmt.Sprintf("goroutines=%d", g), func(b *testing.B) {
			b.SetParallelism(g)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					mlog.Info(args...)
				}
			})
		})
	}
}
