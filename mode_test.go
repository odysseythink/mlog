package mlog

import (
	"sync"
	"testing"
)

func TestSetLogMode(t *testing.T) {
	// Save original state
	origMode := logMode.Load()

	// Reset for testing
	modeSetOnce = sync.Once{}
	logMode.Store(0)

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
	modeSetOnce = sync.Once{}
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
	modeSetOnce = sync.Once{}
	logMode.Store(0)
	SetLogMode(LogModeStructured)

	// Should not panic in structured mode
	Info("test message", String("key", "value"))
}

func TestGlobalInfofStructuredMode(t *testing.T) {
	modeSetOnce = sync.Once{}
	logMode.Store(0)
	SetLogMode(LogModeStructured)

	Infof("formatted %s", "message")
}

func TestLoggerInfoStructuredMode(t *testing.T) {
	modeSetOnce = sync.Once{}
	logMode.Store(0)
	SetLogMode(LogModeStructured)

	logger := With(String("svc", "test"))
	logger.Info("request", String("path", "/api"))
}

func TestLoggerPrintfMode(t *testing.T) {
	modeSetOnce = sync.Once{}
	logMode.Store(0)
	// Default is printf mode, no SetLogMode call needed

	// Avoid writing to real files to prevent O_EXCL collisions
	// when multiple severities are initialized in the same second.
	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	logger := With(String("svc", "test"))
	logger.Info("request") // Should work in printf mode, ignoring fields
}
