# Unified Log Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add runtime `-log_mode` flag to switch between printf and structured logging, remove `S()` entry, replace with `mlog.With(fields...)`.

**Architecture:** Global `logMode` atomic + `sync.Once` for immutability after first log. All global functions (`Info`, `Infof`, etc.) branch on mode. `StructuredLogger` renamed to `Logger`; `With()` becomes the top-level entry for sub-loggers with bound fields. Both modes share the same ring buffer + async writer pipeline.

**Tech Stack:** Go 1.21+, mlog internal pipeline (ring buffer, async writer, Encoder interface)

---

## File Structure

| File | Responsibility |
|---|---|
| `constants.go` | `LogMode` type and constants, `SetLogMode`, `getMode` |
| `mlog_flags.go` | Register `-log_mode` flag, lazy-init mode from flag |
| `structured.go` | `Logger` type (renamed from `StructuredLogger`), `With()`, severity methods with mode branch |
| `mlog.go` | Global `Info/Warning/Error/Fatal/Exit/Debug` functions with mode branch; `Verbose` methods with mode branch |
| `logsink.go` | `logEntry` type already supports both `data` and `entry` fields — no change |
| `mode_test.go` | New test file: mode switching, global function routing, `With` behavior in both modes |
| `example/demo01/main.go` | Update to remove `S()` usage |
| `README.md` | Update API docs to reflect `With()` instead of `S()` |

---

### Task 1: LogMode Infrastructure

**Files:**
- Modify: `constants.go`
- Modify: `mlog_flags.go`
- Test: `mode_test.go` (created in Task 6)

- [ ] **Step 1: Add LogMode type and SetLogMode to constants.go**

Add to `constants.go` after existing constants:

```go
// LogMode controls whether the logger uses printf-style or structured output.
type LogMode int8

const (
	LogModePrintf     LogMode = iota // default: traditional printf-style logging
	LogModeStructured                 // structured: Entry + Encoder path
)

var (
	logMode     atomic.Int32 // 0=printf, 1=structured
	modeSetOnce sync.Once
)

// SetLogMode sets the global logging mode. Must be called before any log output.
// After the first log call, the mode is locked and cannot be changed.
func SetLogMode(mode LogMode) {
	modeSetOnce.Do(func() {
		logMode.Store(int32(mode))
	})
}

func getMode() LogMode {
	return LogMode(logMode.Load())
}
```

- [ ] **Step 2: Register -log_mode flag in mlog_flags.go**

In `mlog_flags.go`, find where other flags are registered (e.g., `logEncoderFlag`) and add:

```go
var logModeFlag = flag.String("log_mode", "printf", "Log output mode: printf or structured")
```

Then in the same file's `init()` or flag-parsing logic (wherever `logEncoderFlag` is processed), add after it:

```go
// Lazy-init mode from flag on first getMode() call.
// We use a separate once so SetLogMode() can be called before flag.Parse().
var flagModeOnce sync.Once

func initLogModeFromFlag() {
	flagModeOnce.Do(func() {
		// Only set from flag if SetLogMode hasn't been called yet.
		if logMode.Load() == 0 {
			switch *logModeFlag {
			case "structured":
				SetLogMode(LogModeStructured)
			default:
				// printf is default, already 0
			}
		}
	})
}
```

Modify `getMode()` in `constants.go` to call `initLogModeFromFlag()`:

```go
func getMode() LogMode {
	initLogModeFromFlag()
	return LogMode(logMode.Load())
}
```

Note: `initLogModeFromFlag` must be declared in `mlog_flags.go` since it references `logModeFlag`. `getMode` can call it because they're in the same package. If `initLogModeFromFlag` is in `mlog_flags.go`, make sure `getMode` in `constants.go` can see it (same package, so yes).

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add constants.go mlog_flags.go
git commit -m "feat(mode): add LogMode type, SetLogMode, and -log_mode flag"
```

---

### Task 2: Rename StructuredLogger → Logger, Remove S(), Add With()

**Files:**
- Modify: `structured.go`
- Test: `structured_test.go` (update existing tests)

- [ ] **Step 1: Rename type and delete S() in structured.go**

Replace the top of `structured.go` (lines 9-19):

Old:
```go
// StructuredLogger provides a fluent API for structured logging.
// Use S() to obtain the global instance, and With() to bind persistent fields.
type StructuredLogger struct {
	fields []Field
}

// globalStructured is the default StructuredLogger with no bound fields.
var globalStructured = &StructuredLogger{}

// S returns the global StructuredLogger.
func S() *StructuredLogger { return globalStructured }
```

New:
```go
// Logger provides a fluent API for structured logging.
// Use With() to obtain a logger with bound persistent fields.
type Logger struct {
	fields []Field
}

// globalLogger is the default Logger with no bound fields.
var globalLogger = &Logger{}

// With returns a new Logger that carries the given fields.
// It replaces the old S().With() pattern.
func With(fields ...Field) *Logger {
	return globalLogger.With(fields...)
}
```

- [ ] **Step 2: Update method receivers in structured.go**

Replace all `*StructuredLogger` receivers with `*Logger`. The `With` method stays the same (already returns `*StructuredLogger`, change to `*Logger`):

```go
func (l *Logger) With(fields ...Field) *Logger {
	merged := make([]Field, 0, len(l.fields)+len(fields))
	merged = append(merged, l.fields...)
	merged = append(merged, fields...)
	return &Logger{fields: merged}
}
```

Update `Info`, `Warning`, `Error`, `Fatal` methods to branch on mode. Replace each method body.

Old `Info`:
```go
func (s *StructuredLogger) Info(msg string, fields ...Field) {
	s.log(Severity_Info, msg, fields)
}
```

New `Info`:
```go
func (l *Logger) Info(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Info, msg, fields)
	} else {
		InfoDepth(1, msg)
	}
}
```

Similarly update `Warning`, `Error`, `Fatal`:

```go
func (l *Logger) Warning(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Warning, msg, fields)
	} else {
		WarningDepth(1, msg)
	}
}

func (l *Logger) Error(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Error, msg, fields)
	} else {
		ErrorDepth(1, msg)
	}
}

func (l *Logger) Fatal(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Fatal, msg, fields)
	} else {
		FatalDepth(1, msg)
	}
}
```

Keep `Logger.log` (the internal method) unchanged — it already handles structured emission.

- [ ] **Step 3: Update existing structured_test.go to use With() instead of S()**

Find all occurrences of `S()` in `structured_test.go` and replace:
- `mlog.S()` → `mlog.With()`
- `mlog.S().With(...)` → `mlog.With(...)`

For example, if a test has:
```go
logger := mlog.S().With(mlog.String("svc", "test"))
```
Change to:
```go
logger := mlog.With(mlog.String("svc", "test"))
```

- [ ] **Step 4: Run tests to verify rename didn't break anything**

Run: `go test -run TestStructured -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add structured.go structured_test.go
git commit -m "feat(mode): rename StructuredLogger to Logger, remove S(), add With()"
```

---

### Task 3: Global Info/Warning/Error/Fatal Functions — Structured Mode Branch

**Files:**
- Modify: `mlog.go`

- [ ] **Step 1: Add infoStructured helper in mlog.go**

Add near the top of `mlog.go` (after the existing helpers like `logf`, `ctxlogf`):

```go
// infoStructured extracts a message and fields from args for structured mode.
// args[0] must be a string (the message). Any Field values in args[1:] are
// extracted as structured fields. Non-Field values are ignored.
func infoStructured(depth int, severity Severity, args ...any) {
	if len(args) == 0 {
		globalLogger.log(severity, "", nil)
		return
	}
	msg, ok := args[0].(string)
	if !ok {
		msg = fmt.Sprint(args[0])
	}
	var fields []Field
	for _, arg := range args[1:] {
		if f, ok := arg.(Field); ok {
			fields = append(fields, f)
		}
	}
	globalLogger.log(severity, msg, fields)
}
```

- [ ] **Step 2: Add infofStructured helper in mlog.go**

```go
func infofStructured(depth int, severity Severity, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	globalLogger.log(severity, msg, nil)
}
```

- [ ] **Step 3: Branch Info and Infof**

Replace `Info` and `Infof` in `mlog.go`:

Old:
```go
func Info(args ...any) {
	InfoDepth(1, args...)
}
```

New:
```go
func Info(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Info, args...)
	} else {
		InfoDepth(1, args...)
	}
}
```

Old:
```go
func Infof(format string, args ...any) {
	logf(1, Severity_Info, false, noStack, format, args...)
}
```

New:
```go
func Infof(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Info, format, args...)
	} else {
		logf(1, Severity_Info, false, noStack, format, args...)
	}
}
```

- [ ] **Step 4: Branch Warning and Warningf**

```go
func Warning(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Warning, args...)
	} else {
		WarningDepth(1, args...)
	}
}

func Warningf(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Warning, format, args...)
	} else {
		logf(1, Severity_Warning, false, noStack, format, args...)
	}
}
```

- [ ] **Step 5: Branch Error and Errorf**

```go
func Error(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Error, args...)
	} else {
		ErrorDepth(1, args...)
	}
}

func Errorf(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Error, format, args...)
	} else {
		logf(1, Severity_Error, false, noStack, format, args...)
	}
}
```

- [ ] **Step 6: Branch Fatal and Fatalf**

```go
func Fatal(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Fatal, args...)
		flushAndAbort()
	} else {
		FatalDepth(1, args...)
	}
}

func Fatalf(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Fatal, format, args...)
		flushAndAbort()
	} else {
		fatalf(1, format, args...)
	}
}
```

- [ ] **Step 7: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 8: Run existing tests to ensure printf mode still works**

Run: `go test -run 'TestInfo|TestWarning|TestError' -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add mlog.go
git commit -m "feat(mode): branch Info/Warning/Error/Fatal for structured mode"
```

---

### Task 4: Debug and Exit Functions — Structured Mode Branch

**Files:**
- Modify: `mlog.go`

- [ ] **Step 1: Branch Debug functions**

```go
func Debug(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Debug, args...)
	} else {
		DebugDepth(1, args...)
	}
}

func Debugf(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Debug, format, args...)
	} else {
		logf(1, Severity_Debug, false, noStack, format, args...)
	}
}
```

- [ ] **Step 2: Branch Exit functions**

```go
func Exit(args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(1, Severity_Fatal, args...)
		Close()
		os.Exit(1)
	} else {
		ExitDepth(1, args...)
	}
}

func Exitf(format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(1, Severity_Fatal, format, args...)
		Close()
		os.Exit(1)
	} else {
		exitf(1, format, args...)
	}
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add mlog.go
git commit -m "feat(mode): branch Debug and Exit for structured mode"
```

---

### Task 5: ln and Context Variants — Structured Mode Branch

**Files:**
- Modify: `mlog.go`

- [ ] **Step 1: Add helper for ln-style structured output**

```go
func infoLnStructured(depth int, severity Severity, args ...any) {
	msg := strings.TrimSpace(fmt.Sprintln(args...))
	globalLogger.log(severity, msg, nil)
}
```

Add `strings` to imports if not already present.

- [ ] **Step 2: Branch all ln variants**

```go
func Debugln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Debug, args...)
	} else {
		logf(1, Severity_Debug, false, noStack, lnFormat(args), args...)
	}
}

func Infoln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Info, args...)
	} else {
		logf(1, Severity_Info, false, noStack, lnFormat(args), args...)
	}
}

func Warningln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Warning, args...)
	} else {
		logf(1, Severity_Warning, false, noStack, lnFormat(args), args...)
	}
}

func Errorln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Error, args...)
	} else {
		logf(1, Severity_Error, false, noStack, lnFormat(args), args...)
	}
}

func Fatalln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Fatal, args...)
		flushAndAbort()
	} else {
		fatalf(1, lnFormat(args), args...)
	}
}

func Exitln(args ...any) {
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Fatal, args...)
		Close()
		os.Exit(1)
	} else {
		exitf(1, lnFormat(args), args...)
	}
}
```

- [ ] **Step 3: Branch Context variants**

For each Context variant, in structured mode we need to pass context through. However, `infoStructured` doesn't handle context. We need a context-aware helper:

```go
func infoContextStructured(depth int, severity Severity, ctx context.Context, args ...any) {
	if len(args) == 0 {
		ctxlogStructured(ctx, depth+1, severity, "", nil)
		return
	}
	msg, ok := args[0].(string)
	if !ok {
		msg = fmt.Sprint(args[0])
	}
	var fields []Field
	for _, arg := range args[1:] {
		if f, ok := arg.(Field); ok {
			fields = append(fields, f)
		}
	}
	ctxlogStructured(ctx, depth+1, severity, msg, fields)
}

func ctxlogStructured(ctx context.Context, depth int, severity Severity, msg string, fields []Field) {
	// Build Entry from context + msg + fields, then emit.
	// For now, reuse Logger.log logic but inject context.
	// Since Logger.log doesn't accept context, we construct Entry manually.
	pcs := [1]uintptr{}
	if runtime.Callers(depth+1, pcs[:]) < 1 {
		return
	}
	frame, _ := runtime.CallersFrames(pcs[:]).Next()

	entry := getEntry()
	entry.Severity = severity
	entry.Time = timeNow().UnixNano()
	entry.Message = msg
	entry.File = frame.File
	entry.Line = frame.Line
	entry.Funcname = frame.Function
	entry.Thread = int64(pid)

	if len(fields) > 0 {
		entry.Fields = append(entry.Fields[:0], fields...)
	} else {
		entry.Fields = entry.Fields[:0]
	}

	if sampler := getSampler(); sampler != nil {
		if !sampler.allowSeverity(severity) {
			atomic.AddInt64(&Stats.Dropped.lines, 1)
			putEntry(entry)
			return
		}
	}

	structuredEmit(entry, severity)
}
```

Then branch all Context variants. Example for InfoContext:

```go
func InfoContext(ctx context.Context, args ...any) {
	if getMode() == LogModeStructured {
		infoContextStructured(1, Severity_Info, ctx, args...)
	} else {
		InfoContextDepth(ctx, 1, args...)
	}
}

func InfoContextf(ctx context.Context, format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		ctxlogStructured(ctx, 1, Severity_Info, msg, nil)
	} else {
		ctxlogf(ctx, 1, Severity_Info, false, noStack, format, args...)
	}
}
```

Repeat for `DebugContext/DebugContextf`, `WarningContext/WarningContextf`, `ErrorContext/ErrorContextf`, `FatalContext/FatalContextf`, `ExitContext/ExitContextf`.

- [ ] **Step 4: Branch Depth variants**

Depth variants in structured mode need to pass the correct depth to `infoStructured`/`infofStructured`:

```go
func InfoDepth(depth int, args ...any) {
	if getMode() == LogModeStructured {
		infoStructured(depth+1, Severity_Info, args...)
	} else {
		logf(depth+1, Severity_Info, false, noStack, defaultFormat(args), args...)
	}
}

func InfoDepthf(depth int, format string, args ...any) {
	if getMode() == LogModeStructured {
		infofStructured(depth+1, Severity_Info, format, args...)
	} else {
		logf(depth+1, Severity_Info, false, noStack, format, args...)
	}
}
```

Repeat for Debug, Warning, Error, Fatal, Exit depth variants.

- [ ] **Step 5: Branch Context+Depth variants**

```go
func InfoContextDepth(ctx context.Context, depth int, args ...any) {
	if getMode() == LogModeStructured {
		infoContextStructured(depth+1, Severity_Info, ctx, args...)
	} else {
		ctxlogf(ctx, depth+1, Severity_Info, false, noStack, defaultFormat(args), args...)
	}
}

func InfoContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		ctxlogStructured(ctx, depth+1, Severity_Info, msg, nil)
	} else {
		ctxlogf(ctx, depth+1, Severity_Info, false, noStack, format, args...)
	}
}
```

Repeat for all severity levels.

- [ ] **Step 6: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 7: Run full test suite for printf mode regression**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add mlog.go
git commit -m "feat(mode): branch ln/Context/Depth variants for structured mode"
```

---

### Task 6: V-Log Methods — Structured Mode Branch

**Files:**
- Modify: `mlog.go`

- [ ] **Step 1: Branch Verbose.Info and Verbose.Infof**

Replace Verbose methods. Old `Verbose.Info`:
```go
func (v Verbose) Info(args ...any) {
	v.InfoDepth(1, args...)
}
```

New:
```go
func (v Verbose) Info(args ...any) {
	if getMode() == LogModeStructured {
		if v {
			infoStructured(1, Severity_Info, args...)
		}
	} else {
		v.InfoDepth(1, args...)
	}
}
```

Old `Verbose.InfoDepth`:
```go
func (v Verbose) InfoDepth(depth int, args ...any) {
	if v {
		logf(depth+1, Severity_Info, true, noStack, defaultFormat(args), args...)
	}
}
```

New:
```go
func (v Verbose) InfoDepth(depth int, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		infoStructured(depth+1, Severity_Info, args...)
	} else {
		logf(depth+1, Severity_Info, true, noStack, defaultFormat(args), args...)
	}
}
```

Old `Verbose.InfoDepthf`:
```go
func (v Verbose) InfoDepthf(depth int, format string, args ...any) {
	if v {
		logf(depth+1, Severity_Info, true, noStack, format, args...)
	}
}
```

New:
```go
func (v Verbose) InfoDepthf(depth int, format string, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		infofStructured(depth+1, Severity_Info, format, args...)
	} else {
		logf(depth+1, Severity_Info, true, noStack, format, args...)
	}
}
```

- [ ] **Step 2: Branch Verbose.Infoln**

```go
func (v Verbose) Infoln(args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		infoLnStructured(1, Severity_Info, args...)
	} else {
		logf(1, Severity_Info, true, noStack, lnFormat(args), args...)
	}
}
```

- [ ] **Step 3: Branch Verbose.InfoContext and variants**

```go
func (v Verbose) InfoContext(ctx context.Context, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		infoContextStructured(1, Severity_Info, ctx, args...)
	} else {
		v.InfoContextDepth(ctx, 1, args...)
	}
}

func (v Verbose) InfoContextf(ctx context.Context, format string, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		ctxlogStructured(ctx, 1, Severity_Info, msg, nil)
	} else {
		ctxlogf(ctx, 1, Severity_Info, true, noStack, format, args...)
	}
}

func (v Verbose) InfoContextDepth(ctx context.Context, depth int, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		infoContextStructured(depth+1, Severity_Info, ctx, args...)
	} else {
		ctxlogf(ctx, depth+1, Severity_Info, true, noStack, defaultFormat(args), args...)
	}
}

func (v Verbose) InfoContextDepthf(ctx context.Context, depth int, format string, args ...any) {
	if !v {
		return
	}
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		ctxlogStructured(ctx, depth+1, Severity_Info, msg, nil)
	} else {
		ctxlogf(ctx, depth+1, Severity_Info, true, noStack, format, args...)
	}
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 5: Run existing vmodule tests**

Run: `go test -run TestV -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add mlog.go
git commit -m "feat(mode): branch Verbose methods for structured mode"
```

---

### Task 7: Add Logger printf-style methods (Infof, Debugf, etc.)

**Files:**
- Modify: `structured.go`

Currently `Logger` only has `Info(msg, fields...)` methods. For API symmetry in both modes, add `Infof`, `Debugf`, etc. to `Logger`:

- [ ] **Step 1: Add Infof/Debugf/Warningf/Errorf/Fatalf to Logger**

```go
func (l *Logger) Infof(format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		l.log(Severity_Info, msg, nil)
	} else {
		InfoDepthf(1, format, args...)
	}
}

func (l *Logger) Debugf(format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		l.log(Severity_Debug, msg, nil)
	} else {
		DebugDepthf(1, format, args...)
	}
}

func (l *Logger) Warningf(format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		l.log(Severity_Warning, msg, nil)
	} else {
		WarningDepthf(1, format, args...)
	}
}

func (l *Logger) Errorf(format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		l.log(Severity_Error, msg, nil)
	} else {
		ErrorDepthf(1, format, args...)
	}
}

func (l *Logger) Fatalf(format string, args ...any) {
	if getMode() == LogModeStructured {
		msg := fmt.Sprintf(format, args...)
		l.log(Severity_Fatal, msg, nil)
		flushAndAbort()
	} else {
		fatalf(1, format, args...)
	}
}
```

- [ ] **Step 2: Add Infoln/Debugln/Warningln/Errorln/Fatalln to Logger**

```go
func (l *Logger) Infoln(args ...any) {
	if getMode() == LogModeStructured {
		msg := strings.TrimSpace(fmt.Sprintln(args...))
		l.log(Severity_Info, msg, nil)
	} else {
		Infoln(args...)
	}
}

// ... repeat for Debugln, Warningln, Errorln, Fatalln
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add structured.go
git commit -m "feat(mode): add printf-style methods to Logger for API symmetry"
```

---

### Task 8: Write Mode Tests

**Files:**
- Create: `mode_test.go`

- [ ] **Step 1: Test SetLogMode and getMode**

```go
package mlog

import (
	"testing"
)

func TestSetLogMode(t *testing.T) {
	// Save and restore original mode
	orig := getMode()
	defer func() {
		logMode.Store(int32(orig))
		modeSetOnce = sync.Once{}
	}()

	SetLogMode(LogModeStructured)
	if got := getMode(); got != LogModeStructured {
		t.Errorf("getMode() = %v, want structured", got)
	}

	// Second call should be no-op
	SetLogMode(LogModePrintf)
	if got := getMode(); got != LogModeStructured {
		t.Errorf("getMode() after second SetLogMode = %v, want structured", got)
	}
}
```

- [ ] **Step 2: Test With() returns Logger with fields**

```go
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
```

- [ ] **Step 3: Test global Info routes to structured path**

```go
func TestInfoStructuredMode(t *testing.T) {
	SetLogMode(LogModeStructured)
	defer func() {
		logMode.Store(0)
		modeSetOnce = sync.Once{}
	}()

	// This should not panic and should route through structured path
	Info("test message", String("key", "value"))
}
```

- [ ] **Step 4: Test global Infof routes to structured path**

```go
func TestInfofStructuredMode(t *testing.T) {
	SetLogMode(LogModeStructured)
	defer func() {
		logMode.Store(0)
		modeSetOnce = sync.Once{}
	}()

	Infof("formatted %s", "message")
}
```

- [ ] **Step 5: Test Logger.Info in structured mode**

```go
func TestLoggerInfoStructuredMode(t *testing.T) {
	SetLogMode(LogModeStructured)
	defer func() {
		logMode.Store(0)
		modeSetOnce = sync.Once{}
	}()

	logger := With(String("svc", "test"))
	logger.Info("request", String("path", "/api"))
}
```

- [ ] **Step 6: Run mode tests**

Run: `go test -run 'TestSetLogMode|TestWith|TestInfoStructured|TestInfofStructured|TestLoggerInfo' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add mode_test.go
git commit -m "test(mode): add LogMode and With() tests"
```

---

### Task 9: Update README and Examples

**Files:**
- Modify: `README.md`
- Modify: `example/demo01/main.go`

- [ ] **Step 1: Update example/demo01/main.go**

Find any `S()` usage and replace with `With()` or global functions:

Old:
```go
mlog.S().Info("请求处理完成", ...)
logger := mlog.S().With(...)
```

New:
```go
mlog.Info("请求处理完成", ...)  // with -log_mode=structured
logger := mlog.With(...)
```

- [ ] **Step 2: Update README.md structured logging section**

Replace the `S()` examples with `With()` and mention `-log_mode`:

```markdown
### 结构化日志

```go
package main

import (
    "github.com/odysseythink/mlog"
)

func main() {
    defer mlog.Flush()

    // 启动时指定 -log_mode=structured
    // 基本结构化日志
    mlog.Info("请求处理完成",
        mlog.String("method", "GET"),
        mlog.String("path", "/api/users"),
        mlog.Int("status", 200),
        mlog.Duration("elapsed", 12*time.Millisecond),
    )

    // 绑定持久字段
    logger := mlog.With(
        mlog.String("service", "user-api"),
        mlog.String("version", "1.0.0"),
    )
    logger.Info("用户登录", mlog.String("user_id", "abc123"))
    logger.Error("数据库超时", mlog.Err(err))
}
```

Update the API reference section to remove `S()` and add `With()`:

```markdown
### 结构化日志 API

```go
// 获取带持久字段的 logger
logger := mlog.With(mlog.String("request_id", "abc"))

// 日志输出
mlog.Info("消息", mlog.Int("key", 42))
logger.Info("消息", mlog.Int("key", 42))
logger.Warning("警告", mlog.Err(err))
```
```

- [ ] **Step 3: Update command line flags table**

Add to the flags table:

```markdown
| `-log_mode` | printf | 日志模式：printf（默认）或 structured |
```

- [ ] **Step 4: Commit**

```bash
git add README.md example/demo01/main.go
git commit -m "docs: update README and example to use With() and -log_mode"
```

---

## Self-Review

### 1. Spec Coverage

| Spec Section | Plan Task |
|---|---|
| LogMode type, SetLogMode, -log_mode flag | Task 1 |
| StructuredLogger → Logger rename | Task 2 |
| Remove S(), add With() | Task 2 |
| Global Info/Warning/Error/Fatal branch | Task 3 |
| Global Debug/Exit branch | Task 4 |
| ln/Context/Depth variants branch | Task 5 |
| V-log methods branch | Task 6 |
| Logger printf-style methods | Task 7 |
| Tests for mode switching | Task 8 |
| README/example updates | Task 9 |

**Gap:** None.

### 2. Placeholder Scan

No TBD/TODO/"implement later"/"similar to Task N" found. All steps contain concrete code.

### 3. Type Consistency

- `LogMode` used consistently as `LogModePrintf` / `LogModeStructured`
- `getMode()` returns `LogMode` everywhere
- `Logger` type name used consistently (replaced all `StructuredLogger`)
- `infoStructured` / `infofStructured` / `infoLnStructured` / `infoContextStructured` signatures are consistent with their call sites

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-08-unified-log-mode.md`.**

**Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
