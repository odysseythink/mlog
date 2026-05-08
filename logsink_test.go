package mlog_test

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/odysseythink/mlog"
)

// A savingTextSink saves the data argument of the last Emit call made to it.
type savingTextSink struct{ data []byte }

func (savingTextSink) Enabled(*mlog.LogsinkMeta) bool { return true }
func (s *savingTextSink) Emit(meta *mlog.LogsinkMeta, data []byte) (n int, err error) {
	s.data = slices.Clone(data)
	return len(data), nil
}

func TestThreadPadding(t *testing.T) {
	originalSinks := mlog.StructuredSinks
	defer func() { mlog.StructuredSinks = originalSinks }()
	var sink savingTextSink
	mlog.TextSinks = []mlog.TextSink{&sink}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	const msg = "DOOMBAH!"

	for _, tc := range [...]struct {
		n    uint64
		want []byte
	}{
		// Integers that encode as fewer than 7 ASCII characters are padded, the
		// rest is not; see nDigits(). Format is [nDigits(7, thread)].
		{want: []byte("[       ]"), n: 0}, // nDigits does not support 0 (I presume for speed reasons).
		{want: []byte("[      1]"), n: 1},
		{want: []byte("[ 912389]"), n: 912389},
		{want: []byte("[2147483648]"), n: math.MaxInt32 + 1},
		{want: []byte("[9223372036854775806]"), n: math.MaxInt64 - 1},
		{want: []byte("[9223372036854775808]"), n: math.MaxInt64 + 1},   // Test int64 overflow.
		{want: []byte("[9223372036854775817]"), n: math.MaxInt64 + 10},  // Test int64 overflow.
		{want: []byte("[18446744073709551614]"), n: math.MaxUint64 - 1}, // Test int64 overflow.
	} {
		meta.Thread = int64(tc.n)
		mlog.LogsinkPrintf(meta, "%v", msg)
		t.Logf(`LogsinkPrintf(%+v, "%%v", %q)`, meta, msg)

		// Check if the needle is present exactly.
		if !bytes.Contains(sink.data, tc.want) {
			t.Errorf("needle = '%s' not found in %s", tc.want, sink.data)
		}
	}
}

func TestFatalMessage(t *testing.T) {
	const msg = "DOOOOOOM!"

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Fatal,
	}

	mlog.LogsinkPrintf(meta, "%v", msg)
	t.Logf(`LogsinkPrintf(%+v, "%%v", %q)`, meta, msg)

	gotMeta, gotMsg, ok := mlog.LogsinkFatalMessage()
	if !ok || !reflect.DeepEqual(gotMeta, meta) || !bytes.Contains(gotMsg, []byte(msg)) {
		t.Errorf("logsink.FatalMessage() = %+v, %q, %v", gotMeta, gotMsg, ok)
	}
}

func BenchmarkStructuredSink(b *testing.B) {
	// Replace global TextSinks to avoid writing to real files (fileSinkSet
	// uses O_EXCL which fails when the same-second filename already exists).
	originalTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = originalTextSinks }()
	mlog.TextSinks = nil

	// Reset mlog.StructuredSinks at the end of the benchmark.
	// Each benchmark case will clear it and insert its own test sink.
	originalSinks := mlog.StructuredSinks
	defer func() {
		mlog.StructuredSinks = originalSinks
	}()

	noop := noopStructuredSink{}
	noopWS := noopStructuredSinkWantStack{}
	stringWS := stringStructuredSinkWantStack{}

	_, file, line, _ := runtime.Caller(0)
	stack := mlog.StackdumpCaller(0)
	genMeta := func(dump *mlog.Stack) *mlog.LogsinkMeta {
		return &mlog.LogsinkMeta{
			Time:     time.Now(),
			File:     file,
			Line:     line,
			Severity: mlog.Severity_Warning,
			Thread:   1240,
			Stack:    dump,
		}
	}

	for _, test := range []struct {
		name  string
		sinks []mlog.StructuredLogsink
		meta  *mlog.LogsinkMeta
	}{
		{name: "meta_nostack_01_sinks_00_want_stack_pconly", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noop}},
		{name: "meta___stack_01_sinks_01_want_stack_pconly", meta: genMeta(&stack), sinks: []mlog.StructuredLogsink{noopWS}},
		{name: "meta_nostack_01_sinks_01_want_stack_pconly", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noopWS}},
		{name: "meta_nostack_01_sinks_01_want_stack_string", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{stringWS}},
		{name: "meta_nostack_02_sinks_01_want_stack_pconly", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noopWS, noop}},
		{name: "meta_nostack_02_sinks_02_want_stack_string", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{stringWS, stringWS}},
		{name: "meta_nostack_10_sinks_00_want_stack_pconly", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noop, noop, noop, noop, noop, noop, noop, noop, noop, noop}},
		{name: "meta_nostack_10_sinks_05_want_stack_pconly", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noop, noopWS, noop, noop, noopWS, noop, noopWS, noopWS, noopWS, noop}},
		{name: "meta_nostack_10_sinks_05_want_stack_string", meta: genMeta(nil), sinks: []mlog.StructuredLogsink{noop, stringWS, noop, noop, stringWS, noop, stringWS, stringWS, stringWS, noop}},
		{name: "meta___stack_10_sinks_05_want_stack_pconly", meta: genMeta(&stack), sinks: []mlog.StructuredLogsink{noop, noopWS, noop, noop, noopWS, noop, noopWS, noopWS, noopWS, noop}},
		{name: "meta___stack_10_sinks_05_want_stack_string", meta: genMeta(&stack), sinks: []mlog.StructuredLogsink{noop, stringWS, noop, noop, stringWS, noop, stringWS, stringWS, stringWS, noop}},
	} {
		b.Run(test.name, func(b *testing.B) {
			mlog.StructuredSinks = test.sinks
			savedStack := test.meta.Stack

			args := []any{1} // Pre-allocate args slice to avoid allocation in benchmark loop.

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := mlog.LogsinkPrintf(test.meta, "test %d", args...)
				if err != nil {
					b.Fatalf("mlog.LogsinkPrintf(): didn't expect any error while benchmarking, got %v", err)
				}
				// mlog.LogsinkPrintf modifies Meta.Depth, which is used during stack
				// collection. If we don't reset it, stacks quickly become empty, making
				// the benchmark useless.
				test.meta.Depth = 0
				// There is a possible optimization where mlog.LogsinkPrintf will avoid
				// allocating a new meta and modify it in-place if it needs a stack.
				// This would throw off benchmarks as subsequent invocations would
				// re-use this stack. Since we know this memoization/modification only
				// happens with stacks, reset it manually to avoid skewing allocation
				// numbers.
				test.meta.Stack = savedStack
			}
		})
	}
}

// testStructuredSinkAndWants contains a StructuredSink under test
// and its wanted values. The struct is created to help with testing
// multiple StructuredSinks for Printf().
type testStructuredSinkAndWants struct {
	// The sink under test.
	sink testStructuredSink
	// Whether this sink should want stack in its meta.
	// Only set when the sink is fakeStructuredSinkThatWantsStack.
	wantStack bool
	// If this sink wants stack, the expected stack.
	// Only set when the sink is fakeStructuredSinkThatWantsStack and returns true for WantStack().
	wantStackEqual *mlog.Stack
}

type testStructuredSink interface {
	mlog.StructuredLogsink

	GotMeta() *mlog.LogsinkMeta
	GotFormat() string
	GotArgs() []any
	Calls() int
}

type fakeStructuredSink struct {
	// err is returned by Printf().
	err error
	// gotMeta is the Meta passed to the last Printf() call.
	gotMeta *mlog.LogsinkMeta
	// gotFormat is the format string passed to the last Printf() call.
	gotFormat string
	// gotArgs are the arguments passed to the last Printf() call.
	gotArgs []any
	// calls is a counter of the number of times Printf() has been called.
	calls int
}

func (s *fakeStructuredSink) GotMeta() *mlog.LogsinkMeta {
	return s.gotMeta
}

func (s *fakeStructuredSink) GotFormat() string {
	return s.gotFormat
}

func (s *fakeStructuredSink) GotArgs() []any {
	return s.gotArgs
}

func (s *fakeStructuredSink) Calls() int {
	return s.calls
}

func (s *fakeStructuredSink) Printf(meta *mlog.LogsinkMeta, format string, a ...any) (n int, err error) {
	s.gotMeta = meta
	s.gotFormat = format
	s.gotArgs = a
	s.calls++
	return 0, s.err
}

type fakeStructuredSinkThatWantsStack struct {
	fakeStructuredSink
	// wantStack controls what the WantStack() method returns.
	wantStack bool
}

func (s *fakeStructuredSinkThatWantsStack) WantStack(meta *mlog.LogsinkMeta) bool {
	return s.wantStack
}

type noopStructuredSink struct{}

func (s noopStructuredSink) Printf(meta *mlog.LogsinkMeta, format string, a ...any) (n int, err error) {
	return 0, nil
}

type noopStructuredSinkWantStack struct{}

func (s noopStructuredSinkWantStack) WantStack(_ *mlog.LogsinkMeta) bool { return true }
func (s noopStructuredSinkWantStack) Printf(meta *mlog.LogsinkMeta, format string, a ...any) (n int, err error) {
	return 0, nil
}

type stringStructuredSinkWantStack struct{}

func (s stringStructuredSinkWantStack) WantStack(_ *mlog.LogsinkMeta) bool { return true }
func (s stringStructuredSinkWantStack) Printf(meta *mlog.LogsinkMeta, format string, a ...any) (n int, err error) {
	return len(meta.Stack.String()), nil
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		name    string
		want    mlog.Severity
		wantErr bool
	}{
		{"INFO", mlog.Severity_Info, false},
		{"info", mlog.Severity_Info, false},
		{"Info", mlog.Severity_Info, false},
		{"WARNING", mlog.Severity_Warning, false},
		{"ERROR", mlog.Severity_Error, false},
		{"FATAL", mlog.Severity_Fatal, false},
		{"DEBUG", mlog.Severity(0), true},
		{"INVALID", mlog.Severity(0), true},
		{"", mlog.Severity(0), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mlog.ParseSeverity(tc.name)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSeverity(%q) expected error", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSeverity(%q) unexpected error: %v", tc.name, err)
			}
			if got != tc.want {
				t.Fatalf("ParseSeverity(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  mlog.Severity
		want string
	}{
		{mlog.Severity_Debug, "DEBUG"},
		{mlog.Severity_Info, "INFO"},
		{mlog.Severity_Warning, "WARNING"},
		{mlog.Severity_Error, "ERROR"},
		{mlog.Severity_Fatal, "FATAL"},
		{mlog.Severity(99), "mlog.Severity(99)"},
	}
	for _, tc := range tests {
		got := tc.sev.String()
		if got != tc.want {
			t.Fatalf("Severity(%d).String() = %q, want %q", tc.sev, got, tc.want)
		}
	}
}

func TestLogsinkPrintfNoEnabledSinks(t *testing.T) {
	origTextSinks := mlog.TextSinks
	origStructuredSinks := mlog.StructuredSinks
	mlog.TextSinks = []mlog.TextSink{&fakeTextSink{enabled: false}}
	mlog.StructuredSinks = nil
	defer func() {
		mlog.TextSinks = origTextSinks
		mlog.StructuredSinks = origStructuredSinks
	}()

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	n, err := mlog.LogsinkPrintf(meta, "hello")
	if err != nil {
		t.Fatalf("LogsinkPrintf() unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("LogsinkPrintf() n = %d, want 0", n)
	}
}

func TestLogsinkPrintfMaxMessageLen(t *testing.T) {
	origTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origTextSinks }()
	sink := &fakeTextSink{enabled: true}
	mlog.TextSinks = []mlog.TextSink{sink}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	longMsg := strings.Repeat("x", mlog.MaxLogMessageLen)
	mlog.LogsinkPrintf(meta, "%s", longMsg)

	if len(sink.gotBytes) != mlog.MaxLogMessageLen {
		t.Fatalf("expected %d bytes, got %d", mlog.MaxLogMessageLen, len(sink.gotBytes))
	}
	if sink.gotBytes[len(sink.gotBytes)-1] != '\n' {
		t.Fatalf("expected trailing newline")
	}
}

func TestLogsinkPrintfNoTrailingNewline(t *testing.T) {
	origTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origTextSinks }()
	sink := &fakeTextSink{enabled: true}
	mlog.TextSinks = []mlog.TextSink{sink}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	mlog.LogsinkPrintf(meta, "hello")
	if !bytes.HasSuffix(sink.gotBytes, []byte("hello\n")) {
		t.Fatalf("expected auto-appended newline, got %q", sink.gotBytes)
	}
}

func TestLogsinkPrintfMultipleSinks(t *testing.T) {
	origTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origTextSinks }()

	sinks := make([]*fakeTextSink, 4)
	textSinks := make([]mlog.TextSink, 4)
	for i := range sinks {
		sinks[i] = &fakeTextSink{enabled: true}
		textSinks[i] = sinks[i]
	}
	mlog.TextSinks = textSinks

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	mlog.LogsinkPrintf(meta, "multi")

	for i, s := range sinks {
		if s.calls != 1 {
			t.Fatalf("sink %d calls = %d, want 1", i, s.calls)
		}
		if !bytes.Contains(s.gotBytes, []byte("multi")) {
			t.Fatalf("sink %d did not receive message", i)
		}
	}
}

func TestLogsinkPrintfSinkError(t *testing.T) {
	origTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origTextSinks }()

	wantErr := errors.New("sink error")
	sink := &fakeTextSink{enabled: true, err: wantErr}
	mlog.TextSinks = []mlog.TextSink{sink}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	_, err := mlog.LogsinkPrintf(meta, "hello")
	if err != wantErr {
		t.Fatalf("LogsinkPrintf() err = %v, want %v", err, wantErr)
	}
}

func TestLogsinkPrintfSinkByteCount(t *testing.T) {
	origTextSinks := mlog.TextSinks
	defer func() { mlog.TextSinks = origTextSinks }()

	sink1 := &fakeTextSink{enabled: true, byteCount: 10}
	sink2 := &fakeTextSink{enabled: true, byteCount: 25}
	sink3 := &fakeTextSink{enabled: true, byteCount: 15}
	mlog.TextSinks = []mlog.TextSink{sink1, sink2, sink3}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	n, err := mlog.LogsinkPrintf(meta, "hello")
	if err != nil {
		t.Fatalf("LogsinkPrintf() unexpected error: %v", err)
	}
	if n != 25 {
		t.Fatalf("LogsinkPrintf() n = %d, want 25", n)
	}
}

func TestLogsinkPrintfStackFromArgs(t *testing.T) {
	origStructuredSinks := mlog.StructuredSinks
	defer func() { mlog.StructuredSinks = origStructuredSinks }()

	sink := &fakeStructuredSinkThatWantsStack{wantStack: true}
	mlog.StructuredSinks = []mlog.StructuredLogsink{sink}

	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	stack := mlog.Stack{Text: []byte("test stack trace")}
	mlog.LogsinkPrintf(meta, "msg %v", stack)

	if sink.gotMeta == nil {
		t.Fatal("sink did not receive meta")
	}
	if sink.gotMeta.Stack == nil {
		t.Fatal("expected Stack to be set in meta")
	}
	if !bytes.Equal(sink.gotMeta.Stack.Text, []byte("test stack trace")) {
		t.Fatalf("unexpected stack text: %q", sink.gotMeta.Stack.Text)
	}
}

func TestLogsinkPrintfStackPreserved(t *testing.T) {
	origStructuredSinks := mlog.StructuredSinks
	defer func() { mlog.StructuredSinks = origStructuredSinks }()

	sink := &fakeStructuredSinkThatWantsStack{wantStack: true}
	mlog.StructuredSinks = []mlog.StructuredLogsink{sink}

	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	_, file, line, _ := runtime.Caller(0)
	existingStack := mlog.Stack{Text: []byte("existing stack")}
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
		Stack:    &existingStack,
	}

	mlog.LogsinkPrintf(meta, "msg")

	if sink.gotMeta == nil {
		t.Fatal("sink did not receive meta")
	}
	if sink.gotMeta.Stack != &existingStack {
		t.Fatal("expected existing Stack to be preserved")
	}
}

func TestLogsinkPrintfStructuredSinkError(t *testing.T) {
	origStructuredSinks := mlog.StructuredSinks
	defer func() { mlog.StructuredSinks = origStructuredSinks }()

	wantErr := errors.New("structured error")
	sink := &fakeStructuredSink{err: wantErr}
	mlog.StructuredSinks = []mlog.StructuredLogsink{sink}

	origTextSinks := mlog.TextSinks
	mlog.TextSinks = nil
	defer func() { mlog.TextSinks = origTextSinks }()

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	_, err := mlog.LogsinkPrintf(meta, "msg")
	if err != wantErr {
		t.Fatalf("LogsinkPrintf() err = %v, want %v", err, wantErr)
	}
}

func TestStructuredTextWrapper(t *testing.T) {
	sink := &fakeTextSink{enabled: true}
	wrapper := mlog.StructuredTextWrapper{TextSinks: []mlog.TextSink{sink}}

	_, file, line, _ := runtime.Caller(0)
	meta := &mlog.LogsinkMeta{
		Time:     time.Now(),
		File:     file,
		Line:     line,
		Severity: mlog.Severity_Info,
	}

	wrapper.Printf(meta, "wrapper msg")

	if sink.calls != 1 {
		t.Fatalf("sink calls = %d, want 1", sink.calls)
	}
	if !bytes.Contains(sink.gotBytes, []byte("wrapper msg")) {
		t.Fatalf("sink did not receive message: %q", sink.gotBytes)
	}
}

type fakeTextSink struct {
	// enabled is returned by Enabled().
	enabled bool
	// byteCount is returned by Emit().
	byteCount int
	// err is returned by Emit().
	err error
	// gotMeta is the Meta passed to the last Emit() call.
	gotMeta *mlog.LogsinkMeta
	// gotBytes is the byte slice passed to the last Emit() call.
	gotBytes []byte
	// calls is a counter of the number of times Emit() has been called.
	calls int
}

func (s *fakeTextSink) Enabled(meta *mlog.LogsinkMeta) bool {
	return s.enabled
}

func (s *fakeTextSink) Emit(meta *mlog.LogsinkMeta, bytes []byte) (n int, err error) {
	s.gotMeta = meta
	s.gotBytes = bytes
	s.calls++
	return s.byteCount, s.err
}
