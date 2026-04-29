// Package mlog implements logging analogous to the Google-internal C++ INFO/ERROR/V setup.
// It provides functions that have a name matched by regex:
//
//	(Info|Warning|Error|Fatal)(Context)?(Depth)?(f)?
//
// If Context is present, function takes context.Context argument. The
// context is used to pass through the Trace Context to log sinks that can make use
// of it.
// It is recommended to use the context variant of the functions over the non-context
// variants if a context is available to make sure the Trace Contexts are present
// in logs.
//
// If Depth is present, this function calls log from a different depth in the call stack.
// This enables a callee to emit logs that use the callsite information of its caller
// or any other callers in the stack. When depth == 0, the original callee's line
// information is emitted. When depth > 0, depth frames are skipped in the call stack
// and the final frame is treated like the original callee to Info.
//
// If 'f' is present, function formats according to a format specifier.
//
// This package also provides V-style logging controlled by the -v and -vmodule=file=2 flags.
//
// Basic examples:
//
//	mlog.Info("Prepare to repel boarders")
//
//	mlog.Fatalf("Initialization failed: %s", err)
//
// See the documentation for the V function for an explanation of these examples:
//
//	if mlog.V(2) {
//		mlog.Info("Starting transaction...")
//	}
//
//	mlog.V(2).Infoln("Processed", nItems, "elements")
//
// Log output is buffered and written periodically using Flush. Programs
// should call Flush before exiting to guarantee all log output is written.
//
// By default, all log statements write to files in a temporary directory.
// This package provides several flags that modify this behavior.
// As a result, flag.Parse must be called before any logging is done.
//
//	-logtostderr=false
//		Logs are written to standard error instead of to files.
//	-alsologtostderr=false
//		Logs are written to standard error as well as to files.
//	-stderrthreshold=ERROR
//		Log events at or above this severity are logged to standard
//		error as well as to files.
//	-log_dir=""
//		Log files will be written to this directory instead of the
//		default temporary directory.
//
// Other flags provide aids to debugging.
//
//	-log_backtrace_at=""
//		A comma-separated list of file and line numbers holding a logging
//		statement, such as
//			-log_backtrace_at=gopherflakes.go:234
//		A stack trace will be written to the Info log whenever execution
//		hits one of these statements. (Unlike with -vmodule, the ".go"
//		must be present.)
//	-v=0
//		Enable V-leveled logging at the specified level.
//	-vmodule=""
//		The syntax of the argument is a comma-separated list of pattern=N,
//		where pattern is a literal file name (minus the ".go" suffix) or
//		"glob" pattern and N is a V level. For instance,
//			-vmodule=gopher*=3
//		sets the V level to 3 in all Go files whose names begin with "gopher",
//		and
//			-vmodule=/path/to/mlog/mlog_test=1
//		sets the V level to 1 in the Go file /path/to/mlog/mlog_test.go.
//		If a glob pattern contains a slash, it is matched against the full path,
//		and the file name. Otherwise, the pattern is
//		matched only against the file's basename.  When both -vmodule and -v
//		are specified, the -vmodule values take precedence for the specified
//		modules.
package mlog

// This file contains the parts of the log package that are shared among all
// implementations (file, envelope, and appengine).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdLog "log"
	"os"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var timeNow = time.Now // Stubbed out for testing.

// MaxSize is the maximum size of a log file in bytes.
var MaxSize uint64 = 1024 * 1024 * 1800

// ErrNoLog is the error we return if no log file has yet been created
// for the specified log type.
var ErrNoLog = errors.New("log file not yet created")

// OutputStats tracks the number of output lines and bytes written.
type OutputStats struct {
	lines int64
	bytes int64
}

// Lines returns the number of lines written.
func (s *OutputStats) Lines() int64 {
	return atomic.LoadInt64(&s.lines)
}

// Bytes returns the number of bytes written.
func (s *OutputStats) Bytes() int64 {
	return atomic.LoadInt64(&s.bytes)
}

// Stats tracks the number of lines of output and number of bytes
// per severity level. Values must be read with atomic.LoadInt64.
var Stats struct {
	Debug, Info, Warning, Error OutputStats
	Dropped                     OutputStats
}

var severityStats = [...]*OutputStats{
	Severity_Debug:   &Stats.Debug,
	Severity_Info:    &Stats.Info,
	Severity_Warning: &Stats.Warning,
	Severity_Error:   &Stats.Error,
	Severity_Fatal:   nil,
}

// Level specifies a level of verbosity for V logs.  The -v flag is of type
// Level and should be modified only through the flag.Value interface.
type Level int32

var metaPool sync.Pool // Pool of *LogsinkMeta.

// metaPoolGet returns a *LogsinkMeta from metaPool as both an interface and a
// pointer, allocating a new one if necessary.  (Returning the interface value
// directly avoids an allocation if there was an existing pointer in the pool.)
func metaPoolGet() (any, *LogsinkMeta) {
	if metai := metaPool.Get(); metai != nil {
		return metai, metai.(*LogsinkMeta)
	}
	meta := new(LogsinkMeta)
	return meta, meta
}

func appendBacktrace(depth int, format string, args []any) (string, []any) {
	// Capture a backtrace as a Stack (both text and PC slice).
	// Structured log sinks can extract the backtrace in whichever format they
	// prefer (PCs or text), and Text sinks will include it as just another part
	// of the log message.
	//
	// Use depth instead of depth+1 so that the backtrace always includes the
	// log function itself - otherwise the reason for the trace appearing in the
	// log may not be obvious to the reader.
	dump := StackdumpCaller(depth)

	// Add an arg and an entry in the format string for the stack dump.
	//
	// Copy the "args" slice to avoid a rare but serious aliasing bug
	// (corrupting the caller's slice if they passed it to a non-Fatal call
	// using "...").
	format = format + "\n\n%v\n"
	args = append(append([]any(nil), args...), dump)

	return format, args
}

// logf acts as ctxlogf, but doesn't expect a context.
func logf(depth int, severity Severity, verbose bool, stack stack, format string, args ...any) {
	ctxlogf(nil, depth+1, severity, verbose, stack, format, args...)
}

// ctxlogf writes a log message for a log function call (or log function wrapper)
// at the given depth in the current goroutine's stack.
func ctxlogf(ctx context.Context, depth int, severity Severity, verbose bool, stack stack, format string, args ...any) {
	funcname := ""
	now := timeNow()
	pc, file, line, ok := runtime.Caller(depth + 1)
	if !ok {
		file = "???"
		line = 1
		funcname = "???"
	} else {
		slash := strings.LastIndex(file, "/")
		if slash >= 0 {
			file = file[slash+1:]
		}
		funcname = runtime.FuncForPC(pc).Name()
		tmplist := strings.Split(funcname, "/")
		if strings.Contains(tmplist[len(tmplist)-1], ".") {
			tmplist = strings.Split(tmplist[len(tmplist)-1], ".")
			funcname = tmplist[len(tmplist)-1]
		} else {
			funcname = tmplist[len(tmplist)-1]
		}
	}

	if stack == withStack || backtraceAt(file, line) {
		format, args = appendBacktrace(depth+1, format, args)
	}
	metai, meta := metaPoolGet()
	*meta = LogsinkMeta{
		Funcname: funcname,
		Context:  ctx,
		Time:     now,
		File:     file,
		Line:     line,
		Depth:    depth + 1,
		Severity: severity,
		Verbose:  verbose,
		Thread:   int64(pid),
	}
	sinkf(meta, format, args...)
	// Clear pointer fields so they can be garbage collected early.
	meta.Context = nil
	meta.Stack = nil
	metaPool.Put(metai)
}

var sinkErrOnce sync.Once

func sinkf(meta *LogsinkMeta, format string, args ...any) {
	meta.Depth++
	n, err := LogsinkPrintf(meta, format, args...)
	if stats := severityStats[meta.Severity]; stats != nil {
		atomic.AddInt64(&stats.lines, 1)
		atomic.AddInt64(&stats.bytes, int64(n))
	}

	if err != nil {
		// Best-effort to generate a reasonable Fatalf-like
		// error message in all sinks that are still here for
		// the first goroutine that comes here and terminate
		// the process.
		sinkErrOnce.Do(func() {
			m := &LogsinkMeta{}
			m.Time = timeNow()
			m.Severity = Severity_Fatal
			m.Thread = int64(pid)
			_, m.File, m.Line, _ = runtime.Caller(0)
			format, args := appendBacktrace(1, "log: exiting because of error writing previous log to sinks: %v", []any{err})
			LogsinkPrintf(m, format, args...)
			flushAndAbort()
		})
	}
}

// CopyStandardLogTo arranges for messages written to the Go "log" package's
// default logs to also appear in the Google logs for the named and lower
// severities.  Subsequent changes to the standard log's default output location
// or format may break this behavior.
//
// Valid names are "INFO", "WARNING", "ERROR", and "FATAL".  If the name is not
// recognized, CopyStandardLogTo panics.
func CopyStandardLogTo(name string) {
	sev, err := ParseSeverity(name)
	if err != nil {
		panic(fmt.Sprintf("log.CopyStandardLogTo(%q): %v", name, err))
	}
	// Set a log format that captures the user's file and line:
	//   d.go:23: message
	stdLog.SetFlags(stdLog.Lshortfile)
	stdLog.SetOutput(logBridge(sev))
}

// NewStandardLogger returns a Logger that writes to the Google logs for the
// named and lower severities.
//
// Valid names are "INFO", "WARNING", "ERROR", and "FATAL". If the name is not
// recognized, NewStandardLogger panics.
func NewStandardLogger(name string) *stdLog.Logger {
	sev, err := ParseSeverity(name)
	if err != nil {
		panic(fmt.Sprintf("log.NewStandardLogger(%q): %v", name, err))
	}
	return stdLog.New(logBridge(sev), "", stdLog.Lshortfile)
}

// logBridge provides the Write method that enables CopyStandardLogTo to connect
// Go's standard logs to the logs provided by this package.
type logBridge Severity

// Write parses the standard logging line and passes its components to the
// logger for severity(lb).
func (lb logBridge) Write(b []byte) (n int, err error) {
	var (
		file = "???"
		line = 1
		text string
	)
	// Split "d.go:23: message" into "d.go", "23", and "message".
	if parts := bytes.SplitN(b, []byte{':'}, 3); len(parts) != 3 || len(parts[0]) < 1 || len(parts[2]) < 1 {
		text = fmt.Sprintf("bad log format: %s", b)
	} else {
		file = string(parts[0])
		text = string(parts[2][1:]) // skip leading space
		line, err = strconv.Atoi(string(parts[1]))
		if err != nil {
			text = fmt.Sprintf("bad line number: %s", b)
			line = 1
		}
	}

	// The depth below hard-codes details of how stdlog gets here.  The alternative would be to walk
	// up the stack looking for src/log/log.go but that seems like it would be
	// unfortunately slow.
	const stdLogDepth = 4

	metai, meta := metaPoolGet()
	*meta = LogsinkMeta{
		Time:     timeNow(),
		File:     file,
		Line:     line,
		Depth:    stdLogDepth,
		Severity: Severity(lb),
		Thread:   int64(pid),
	}

	format := "%s"
	args := []any{text}
	if backtraceAt(file, line) {
		format, args = appendBacktrace(meta.Depth, format, args)
	}

	sinkf(meta, format, args...)
	metaPool.Put(metai)

	return len(b), nil
}

// defaultFormat returns a fmt.Printf format specifier that formats its
// arguments as if they were passed to fmt.Print.
func defaultFormat(args []any) string {
	n := len(args)
	switch n {
	case 0:
		return ""
	case 1:
		return "%v"
	}

	b := make([]byte, 0, n*3-1)
	wasString := true // Suppress leading space.
	for _, arg := range args {
		isString := arg != nil && reflect.TypeOf(arg).Kind() == reflect.String
		if wasString || isString {
			b = append(b, "%v"...)
		} else {
			b = append(b, " %v"...)
		}
		wasString = isString
	}
	return string(b)
}

// lnFormat returns a fmt.Printf format specifier that formats its arguments
// as if they were passed to fmt.Println.
func lnFormat(args []any) string {
	if len(args) == 0 {
		return "\n"
	}

	b := make([]byte, 0, len(args)*3)
	for range args {
		b = append(b, "%v "...)
	}
	b[len(b)-1] = '\n' // Replace the last space with a newline.
	return string(b)
}

// Verbose is a boolean type that implements Infof (like Printf) etc.
// See the documentation of V for more information.
type Verbose bool

// V reports whether verbosity at the call site is at least the requested level.
// The returned value is a boolean of type Verbose, which implements Info, Infoln
// and Infof. These methods will write to the Info log if called.
// Thus, one may write either
//
//	if mlog.V(2) { mlog.Info("log this") }
//
// or
//
//	mlog.V(2).Info("log this")
//
// The second form is shorter but the first is cheaper if logging is off because it does
// not evaluate its arguments.
//
// Whether an individual call to V generates a log record depends on the setting of
// the -v and --vmodule flags; both are off by default. If the level in the call to
// V is at most the value of -v, or of -vmodule for the source file containing the
// call, the V call will log.
func V(level Level) Verbose {
	return VDepth(1, level)
}

// VDepth acts as V but uses depth to determine which call frame to check vmodule for.
// VDepth(0, level) is the same as V(level).
func VDepth(depth int, level Level) Verbose {
	return Verbose(verboseEnabled(depth+1, level))
}

// Info is equivalent to the global Info function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) Info(args ...any) {
	v.InfoDepth(1, args...)
}

// InfoDepth is equivalent to the global InfoDepth function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoDepth(depth int, args ...any) {
	if v {
		logf(depth+1, Severity_Info, true, noStack, defaultFormat(args), args...)
	}
}

// InfoDepthf is equivalent to the global InfoDepthf function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoDepthf(depth int, format string, args ...any) {
	if v {
		logf(depth+1, Severity_Info, true, noStack, format, args...)
	}
}

// Infoln is equivalent to the global Infoln function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) Infoln(args ...any) {
	if v {
		logf(1, Severity_Info, true, noStack, lnFormat(args), args...)
	}
}

// Infof is equivalent to the global Infof function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) Infof(format string, args ...any) {
	if v {
		logf(1, Severity_Info, true, noStack, format, args...)
	}
}

// InfoContext is equivalent to the global InfoContext function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoContext(ctx context.Context, args ...any) {
	v.InfoContextDepth(ctx, 1, args...)
}

// InfoContextf is equivalent to the global InfoContextf function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoContextf(ctx context.Context, format string, args ...any) {
	if v {
		ctxlogf(ctx, 1, Severity_Info, true, noStack, format, args...)
	}
}

// InfoContextDepth is equivalent to the global InfoContextDepth function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoContextDepth(ctx context.Context, depth int, args ...any) {
	if v {
		ctxlogf(ctx, depth+1, Severity_Info, true, noStack, defaultFormat(args), args...)
	}
}

// InfoContextDepthf is equivalent to the global InfoContextDepthf function, guarded by the value of v.
// See the documentation of V for usage.
func (v Verbose) InfoContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	if v {
		ctxlogf(ctx, depth+1, Severity_Info, true, noStack, format, args...)
	}
}

// Debug logs to the DEBUG log.
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Debug(args ...any) {
	DebugDepth(1, args...)
}

// DebugDepth calls Debug from a different depth in the call stack.
// This enables a callee to emit logs that use the callsite information of its caller
// or any other callers in the stack. When depth == 0, the original callee's line
// information is emitted. When depth > 0, depth frames are skipped in the call stack
// and the final frame is treated like the original callee to Debug.
func DebugDepth(depth int, args ...any) {
	logf(depth+1, Severity_Debug, false, noStack, defaultFormat(args), args...)
}

// DebugDepthf acts as DebugDepth but with format string.
func DebugDepthf(depth int, format string, args ...any) {
	logf(depth+1, Severity_Debug, false, noStack, format, args...)
}

// Debugln logs to the DEBUG log.
// Arguments are handled in the manner of fmt.Println; a newline is appended if missing.
func Debugln(args ...any) {
	logf(1, Severity_Debug, false, noStack, lnFormat(args), args...)
}

// Debugf logs to the DEBUG log.
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Debugf(format string, args ...any) {
	logf(1, Severity_Debug, false, noStack, format, args...)
}

// DebugContext is like [Debug], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func DebugContext(ctx context.Context, args ...any) {
	DebugContextDepth(ctx, 1, args...)
}

// DebugContextf is like [Debugf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func DebugContextf(ctx context.Context, format string, args ...any) {
	ctxlogf(ctx, 1, Severity_Debug, false, noStack, format, args...)
}

// DebugContextDepth is like [DebugDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func DebugContextDepth(ctx context.Context, depth int, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Debug, false, noStack, defaultFormat(args), args...)
}

// DebugContextDepthf is like [DebugDepthf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func DebugContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Debug, false, noStack, format, args...)
}

// Info logs to the INFO log.
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Info(args ...any) {
	InfoDepth(1, args...)
}

// InfoDepth calls Info from a different depth in the call stack.
// This enables a callee to emit logs that use the callsite information of its caller
// or any other callers in the stack. When depth == 0, the original callee's line
// information is emitted. When depth > 0, depth frames are skipped in the call stack
// and the final frame is treated like the original callee to Info.
func InfoDepth(depth int, args ...any) {
	logf(depth+1, Severity_Info, false, noStack, defaultFormat(args), args...)
}

// InfoDepthf acts as InfoDepth but with format string.
func InfoDepthf(depth int, format string, args ...any) {
	logf(depth+1, Severity_Info, false, noStack, format, args...)
}

// Infoln logs to the INFO log.
// Arguments are handled in the manner of fmt.Println; a newline is appended if missing.
func Infoln(args ...any) {
	logf(1, Severity_Info, false, noStack, lnFormat(args), args...)
}

// Infof logs to the INFO log.
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Infof(format string, args ...any) {
	logf(1, Severity_Info, false, noStack, format, args...)
}

// InfoContext is like [Info], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func InfoContext(ctx context.Context, args ...any) {
	InfoContextDepth(ctx, 1, args...)
}

// InfoContextf is like [Infof], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func InfoContextf(ctx context.Context, format string, args ...any) {
	ctxlogf(ctx, 1, Severity_Info, false, noStack, format, args...)
}

// InfoContextDepth is like [InfoDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func InfoContextDepth(ctx context.Context, depth int, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Info, false, noStack, defaultFormat(args), args...)
}

// InfoContextDepthf is like [InfoDepthf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func InfoContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Info, false, noStack, format, args...)
}

// Warning logs to the WARNING and INFO logs.
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Warning(args ...any) {
	WarningDepth(1, args...)
}

// WarningDepth acts as Warning but uses depth to determine which call frame to log.
// WarningDepth(0, "msg") is the same as Warning("msg").
func WarningDepth(depth int, args ...any) {
	logf(depth+1, Severity_Warning, false, noStack, defaultFormat(args), args...)
}

// WarningDepthf acts as Warningf but uses depth to determine which call frame to log.
// WarningDepthf(0, "msg") is the same as Warningf("msg").
func WarningDepthf(depth int, format string, args ...any) {
	logf(depth+1, Severity_Warning, false, noStack, format, args...)
}

// Warningln logs to the WARNING and INFO logs.
// Arguments are handled in the manner of fmt.Println; a newline is appended if missing.
func Warningln(args ...any) {
	logf(1, Severity_Warning, false, noStack, lnFormat(args), args...)
}

// Warningf logs to the WARNING and INFO logs.
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Warningf(format string, args ...any) {
	logf(1, Severity_Warning, false, noStack, format, args...)
}

// WarningContext is like [Warning], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func WarningContext(ctx context.Context, args ...any) {
	WarningContextDepth(ctx, 1, args...)
}

// WarningContextf is like [Warningf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func WarningContextf(ctx context.Context, format string, args ...any) {
	ctxlogf(ctx, 1, Severity_Warning, false, noStack, format, args...)
}

// WarningContextDepth is like [WarningDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func WarningContextDepth(ctx context.Context, depth int, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Warning, false, noStack, defaultFormat(args), args...)
}

// WarningContextDepthf is like [WarningDepthf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func WarningContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Warning, false, noStack, format, args...)
}

// Error logs to the ERROR, WARNING, and INFO logs.
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Error(args ...any) {
	ErrorDepth(1, args...)
}

// ErrorDepth acts as Error but uses depth to determine which call frame to log.
// ErrorDepth(0, "msg") is the same as Error("msg").
func ErrorDepth(depth int, args ...any) {
	logf(depth+1, Severity_Error, false, noStack, defaultFormat(args), args...)
}

// ErrorDepthf acts as Errorf but uses depth to determine which call frame to log.
// ErrorDepthf(0, "msg") is the same as Errorf("msg").
func ErrorDepthf(depth int, format string, args ...any) {
	logf(depth+1, Severity_Error, false, noStack, format, args...)
}

// Errorln logs to the ERROR, WARNING, and INFO logs.
// Arguments are handled in the manner of fmt.Println; a newline is appended if missing.
func Errorln(args ...any) {
	logf(1, Severity_Error, false, noStack, lnFormat(args), args...)
}

// Errorf logs to the ERROR, WARNING, and INFO logs.
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Errorf(format string, args ...any) {
	logf(1, Severity_Error, false, noStack, format, args...)
}

// ErrorContext is like [Error], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ErrorContext(ctx context.Context, args ...any) {
	ErrorContextDepth(ctx, 1, args...)
}

// ErrorContextf is like [Errorf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ErrorContextf(ctx context.Context, format string, args ...any) {
	ctxlogf(ctx, 1, Severity_Error, false, noStack, format, args...)
}

// ErrorContextDepth is like [ErrorDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ErrorContextDepth(ctx context.Context, depth int, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Error, false, noStack, defaultFormat(args), args...)
}

// ErrorContextDepthf is like [ErrorDepthf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ErrorContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Error, false, noStack, format, args...)
}

func ctxfatalf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Fatal, false, withStack, format, args...)
	flushAndAbort()
}

func flushAndAbort() {
	Flush()

	err := abortProcess() // Should not return.

	// Failed to abort the process using signals.  Dump a stack trace and exit.
	Errorf("abortProcess returned unexpectedly: %v", err)
	Flush()
	pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
	os.Exit(2) // Exit with the same code as the default SIGABRT handler.
}

func fatalf(depth int, format string, args ...any) {
	ctxfatalf(nil, depth+1, format, args...)
}

// Fatal logs to the FATAL, ERROR, WARNING, and INFO logs,
// including a stack trace of all running goroutines, then calls os.Exit(2).
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Fatal(args ...any) {
	FatalDepth(1, args...)
}

// FatalDepth acts as Fatal but uses depth to determine which call frame to log.
// FatalDepth(0, "msg") is the same as Fatal("msg").
func FatalDepth(depth int, args ...any) {
	fatalf(depth+1, defaultFormat(args), args...)
}

// FatalDepthf acts as Fatalf but uses depth to determine which call frame to log.
// FatalDepthf(0, "msg") is the same as Fatalf("msg").
func FatalDepthf(depth int, format string, args ...any) {
	fatalf(depth+1, format, args...)
}

// Fatalln logs to the FATAL, ERROR, WARNING, and INFO logs,
// including a stack trace of all running goroutines, then calls os.Exit(2).
// Arguments are handled in the manner of fmt.Println; a newline is appended if missing.
func Fatalln(args ...any) {
	fatalf(1, lnFormat(args), args...)
}

// Fatalf logs to the FATAL, ERROR, WARNING, and INFO logs,
// including a stack trace of all running goroutines, then calls os.Exit(2).
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Fatalf(format string, args ...any) {
	fatalf(1, format, args...)
}

// FatalContext is like [Fatal], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func FatalContext(ctx context.Context, args ...any) {
	FatalContextDepth(ctx, 1, args...)
}

// FatalContextf is like [Fatalf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func FatalContextf(ctx context.Context, format string, args ...any) {
	ctxfatalf(ctx, 1, format, args...)
}

// FatalContextDepth is like [FatalDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func FatalContextDepth(ctx context.Context, depth int, args ...any) {
	ctxfatalf(ctx, depth+1, defaultFormat(args), args...)
}

// FatalContextDepthf is like [FatalDepthf], but with an extra [context.Context] parameter.
func FatalContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxfatalf(ctx, depth+1, format, args...)
}

func ctxexitf(ctx context.Context, depth int, format string, args ...any) {
	ctxlogf(ctx, depth+1, Severity_Fatal, false, noStack, format, args...)
	Flush()
	os.Exit(1)
}

func exitf(depth int, format string, args ...any) {
	ctxexitf(nil, depth+1, format, args...)
}

// Exit logs to the FATAL, ERROR, WARNING, and INFO logs, then calls os.Exit(1).
// Arguments are handled in the manner of fmt.Print; a newline is appended if missing.
func Exit(args ...any) {
	ExitDepth(1, args...)
}

// ExitDepth acts as Exit but uses depth to determine which call frame to log.
// ExitDepth(0, "msg") is the same as Exit("msg").
func ExitDepth(depth int, args ...any) {
	exitf(depth+1, defaultFormat(args), args...)
}

// ExitDepthf acts as Exitf but uses depth to determine which call frame to log.
// ExitDepthf(0, "msg") is the same as Exitf("msg").
func ExitDepthf(depth int, format string, args ...any) {
	exitf(depth+1, format, args...)
}

// Exitln logs to the FATAL, ERROR, WARNING, and INFO logs, then calls os.Exit(1).
func Exitln(args ...any) {
	exitf(1, lnFormat(args), args...)
}

// Exitf logs to the FATAL, ERROR, WARNING, and INFO logs, then calls os.Exit(1).
// Arguments are handled in the manner of fmt.Printf; a newline is appended if missing.
func Exitf(format string, args ...any) {
	exitf(1, format, args...)
}

// ExitContext is like [Exit], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ExitContext(ctx context.Context, args ...any) {
	ExitContextDepth(ctx, 1, args...)
}

// ExitContextf is like [Exitf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ExitContextf(ctx context.Context, format string, args ...any) {
	ctxexitf(ctx, 1, format, args...)
}

// ExitContextDepth is like [ExitDepth], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ExitContextDepth(ctx context.Context, depth int, args ...any) {
	ctxexitf(ctx, depth+1, defaultFormat(args), args...)
}

// ExitContextDepthf is like [ExitDepthf], but with an extra [context.Context] parameter. The
// context is used to pass the Trace Context to log sinks.
func ExitContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	ctxexitf(ctx, depth+1, format, args...)
}
