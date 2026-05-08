package mlog

import (
	"strings"
	"testing"
	"time"
)

func TestStructuredLogInfo(t *testing.T) {
	setStructured()
	defer resetMode()
	With().Info("hello", String("key", "value"))
}

func TestStructuredLogWithFields(t *testing.T) {
	setStructured()
	defer resetMode()
	logger := With(String("request_id", "abc123"))
	logger.Info("handling request", Int("status", 200))
}

func TestStructuredLogAllSeverities(t *testing.T) {
	setStructured()
	defer resetMode()
	With().Info("info msg")
	With().Warning("warning msg")
	With().Error("error msg")
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

func TestLoggerPrintfMode(t *testing.T) {
	setPrintf()
	defer resetMode()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	l := With(String("svc", "test"))

	l.Debug("debug msg")
	l.Debugf("debug %s", "msg")
	l.Debugln("debug", "msg")

	l.Info("info msg", String("k", "v"))
	l.Infof("info %s", "msg")
	l.Infoln("info", "msg")

	l.Warning("warn msg")
	l.Warningf("warn %s", "msg")
	l.Warningln("warn", "msg")

	l.Error("error msg")
	l.Errorf("error %s", "msg")
	l.Errorln("error", "msg")
}

func TestLoggerStructuredMode(t *testing.T) {
	setStructured()
	defer resetMode()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	l := With(String("svc", "test"))

	l.Debug("debug msg", String("k", "v"))
	l.Debugf("debug %s", "msg")
	l.Debugln("debug", "msg")

	l.Info("info msg", String("k", "v"))
	l.Infof("info %s", "msg")
	l.Infoln("info", "msg")

	l.Warning("warn msg", String("k", "v"))
	l.Warningf("warn %s", "msg")
	l.Warningln("warn", "msg")

	l.Error("error msg", String("k", "v"))
	l.Errorf("error %s", "msg")
	l.Errorln("error", "msg")
}

func TestLoggerWithChaining(t *testing.T) {
	l1 := With(String("a", "1"))
	l2 := l1.With(Int("b", 2))

	if len(l1.fields) != 1 {
		t.Errorf("l1 mutated: got %d fields", len(l1.fields))
	}
	if len(l2.fields) != 2 {
		t.Errorf("l2 expected 2 fields, got %d", len(l2.fields))
	}
}
