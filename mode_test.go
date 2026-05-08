package mlog

import (
	"sync"
	"testing"
)

func resetMode() {
	modeSetOnce = sync.Once{}
	logMode.Store(0)
}

func setStructured() {
	resetMode()
	SetLogMode(LogModeStructured)
}

func setPrintf() {
	resetMode()
	// printf is default (0)
}

func TestSetLogMode(t *testing.T) {
	// Save original state
	origMode := logMode.Load()

	// Reset for testing
	resetMode()

	SetLogMode(LogModeStructured)
	if got := getMode(); got != LogModeStructured {
		t.Errorf("getMode() = %v, want structured", got)
	}

	// Second call should be no-op (sync.Once already triggered)
	SetLogMode(LogModePrintf)
	if got := getMode(); got != LogModeStructured {
		t.Errorf("getMode() after second SetLogMode = %v, want structured (no-op)", got)
	}

	// Restore
	resetMode()
	logMode.Store(origMode)
}

func TestWith(t *testing.T) {
	logger := With(String("svc", "test"))
	if logger == nil {
		t.Fatal("With() returned nil")
	}
	if len(logger.fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(logger.fields))
	}
	if logger.fields[0].Key != "svc" || logger.fields[0].String != "test" {
		t.Errorf("unexpected field: %+v", logger.fields[0])
	}

	// Chained With
	l2 := logger.With(Int("count", 42))
	if len(l2.fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(l2.fields))
	}
	// Original logger should be unchanged
	if len(logger.fields) != 1 {
		t.Errorf("original logger mutated: got %d fields", len(logger.fields))
	}
}

func TestGlobalInfoStructuredMode(t *testing.T) {
	// Ensure structured mode
	setStructured()

	// Should not panic in structured mode
	Info("test message", String("key", "value"))
}

func TestGlobalInfofStructuredMode(t *testing.T) {
	setStructured()

	Infof("formatted %s", "message")
}

func TestLoggerInfoStructuredMode(t *testing.T) {
	setStructured()

	logger := With(String("svc", "test"))
	logger.Info("request", String("path", "/api"))
}

func TestLoggerPrintfModeBasic(t *testing.T) {
	setPrintf()
	// Default is printf mode, no SetLogMode call needed

	// Avoid writing to real files to prevent O_EXCL collisions
	// when multiple severities are initialized in the same second.
	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	logger := With(String("svc", "test"))
	logger.Info("request") // Should work in printf mode, ignoring fields
}

func TestInfoStructured(t *testing.T) {
	setStructured()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	// Empty args
	infoStructured(1, Severity_Info)

	// String msg only
	infoStructured(1, Severity_Info, "hello")

	// String msg + fields
	infoStructured(1, Severity_Info, "hello", String("k", "v"))

	// Non-string first arg falls back to fmt.Sprint
	infoStructured(1, Severity_Info, 123)
}

func TestInfofStructured(t *testing.T) {
	setStructured()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	infofStructured(1, Severity_Info, "hello %s", "world")
}

func TestGlobalFunctionsStructured(t *testing.T) {
	setStructured()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	Debug("debug msg")
	Debugf("debug %s", "msg")
	Info("info msg")
	Infof("info %s", "msg")
	Warning("warn msg")
	Warningf("warn %s", "msg")
	Error("error msg")
	Errorf("error %s", "msg")
}

func TestGlobalFunctionsPrintf(t *testing.T) {
	setPrintf()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	Debug("debug msg")
	Debugf("debug %s", "msg")
	Info("info msg")
	Infof("info %s", "msg")
	Warning("warn msg")
	Warningf("warn %s", "msg")
	Error("error msg")
	Errorf("error %s", "msg")
}

func TestVerboseStructured(t *testing.T) {
	setStructured()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	v := Verbose(true)
	v.Info("verbose info")
	v.Infof("verbose %s", "infof")
	v.Infoln("verbose infoln")

	// Verbose(false) should be no-ops
	v2 := Verbose(false)
	v2.Info("should not log")
	v2.Infof("should %s", "not log")
	v2.Infoln("should not log")
}

func TestVerbosePrintf(t *testing.T) {
	setPrintf()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	v := Verbose(true)
	v.Info("verbose info")
	v.Infof("verbose %s", "infof")
	v.Infoln("verbose infoln")

	// Verbose(false) should be no-ops
	v2 := Verbose(false)
	v2.Info("should not log")
	v2.Infof("should %s", "not log")
	v2.Infoln("should not log")
}
