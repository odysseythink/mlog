package mlog

import (
	"testing"
	"time"
)

func BenchmarkFieldConstruction(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Int("count", 42)
		_ = String("name", "test")
		_ = Bool("ok", true)
		_ = Float64("ratio", 3.14)
	}
}

func BenchmarkTextEncoderEncode(b *testing.B) {
	enc := &textEncoder{}
	entry := &Entry{
		Severity: Severity_Info,
		Time:     time.Now().UnixNano(),
		Message:  "benchmark log message",
		File:     "main.go",
		Line:     42,
		Funcname: "main.main",
		Thread:   12345,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
			Duration("elapsed", 5*time.Millisecond),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := enc.EncodeEntry(entry)
		putEncBuf(&out)
	}
}

func BenchmarkJSONEncoderEncode(b *testing.B) {
	enc := &jsonEncoder{}
	entry := &Entry{
		Severity: Severity_Info,
		Time:     time.Now().UnixNano(),
		Message:  "benchmark log message",
		File:     "main.go",
		Line:     42,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := enc.EncodeEntry(entry)
		putEncBuf(&out)
	}
}

func BenchmarkEntryPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := getEntry()
		putEntry(e)
	}
}

func BenchmarkStructuredInfo(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := getEntry()
		e.Severity = Severity_Info
		e.Time = time.Now().UnixNano()
		e.Message = "benchmark message"
		e.File = "bench_test.go"
		e.Line = 42
		e.Funcname = "BenchmarkStructuredInfo"
		e.Thread = 12345
		e.Fields = append(e.Fields[:0], Int("status", 200), String("method", "GET"))
		putEntry(e)
	}
}
