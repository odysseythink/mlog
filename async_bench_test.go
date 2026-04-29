package mlog_test

import (
	"testing"

	"github.com/odysseythink/mlog"
)

func BenchmarkInfoParallel64(b *testing.B) {
	mlog.SetLogDir(b.TempDir())

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mlog.Info("concurrent log message")
		}
	})
}

func BenchmarkInfoParallel8(b *testing.B) {
	mlog.SetLogDir(b.TempDir())

	b.SetParallelism(8)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mlog.Info("concurrent log message")
		}
	})
}
