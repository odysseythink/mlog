package mlog_test

import (
	"bytes"
	"math"
	"reflect"
	"runtime"
	"slices"
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
		// rest is not; see nDigits().
		{want: []byte("         "), n: 0}, // nDigits does not support 0 (I presume for speed reasons).
		{want: []byte("       1 "), n: 1},
		{want: []byte("  912389 "), n: 912389},
		{want: []byte(" 2147483648 "), n: math.MaxInt32 + 1},
		{want: []byte(" 9223372036854775806 "), n: math.MaxInt64 - 1},
		{want: []byte(" 9223372036854775808 "), n: math.MaxInt64 + 1},   // Test int64 overflow.
		{want: []byte(" 9223372036854775817 "), n: math.MaxInt64 + 10},  // Test int64 overflow.
		{want: []byte(" 18446744073709551614 "), n: math.MaxUint64 - 1}, // Test int64 overflow.
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
