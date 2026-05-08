package mlog_test

import (
	"testing"

	"github.com/odysseythink/mlog"
)

func BenchmarkPrintfInfo(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info("benchmark log message")
	}
}

func BenchmarkPrintfInfof(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Infof("benchmark %s %d", "msg", 42)
	}
}

func BenchmarkPrintfInfoln(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Infoln("benchmark", "message")
	}
}

func BenchmarkPrintfDebug(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Debug("debug message")
	}
}

func BenchmarkPrintfParallel(b *testing.B) {
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mlog.Info("concurrent log message")
		}
	})
}
