package mlog_test

import (
	"testing"
	"time"

	"github.com/odysseythink/mlog"
)

func BenchmarkStructuredInfoText(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewTextEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	// Warm-up to pre-initialize file sinks
	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info("benchmark log message")
	}
}

func BenchmarkStructuredInfoJSON(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewJSONEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info("benchmark log message")
	}
}

func BenchmarkStructuredInfoLogfmt(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewLogfmtEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info("benchmark log message")
	}
}

func BenchmarkStructuredWithFieldsText(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewTextEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	args := []any{
		"request handled",
		mlog.String("method", "GET"),
		mlog.Int("status", 200),
		mlog.Bool("cached", true),
		mlog.Float64("latency", 1.23),
		mlog.Duration("elapsed", 5*time.Millisecond),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info(args...)
	}
}

func BenchmarkStructuredWithFieldsJSON(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewJSONEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	args := []any{
		"request handled",
		mlog.String("method", "GET"),
		mlog.Int("status", 200),
		mlog.Bool("cached", true),
		mlog.Float64("latency", 1.23),
		mlog.Duration("elapsed", 5*time.Millisecond),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info(args...)
	}
}

func BenchmarkStructuredWithFieldsLogfmt(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewLogfmtEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	args := []any{
		"request handled",
		mlog.String("method", "GET"),
		mlog.Int("status", 200),
		mlog.Bool("cached", true),
		mlog.Float64("latency", 1.23),
		mlog.Duration("elapsed", 5*time.Millisecond),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mlog.Info(args...)
	}
}

func BenchmarkStructuredLoggerChain(b *testing.B) {
	mlog.SetLogMode(mlog.LogModeStructured)
	mlog.SetEncoder(mlog.NewTextEncoder())
	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	mlog.SetLogDir(b.TempDir())
	mlog.Info("warmup")

	logger := mlog.With(
		mlog.String("service", "api"),
		mlog.String("version", "1.0.0"),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("request handled")
	}
}
