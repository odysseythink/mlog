package mlog

import (
	"bytes"
	"context"
	"log"
	"sync"
	"sync/atomic"
	"testing"
)

func TestVDepth(t *testing.T) {
	origV := vflags.v
	origModule := vflags.module
	origModuleLength := vflags.moduleLength
	defer func() {
		vflags.v = origV
		vflags.module = origModule
		atomic.StoreInt32(&vflags.moduleLength, origModuleLength)
		vflags.moduleLevelCache.Store(&sync.Map{})
	}()

	vflags.module = nil
	atomic.StoreInt32(&vflags.moduleLength, 0)

	// v=0: VDepth(0, 0) true, VDepth(0, 1) false
	atomic.StoreInt32((*int32)(&vflags.v), 0)
	vflags.moduleLevelCache.Store(&sync.Map{})
	if !VDepth(0, 0) {
		t.Error("VDepth(0, 0) should be true when v=0")
	}
	if VDepth(0, 1) {
		t.Error("VDepth(0, 1) should be false when v=0")
	}

	// v=1: VDepth(0, 0) and VDepth(0, 1) true
	atomic.StoreInt32((*int32)(&vflags.v), 1)
	vflags.moduleLevelCache.Store(&sync.Map{})
	if !VDepth(0, 0) {
		t.Error("VDepth(0, 0) should be true when v=1")
	}
	if !VDepth(0, 1) {
		t.Error("VDepth(0, 1) should be true when v=1")
	}
}

func TestVerboseMethods(t *testing.T) {
	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"structured", setStructured},
		{"printf", setPrintf},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mode.set()
			defer resetMode()

			origTextSinks := TextSinks
			TextSinks = nil
			defer func() { TextSinks = origTextSinks }()

			v := Verbose(true)
			v.InfoDepth(0, "verbose info depth")
			v.InfoDepthf(0, "verbose %s", "info depthf")
			// Infof does not check mode; test it anyway.
			v.Infof("verbose %s", "infof")

			// Verbose(false) should be no-ops
			v2 := Verbose(false)
			v2.InfoDepth(0, "should not log")
			v2.InfoDepthf(0, "should %s", "not log")
			v2.Infof("should %s", "not log")
		})
	}
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"structured", setStructured},
		{"printf", setPrintf},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mode.set()
			defer resetMode()

			origTextSinks := TextSinks
			TextSinks = nil
			defer func() { TextSinks = origTextSinks }()

			InfoContext(ctx, "info context")
			InfoContextf(ctx, "info %s", "contextf")
			InfoContextDepth(ctx, 0, "info context depth")
			InfoContextDepthf(ctx, 0, "info %s", "context depthf")
		})
	}
}

func TestDebugContext(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"structured", setStructured},
		{"printf", setPrintf},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mode.set()
			defer resetMode()

			origTextSinks := TextSinks
			TextSinks = nil
			defer func() { TextSinks = origTextSinks }()

			DebugContext(ctx, "debug context")
			DebugContextf(ctx, "debug %s", "contextf")
			DebugContextDepth(ctx, 0, "debug context depth")
			DebugContextDepthf(ctx, 0, "debug %s", "context depthf")
		})
	}
}

func TestWarningContext(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"structured", setStructured},
		{"printf", setPrintf},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mode.set()
			defer resetMode()

			origTextSinks := TextSinks
			TextSinks = nil
			defer func() { TextSinks = origTextSinks }()

			WarningContext(ctx, "warning context")
			WarningContextf(ctx, "warning %s", "contextf")
			WarningContextDepth(ctx, 0, "warning context depth")
			WarningContextDepthf(ctx, 0, "warning %s", "context depthf")
		})
	}
}

func TestErrorContext(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"structured", setStructured},
		{"printf", setPrintf},
	} {
		t.Run(mode.name, func(t *testing.T) {
			mode.set()
			defer resetMode()

			origTextSinks := TextSinks
			TextSinks = nil
			defer func() { TextSinks = origTextSinks }()

			ErrorContext(ctx, "error context")
			ErrorContextf(ctx, "error %s", "contextf")
			ErrorContextDepth(ctx, 0, "error context depth")
			ErrorContextDepthf(ctx, 0, "error %s", "context depthf")
		})
	}
}

func TestInfoDepthf(t *testing.T) {
	setPrintf()
	defer resetMode()

	origTextSinks := TextSinks
	TextSinks = nil
	defer func() { TextSinks = origTextSinks }()

	InfoDepthf(0, "info depthf %s", "test")
}

func TestLinesBytes(t *testing.T) {
	setPrintf()
	defer resetMode()

	// Save and restore stats
	origInfoLines := atomic.LoadInt64(&Stats.Info.lines)
	origInfoBytes := atomic.LoadInt64(&Stats.Info.bytes)
	defer func() {
		atomic.StoreInt64(&Stats.Info.lines, origInfoLines)
		atomic.StoreInt64(&Stats.Info.bytes, origInfoBytes)
	}()

	sink := &testTextSink{enabled: true, n: 10}
	origTextSinks := TextSinks
	TextSinks = []TextSink{sink}
	defer func() { TextSinks = origTextSinks }()

	beforeLines := Stats.Info.Lines()
	beforeBytes := Stats.Info.Bytes()

	Info("test message")

	afterLines := Stats.Info.Lines()
	afterBytes := Stats.Info.Bytes()

	if afterLines != beforeLines+1 {
		t.Errorf("Lines() = %d, want %d", afterLines, beforeLines+1)
	}
	if afterBytes != beforeBytes+10 {
		t.Errorf("Bytes() = %d, want %d", afterBytes, beforeBytes+10)
	}
}

func TestCopyStandardLogTo(t *testing.T) {
	setPrintf()
	defer resetMode()

	sink := &testTextSink{enabled: true, n: 10}
	origTextSinks := TextSinks
	TextSinks = []TextSink{sink}
	defer func() { TextSinks = origTextSinks }()

	// Save original stdLog state
	origFlags := log.Flags()
	origOutput := log.Writer()
	defer func() {
		log.SetFlags(origFlags)
		log.SetOutput(origOutput)
	}()

	CopyStandardLogTo("INFO")
	log.Print("hello from standard log")

	if sink.calls == 0 {
		t.Error("expected sink to receive log from standard log")
	}
}

func TestNewStandardLogger(t *testing.T) {
	setPrintf()
	defer resetMode()

	sink := &testTextSink{enabled: true, n: 10}
	origTextSinks := TextSinks
	TextSinks = []TextSink{sink}
	defer func() { TextSinks = origTextSinks }()

	logger := NewStandardLogger("INFO")
	logger.Print("hello from new standard logger")

	if sink.calls == 0 {
		t.Error("expected sink to receive log from new standard logger")
	}
}

func TestLogBridgeBadFormat(t *testing.T) {
	setPrintf()
	defer resetMode()

	sink := &testTextSink{enabled: true, n: 10}
	origTextSinks := TextSinks
	TextSinks = []TextSink{sink}
	defer func() { TextSinks = origTextSinks }()

	lb := logBridge(Severity_Info)
	n, err := lb.Write([]byte("bad format"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len("bad format") {
		t.Errorf("n = %d, want %d", n, len("bad format"))
	}
	if sink.calls == 0 {
		t.Error("expected sink to receive log even with bad format")
	}
	if !bytes.Contains(sink.gotBytes, []byte("bad log format")) {
		t.Errorf("expected 'bad log format' in output, got %q", sink.gotBytes)
	}
}
