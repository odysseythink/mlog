package mlog

import (
	"strings"
	"testing"
	"time"
)

func TestStructuredLogInfo(t *testing.T) {
	S().Info("hello", String("key", "value"))
}

func TestStructuredLogWithFields(t *testing.T) {
	logger := S().With(String("request_id", "abc123"))
	logger.Info("handling request", Int("status", 200))
}

func TestStructuredLogAllSeverities(t *testing.T) {
	S().Info("info msg")
	S().Warning("warning msg")
	S().Error("error msg")
}

func TestTextEncoderIntegration(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "test",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("code", 200),
			Bool("ok", true),
			Duration("elapsed", 5*time.Second),
		},
	}

	enc := getEncoder()
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "code=200") {
		t.Fatalf("TextEncoder missing field: %s", s)
	}
	if !strings.Contains(s, "ok=true") {
		t.Fatalf("TextEncoder missing bool: %s", s)
	}
}

func TestJSONEncoderIntegration(t *testing.T) {
	SetEncoder(&jsonEncoder{})
	defer SetEncoder(defaultTextEncoder)

	e := &Entry{
		Severity: Severity_Info,
		Time:     time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC).UnixNano(),
		Message:  "test",
		File:     "main.go",
		Line:     1,
		Fields:   []Field{Int("code", 200)},
	}

	enc := getEncoder()
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, `"code":200`) {
		t.Fatalf("JSONEncoder missing field: %s", s)
	}
}
