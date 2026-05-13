package mlog_test

import (
	"strings"
	"testing"

	"github.com/odysseythink/mlog"
)

// TestCallerInfoCorrectness verifies that caller info points to the test
// function, not mlog internals.
func TestCallerInfoCorrectness(t *testing.T) {
	var capturedMeta *mlog.LogsinkMeta
	origSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origSinks }()

	mlog.TextSinks = []mlog.TextSink{&testCaptureSink{
		onMeta: func(meta *mlog.LogsinkMeta) {
			capturedMeta = meta
		},
	}}

	// Call Infof directly from this test function.
	mlog.Infof("test message %d", 42)

	if capturedMeta == nil {
		t.Fatal("no meta captured")
	}

	t.Logf("captured file=%q funcname=%q line=%d", capturedMeta.File, capturedMeta.Funcname, capturedMeta.Line)

	// The file should be this test file, not mlog.go.
	if strings.Contains(capturedMeta.File, "mlog.go") {
		t.Errorf("caller file incorrect: got %q, expected test file, not mlog.go", capturedMeta.File)
	}

	// The funcname should contain the test function name, not mlog.Infof.
	if strings.Contains(capturedMeta.Funcname, "Infof") {
		t.Errorf("caller funcname incorrect: got %q, expected test func, not Infof", capturedMeta.Funcname)
	}
	if !strings.Contains(capturedMeta.Funcname, "TestCallerInfoCorrectness") {
		t.Errorf("caller funcname incorrect: got %q, want to contain TestCallerInfoCorrectness", capturedMeta.Funcname)
	}
}

// TestCallerInfoWithNoinline wraps Infof in a noinline function to simulate
// a scenario where Infof is not inlined by the compiler.
//go:noinline
func callInfofNoInline(format string, args ...any) {
	mlog.Infof(format, args...)
}

func TestCallerInfoWithNoInlineWrapper(t *testing.T) {
	var capturedMeta *mlog.LogsinkMeta
	origSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origSinks }()

	mlog.TextSinks = []mlog.TextSink{&testCaptureSink{
		onMeta: func(meta *mlog.LogsinkMeta) {
			capturedMeta = meta
		},
	}}

	// Call Infof through a noinline wrapper.
	callInfofNoInline("test message %d", 42)

	if capturedMeta == nil {
		t.Fatal("no meta captured")
	}

	t.Logf("captured file=%q funcname=%q line=%d", capturedMeta.File, capturedMeta.Funcname, capturedMeta.Line)

	// The file should be this test file, not mlog.go.
	if strings.Contains(capturedMeta.File, "mlog.go") {
		t.Errorf("caller file incorrect: got %q, expected test file, not mlog.go", capturedMeta.File)
	}

	// The funcname should contain the wrapper function name, not mlog.Infof.
	if strings.Contains(capturedMeta.Funcname, "mlog.Infof") {
		t.Errorf("caller funcname incorrect: got %q, expected wrapper func, not mlog.Infof", capturedMeta.Funcname)
	}
	if !strings.Contains(capturedMeta.Funcname, "callInfofNoInline") {
		t.Errorf("caller funcname incorrect: got %q, want to contain callInfofNoInline", capturedMeta.Funcname)
	}
}

type testCaptureSink struct {
	onMeta func(*mlog.LogsinkMeta)
}

func (s *testCaptureSink) Enabled(*mlog.LogsinkMeta) bool { return true }
func (s *testCaptureSink) Emit(meta *mlog.LogsinkMeta, _ []byte) (int, error) {
	if s.onMeta != nil {
		s.onMeta(meta)
	}
	return 0, nil
}
