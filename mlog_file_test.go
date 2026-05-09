package mlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSetLogDir(t *testing.T) {
	orig := *logDir
	defer func() { *logDir = orig }()
	SetLogDir("/tmp/testlog")
	if *logDir != "/tmp/testlog" {
		t.Errorf("logDir = %q, want /tmp/testlog", *logDir)
	}
}

func TestSetLogLevel(t *testing.T) {
	orig := *logBufLevel
	defer func() { *logBufLevel = orig }()
	SetLogLevel(3)
	if *logBufLevel != 3 {
		t.Errorf("logBufLevel = %d, want 3", *logBufLevel)
	}
	SetLogLevel(int32(5))
	if *logBufLevel != 5 {
		t.Errorf("logBufLevel = %d, want 5", *logBufLevel)
	}
}

func TestSetMaxLogSize(t *testing.T) {
	orig := MaxSize
	defer func() { MaxSize = orig }()
	SetMaxLogSize(10)
	if MaxSize != 10*1024*1024 {
		t.Errorf("MaxSize = %d, want %d", MaxSize, 10*1024*1024)
	}
}

func TestPathExist(t *testing.T) {
	if !pathExist(".") {
		t.Error("pathExist(.) should be true")
	}
	if pathExist("/nonexistent/path/12345") {
		t.Error("pathExist(nonexistent) should be false")
	}
}

func TestShortHostname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"www.google.com", "www"},
		{"localhost", "localhost"},
		{"", ""},
	}
	for _, tc := range tests {
		got := shortHostname(tc.input)
		if got != tc.want {
			t.Errorf("shortHostname(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStderrSinkEnabled(t *testing.T) {
	origToStderr := toStderr
	origAlsoToStderr := alsoToStderr
	defer func() {
		toStderr = origToStderr
		alsoToStderr = origAlsoToStderr
	}()

	s := &stderrSink{}
	toStderr = false
	alsoToStderr = false
	meta := &LogsinkMeta{Severity: Severity_Info}
	if s.Enabled(meta) {
		t.Error("expected false when toStderr=false, alsoToStderr=false, severity=INFO")
	}

	toStderr = true
	if !s.Enabled(meta) {
		t.Error("expected true when toStderr=true")
	}
}

func TestLogNameContainsPid(t *testing.T) {
	now := time.Now()
	name, _ := logName("INFO", now)
	wantSuffix := fmt.Sprintf(".%09d.%d.log", now.Nanosecond(), pid)
	if !strings.HasSuffix(name, wantSuffix) {
		t.Errorf("logName suffix = %q, want suffix %q", name, wantSuffix)
	}
}

func TestStderrSinkEmit(t *testing.T) {
	var buf bytes.Buffer
	s := &stderrSink{w: &buf}
	meta := &LogsinkMeta{Severity: Severity_Info}
	n, err := s.Emit(meta, []byte("test\n"))
	if err != nil {
		t.Fatalf("Emit error: %v", err)
	}
	if n != len("test\n") {
		t.Errorf("n = %d, want %d", n, len("test\n"))
	}
	if buf.String() != "test\n" {
		t.Errorf("buf = %q, want %q", buf.String(), "test\n")
	}
}

func TestSyncBufferFilenames(t *testing.T) {
	sb := &syncBuffer{names: []string{"a.log", "b.log"}}
	got := sb.filenames()
	if len(got) != 2 || got[0] != "a.log" || got[1] != "b.log" {
		t.Errorf("filenames() = %v, want [a.log b.log]", got)
	}
}

func TestAcquireEntry(t *testing.T) {
	meta := &LogsinkMeta{Severity: Severity_Info}
	entry := acquireEntry([]byte("hello"), meta, 1)
	if string(entry.data) != "hello" {
		t.Errorf("entry.data = %q, want %q", entry.data, "hello")
	}
	if entry.meta != meta {
		t.Error("entry.meta mismatch")
	}
	if entry.refCnt.Load() != 1 {
		t.Errorf("entry.refCnt = %d, want 1", entry.refCnt.Load())
	}
	if entry.ack != nil {
		t.Error("expected nil ack for INFO severity")
	}

	// Error severity should have ack channel
	metaErr := &LogsinkMeta{Severity: Severity_Error}
	entryErr := acquireEntry([]byte("error"), metaErr, 1)
	if entryErr.ack == nil {
		t.Error("expected non-nil ack for ERROR severity")
	}
}

func TestNamesErrNoLog(t *testing.T) {
	// Isolate from tests that may have initialized file sinks.
	orig := sinks.file
	sinks.file = fileSinkSet{}
	defer func() { sinks.file = orig }()

	// Before any log files are created, Names should return ErrNoLog.
	_, err := Names("INFO")
	if !errors.Is(err, ErrNoLog) {
		t.Errorf("Names(\"INFO\") error = %v, want ErrNoLog", err)
	}
}

func TestNamesInvalidSeverity(t *testing.T) {
	_, err := Names("INVALID")
	if err == nil {
		t.Error("Names(\"INVALID\") expected error")
	}
}

func TestFlushCloseNoWriters(t *testing.T) {
	// Flush and Close should not panic when no writers exist.
	Flush()
	Close()
}

func TestCreateLogDirs(t *testing.T) {
	origLogDir := *logDir
	origLogDirs := logDirs
	defer func() {
		*logDir = origLogDir
		logDirs = origLogDirs
	}()

	*logDir = os.TempDir()
	logDirs = nil
	createLogDirs()
	if len(logDirs) == 0 {
		t.Error("createLogDirs should populate logDirs")
	}
}

func TestFileSinkSetEnabled(t *testing.T) {
	origToStderr := toStderr
	origLogDir := *logDir
	defer func() {
		toStderr = origToStderr
		*logDir = origLogDir
	}()

	fss := &fileSinkSet{}
	toStderr = false
	*logDir = "/var/log/test"
	meta := &LogsinkMeta{Severity: Severity_Info}
	if !fss.Enabled(meta) {
		t.Error("fileSinkSet.Enabled should be true when toStderr=false and logDir is set")
	}

	toStderr = true
	if fss.Enabled(meta) {
		t.Error("fileSinkSet.Enabled should be false when toStderr=true")
	}

	*logDir = ""
	toStderr = false
	if fss.Enabled(meta) {
		t.Error("fileSinkSet.Enabled should be false when logDir is empty")
	}
}

func TestFileSinkDisabledByDefault(t *testing.T) {
	// 默认 toStderr=true, logDir=""
	fss := &fileSinkSet{}
	meta := &LogsinkMeta{Severity: Severity_Info}
	if fss.Enabled(meta) {
		t.Error("fileSinkSet should be disabled by default when logDir is empty")
	}
}

func TestSetOutput(t *testing.T) {
	var buf bytes.Buffer
	origToStderr := toStderr
	origWriter := sinks.stderr.w
	defer func() {
		toStderr = origToStderr
		sinks.stderr.mu.Lock()
		sinks.stderr.w = origWriter
		sinks.stderr.mu.Unlock()
	}()

	SetOutput(&buf)
	if !toStderr {
		t.Error("toStderr should be true after SetOutput")
	}
	if sinks.stderr.w != &buf {
		t.Error("sinks.stderr.w should point to the provided writer")
	}
}

func TestSetLevel(t *testing.T) {
	orig := *logBufLevel
	defer func() { *logBufLevel = orig }()

	SetLevel(7)
	if *logBufLevel != 7 {
		t.Errorf("logBufLevel = %d, want 7", *logBufLevel)
	}
	SetLevel(int64(9))
	if *logBufLevel != 9 {
		t.Errorf("logBufLevel = %d, want 9", *logBufLevel)
	}
}
