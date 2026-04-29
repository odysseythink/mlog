package mlog

import (
	"strings"
	"testing"
	"time"
)

func TestTextEncoderNoFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 123456000, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "hello world",
		File:     "/src/main.go",
		Line:     42,
		Funcname: "main.main",
		Thread:   12345,
	}

	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "[2026-04-30") {
		t.Fatalf("missing date in: %s", s)
	}
	if !strings.Contains(s, "[I]") {
		t.Fatalf("missing severity in: %s", s)
	}
	if !strings.Contains(s, "hello world") {
		t.Fatalf("missing message in: %s", s)
	}
	if !strings.Contains(s, "main.main:42") {
		t.Fatalf("missing caller in: %s", s)
	}
}

func TestTextEncoderWithFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "request",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
			Bool("ok", true),
		},
	}

	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "status=200") {
		t.Fatalf("missing int field in: %s", s)
	}
	if !strings.Contains(s, "method=GET") {
		t.Fatalf("missing string field in: %s", s)
	}
	if !strings.Contains(s, "ok=true") {
		t.Fatalf("missing bool field in: %s", s)
	}
}
