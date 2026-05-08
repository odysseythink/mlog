package mlog

import (
	"bytes"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type testTextSink struct {
	enabled  bool
	calls    int
	gotBytes []byte
	n        int
	err      error
}

func (s *testTextSink) Enabled(*LogsinkMeta) bool { return s.enabled }
func (s *testTextSink) Emit(_ *LogsinkMeta, data []byte) (int, error) {
	s.calls++
	s.gotBytes = append([]byte(nil), data...)
	return s.n, s.err
}

func TestTextPrintfRateLimiterDrop(t *testing.T) {
	origSampler := logSampler.Load()
	defer func() {
		if origSampler != nil {
			logSampler.Store(origSampler)
		} else {
			logSampler.Store((*sampler)(nil))
		}
	}()

	origTextSinks := TextSinks
	defer func() { TextSinks = origTextSinks }()

	// Create a sampler with 0 max tokens so it always denies.
	logSampler.Store(newSampler(1, 0))

	sink := &testTextSink{enabled: true}
	TextSinks = []TextSink{sink}

	droppedBefore := atomic.LoadInt64(&Stats.Dropped.lines)

	_, file, line, _ := runtime.Caller(0)
	meta := &LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: Severity_Info,
	}

	n, err := textPrintf(meta, TextSinks, "hello")
	if err != nil {
		t.Fatalf("textPrintf() unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("textPrintf() n = %d, want 0", n)
	}
	if sink.calls != 0 {
		t.Fatalf("sink calls = %d, want 0", sink.calls)
	}

	droppedAfter := atomic.LoadInt64(&Stats.Dropped.lines)
	if droppedAfter <= droppedBefore {
		t.Fatalf("expected Stats.Dropped.lines to increase, before=%d after=%d", droppedBefore, droppedAfter)
	}
}

func TestTextPrintfFatalNoBufPool(t *testing.T) {
	// Save and restore fatal message so we don't interfere with other tests.
	origFatal := atomic.LoadPointer(&fatalMessage)
	defer atomic.StorePointer(&fatalMessage, origFatal)
	atomic.StorePointer(&fatalMessage, nil)

	origTextSinks := TextSinks
	defer func() { TextSinks = origTextSinks }()

	sink := &testTextSink{enabled: true, n: 1}
	TextSinks = []TextSink{sink}

	_, file, line, _ := runtime.Caller(0)
	meta := &LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: Severity_Fatal,
	}

	n, err := textPrintf(meta, TextSinks, "fatal test message")
	if err != nil {
		t.Fatalf("textPrintf() unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("textPrintf() n = %d, want 1", n)
	}

	gotMeta, gotMsg, ok := LogsinkFatalMessage()
	if !ok {
		t.Fatal("expected fatal message to be stored")
	}
	if gotMeta == nil {
		t.Fatal("expected fatal meta to be non-nil")
	}
	if !bytes.Contains(gotMsg, []byte("fatal test message")) {
		t.Fatalf("unexpected fatal message: %q", gotMsg)
	}
}
