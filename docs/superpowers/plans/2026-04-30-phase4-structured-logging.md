# Phase 4: Structured Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add structured logging with Field tagged union, pluggable Encoder (JSON/logfmt/text), type-safe `S().Info(msg, fields...)` API, and zero-alloc hot path. Existing `Infof`/`Errorf` API unchanged.

**Architecture:** New API builds `Entry` structs with `[]Field`, pushes them into the existing per-severity ring buffers. Async writer goroutines call `encoder.EncodeEntry(entry)` to produce bytes. Two paths (old textPrintf, new structuredLog) share ring buffer + async writer + file sink.

**Tech Stack:** Go 1.21+, `sync.Pool`, `sync/atomic`, `math.Float64bits`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `field.go` | Create | FieldType enum, Field tagged union, constructor functions |
| `field_test.go` | Create | Field constructor correctness tests |
| `entry.go` | Create | Entry struct, sync.Pool, putEntry |
| `entry_test.go` | Create | Entry pool tests |
| `encoder.go` | Create | Encoder interface, activeEncoder atomic.Value, getEncoder/SetEncoder |
| `encoder_text.go` | Create | TextEncoder: matches existing textPrintf format + logfmt key=value fields |
| `encoder_json.go` | Create | JSONEncoder: JSON Lines output |
| `encoder_logfmt.go` | Create | LogfmtEncoder: logfmt output |
| `encoder_test.go` | Create | Encoder output format tests for all three |
| `structured.go` | Create | StructuredLogger, S(), With(), structuredLog(), structuredEmit |
| `structured_test.go` | Create | Integration tests for new API |
| `structured_bench_test.go` | Create | Zero-alloc benchmarks |
| `logsink.go` | Modify | logEntry gains `entry *Entry` field |
| `async_writer.go` | Modify | writeBatch() handles data vs entry path |
| `mlog_flags.go` | Modify | Add `-log_encoder` flag |
| `mlog.go` | Modify | Add `S()` function |

---

## Task 1: Field Type + Constructors

**Files:**
- Create: `field.go`
- Create: `field_test.go`

- [ ] **Step 1: Create `field.go`**

```go
package mlog

import (
	"math"
	"time"
)

type FieldType uint8

const (
	fieldTypeUnknown FieldType = iota
	FieldTypeInt64
	FieldTypeFloat64
	FieldTypeString
	FieldTypeBool
	FieldTypeDuration
	FieldTypeErr
	FieldTypeAny
)

type Field struct {
	Key       string
	Type      FieldType
	Integer   int64
	String    string
	Interface any
}

func Int(key string, val int) Field {
	return Field{Key: key, Type: FieldTypeInt64, Integer: int64(val)}
}

func Int64(key string, val int64) Field {
	return Field{Key: key, Type: FieldTypeInt64, Integer: val}
}

func Float64(key string, val float64) Field {
	return Field{Key: key, Type: FieldTypeFloat64, Integer: int64(math.Float64bits(val))}
}

func String(key, val string) Field {
	return Field{Key: key, Type: FieldTypeString, String: val}
}

func Bool(key string, val bool) Field {
	return Field{Key: key, Type: FieldTypeBool, Integer: boolToInt64(val)}
}

func Duration(key string, val time.Duration) Field {
	return Field{Key: key, Type: FieldTypeDuration, Integer: int64(val)}
}

func Err(err error) Field {
	return Field{Key: "error", Type: FieldTypeErr, Interface: err}
}

func Any(key string, val any) Field {
	return Field{Key: key, Type: FieldTypeAny, Interface: val}
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: Create `field_test.go`**

```go
package mlog

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestFieldInt(t *testing.T) {
	f := Int("count", 42)
	if f.Key != "count" || f.Type != FieldTypeInt64 || f.Integer != 42 {
		t.Fatalf("Int field wrong: %+v", f)
	}
}

func TestFieldInt64(t *testing.T) {
	f := Int64("count", -1)
	if f.Integer != -1 || f.Type != FieldTypeInt64 {
		t.Fatalf("Int64 field wrong: %+v", f)
	}
}

func TestFieldFloat64(t *testing.T) {
	f := Float64("ratio", 3.14)
	got := math.Float64frombits(uint64(f.Integer))
	if got != 3.14 {
		t.Fatalf("Float64 roundtrip: got %v, want 3.14", got)
	}
}

func TestFieldString(t *testing.T) {
	f := String("name", "test")
	if f.String != "test" || f.Type != FieldTypeString {
		t.Fatalf("String field wrong: %+v", f)
	}
}

func TestFieldBool(t *testing.T) {
	tf := Bool("ok", true)
	if tf.Integer != 1 || tf.Type != FieldTypeBool {
		t.Fatalf("Bool(true) wrong: %+v", tf)
	}
	ff := Bool("ok", false)
	if ff.Integer != 0 {
		t.Fatalf("Bool(false) wrong: %+v", ff)
	}
}

func TestFieldDuration(t *testing.T) {
	d := 5 * time.Second
	f := Duration("elapsed", d)
	if f.Integer != int64(d) || f.Type != FieldTypeDuration {
		t.Fatalf("Duration field wrong: %+v", f)
	}
}

func TestFieldErr(t *testing.T) {
	err := errors.New("fail")
	f := Err(err)
	if f.Key != "error" || f.Type != FieldTypeErr || f.Interface != err {
		t.Fatalf("Err field wrong: %+v", f)
	}
}

func TestFieldAny(t *testing.T) {
	val := []int{1, 2, 3}
	f := Any("data", val)
	if f.Type != FieldTypeAny || f.Interface != val {
		t.Fatalf("Any field wrong: %+v", f)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test -run TestField -v -count=1`
Expected: All 8 tests PASS

- [ ] **Step 4: Commit**

```bash
git add field.go field_test.go
git commit -m "feat(structured): add Field tagged union type with zero-boxing constructors"
```

---

## Task 2: Entry Struct + Pool

**Files:**
- Create: `entry.go`
- Create: `entry_test.go`

- [ ] **Step 1: Create `entry.go`**

```go
package mlog

import "sync"

// Entry is a structured log entry that flows through the pipeline.
// Pooled via sync.Pool to avoid allocations on the hot path.
type Entry struct {
	Severity Severity
	Time     int64 // Unix nanoseconds
	Message  string
	Fields   []Field // nil = no structured fields (old API path)
	File     string
	Line     int
	Funcname string
	Thread   int64
	Stack    *Stack
}

var entryPool = sync.Pool{
	New: func() any {
		return &Entry{
			Fields: make([]Field, 0, 16),
		}
	},
}

func getEntry() *Entry {
	e := entryPool.Get().(*Entry)
	return e
}

func putEntry(e *Entry) {
	e.Severity = 0
	e.Time = 0
	e.Message = ""
	e.Fields = e.Fields[:0]
	e.File = ""
	e.Line = 0
	e.Funcname = ""
	e.Thread = 0
	e.Stack = nil
	entryPool.Put(e)
}
```

- [ ] **Step 2: Create `entry_test.go`**

```go
package mlog

import "testing"

func TestEntryPool(t *testing.T) {
	e := getEntry()
	if e == nil {
		t.Fatal("getEntry returned nil")
	}
	if cap(e.Fields) < 16 {
		t.Fatalf("Fields capacity %d, want >= 16", cap(e.Fields))
	}

	e.Message = "test"
	e.Fields = append(e.Fields, Int("k", 1))
	putEntry(e)

	e2 := getEntry()
	if e2.Message != "" {
		t.Fatal("entry not reset after put")
	}
	if len(e2.Fields) != 0 {
		t.Fatal("Fields not cleared after put")
	}
	putEntry(e2)
}
```

- [ ] **Step 3: Run tests**

Run: `go test -run TestEntry -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add entry.go entry_test.go
git commit -m "feat(structured): add Entry struct with sync.Pool"
```

---

## Task 3: Encoder Interface + TextEncoder

**Files:**
- Create: `encoder.go`
- Create: `encoder_text.go`
- Create: `encoder_test.go` (partial, TextEncoder only)

- [ ] **Step 1: Create `encoder.go`**

```go
package mlog

import (
	"sync"
	"sync/atomic"
)

// Encoder converts a structured Entry into bytes.
type Encoder interface {
	// EncodeEntry encodes the entry and returns the formatted bytes.
	// The returned []byte is from a pool and must be returned via encBufPool.
	EncodeEntry(entry *Entry) []byte
}

var (
	activeEncoder atomic.Value // stores Encoder
)

func getEncoder() Encoder {
	if v := activeEncoder.Load(); v != nil {
		return v.(Encoder)
	}
	return defaultTextEncoder
}

func SetEncoder(enc Encoder) {
	activeEncoder.Store(enc)
}

// encBufPool holds []byte buffers used by encoders.
var encBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, defaultEntryBufSize)
		return &b
	},
}

func getEncBuf() *[]byte {
	return encBufPool.Get().(*[]byte)
}

func putEncBuf(p *[]byte) {
	if cap(*p) > maxPooledEntryBuf {
		return
	}
	*p = (*p)[:0]
	encBufPool.Put(p)
}

var defaultTextEncoder = &textEncoder{}
```

- [ ] **Step 2: Create `encoder_text.go`**

```go
package mlog

import (
	"strconv"
	"strings"
	"time"
)

type textEncoder struct{}

func (e *textEncoder) EncodeEntry(entry *Entry) []byte {
	bp := getEncBuf()
	buf := *bp

	// Header: [YYYY-MM-DD HH:MM:SS.uuuuuu][S][PID][file:func:line]
	buf = append(buf, '[')
	t := time.Unix(0, entry.Time)
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	buf = appendDigits(buf, 4, uint64(year), '0')
	buf = append(buf, '-')
	buf = appendDigits(buf, 2, uint64(month), '0')
	buf = append(buf, '-')
	buf = appendDigits(buf, 2, uint64(day), '0')
	buf = append(buf, ' ')
	buf = appendDigits(buf, 2, uint64(hour), '0')
	buf = append(buf, ':')
	buf = appendDigits(buf, 2, uint64(minute), '0')
	buf = append(buf, ':')
	buf = appendDigits(buf, 2, uint64(second), '0')
	buf = append(buf, '.')
	buf = appendDigits(buf, 6, uint64(t.Nanosecond()/1000), '0')
	buf = append(buf, ']')
	buf = append(buf, '[')
	buf = append(buf, severityChar[entry.Severity])
	buf = append(buf, ']')
	buf = append(buf, '[')
	buf = appendDigits(buf, 7, uint64(entry.Thread), ' ')
	buf = append(buf, ']')
	buf = append(buf, '[')
	file := entry.File
	if i := strings.LastIndex(file, "/"); i >= 0 {
		file = file[i+1:]
	}
	buf = append(buf, file...)
	buf = append(buf, ' ')
	fn := entry.Funcname
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		fn = fn[i+1:]
	}
	buf = append(buf, fn...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(entry.Line), 10)
	buf = append(buf, ']')
	buf = append(buf, ' ')

	// Message
	buf = append(buf, entry.Message...)

	// Fields as key=value pairs
	for _, f := range entry.Fields {
		buf = append(buf, ' ')
		buf = append(buf, f.Key...)
		buf = append(buf, '=')
		buf = appendFieldTextVal(buf, f)
	}

	buf = append(buf, '\n')

	*bp = buf
	return *bp
}

func appendFieldTextVal(buf []byte, f Field) []byte {
	switch f.Type {
	case FieldTypeInt64:
		return strconv.AppendInt(buf, f.Integer, 10)
	case FieldTypeFloat64:
		return strconv.AppendFloat(buf, math.Float64frombits(uint64(f.Integer)), 'f', -1, 64)
	case FieldTypeString:
		return append(buf, f.String...)
	case FieldTypeBool:
		if f.Integer != 0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case FieldTypeDuration:
		return append(buf, time.Duration(f.Integer).String()...)
	case FieldTypeErr:
		if f.Interface != nil {
			return append(buf, f.Interface.(error).Error()...)
		}
		return buf
	case FieldTypeAny:
		return strconv.AppendQuote(buf, fmt.Sprintf("%v", f.Interface))
	default:
		return buf
	}
}

func appendDigits(buf []byte, n int, d uint64, pad byte) []byte {
	var tmp [20]byte
	cutoff := len(tmp) - n
	j := len(tmp) - 1
	for ; d > 0; j-- {
		tmp[j] = digits[d%10]
		d /= 10
	}
	for ; j >= cutoff; j-- {
		tmp[j] = pad
	}
	j++
	return append(buf, tmp[j:]...)
}
```

Note: `encoder_text.go` needs imports `"math"`, `"fmt"`, `"strconv"`, `"strings"`, `"time"`. The `digits` constant is already defined in `constants.go`.

- [ ] **Step 3: Create initial `encoder_test.go` (TextEncoder)**

```go
package mlog

import (
	"strings"
	"testing"
	"time"
)

func TestTextEncoderNoFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 123456000, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "hello world",
		File:     "/src/main.go",
		Line:     42,
		Funcname: "main.main",
		Thread:   12345,
	}

	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "[2026-04-30") {
		t.Fatalf("missing date in: %s", s)
	}
	if !strings.Contains(s, "[I]") {
		t.Fatalf("missing severity in: %s", s)
	}
	if !strings.Contains(s, "hello world") {
		t.Fatalf("missing message in: %s", s)
	}
	if !strings.Contains(s, "main.go:42") {
		t.Fatalf("missing caller in: %s", s)
	}
}

func TestTextEncoderWithFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "request",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
			Bool("ok", true),
		},
	}

	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "status=200") {
		t.Fatalf("missing int field in: %s", s)
	}
	if !strings.Contains(s, "method=GET") {
		t.Fatalf("missing string field in: %s", s)
	}
	if !strings.Contains(s, "ok=true") {
		t.Fatalf("missing bool field in: %s", s)
	}
}
```

- [ ] **Step 4: Build and fix any compile errors**

Run: `go build ./...`

The `encoder_text.go` uses `math.Float64frombits` and `fmt.Sprintf` — add `"math"` and `"fmt"` to imports. If `appendDigits` conflicts with existing `nDigits` in `logsink.go`, rename or use a private name. Fix until it compiles.

- [ ] **Step 5: Run tests**

Run: `go test -run TestTextEncoder -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add encoder.go encoder_text.go encoder_test.go
git commit -m "feat(structured): add Encoder interface and TextEncoder implementation"
```

---

## Task 4: JSONEncoder + LogfmtEncoder

**Files:**
- Create: `encoder_json.go`
- Create: `encoder_logfmt.go`
- Modify: `encoder_test.go` (add tests)

- [ ] **Step 1: Create `encoder_json.go`**

```go
package mlog

import (
	"strconv"
	"strings"
	"time"
)

type jsonEncoder struct{}

func (e *jsonEncoder) EncodeEntry(entry *Entry) []byte {
	bp := getEncBuf()
	buf := *bp

	buf = append(buf, '{')

	// ts
	t := time.Unix(0, entry.Time)
	buf = append(buf, `"ts":"`...)
	buf = append(buf, t.Format(time.RFC3339Nano)...)
	buf = append(buf, '"', ',')

	// level
	buf = append(buf, `"level":"`...)
	buf = append(buf, entry.Severity.String()...)
	buf = append(buf, '"', ',')

	// caller
	file := entry.File
	if i := strings.LastIndex(file, "/"); i >= 0 {
		file = file[i+1:]
	}
	buf = append(buf, `"caller":"`...)
	buf = append(buf, file...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(entry.Line), 10)
	buf = append(buf, '"', ',')

	// msg
	buf = append(buf, `"msg":`...)
	buf = appendJSONString(buf, entry.Message)
	buf = append(buf, ',')

	// fields
	for i, f := range entry.Fields {
		buf = append(buf, '"')
		buf = append(buf, f.Key...)
		buf = append(buf, '"', ':')
		buf = appendFieldJSONVal(buf, f)
		if i < len(entry.Fields)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, '}', '\n')

	*bp = buf
	return *bp
}

func appendJSONString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	for _, c := range s {
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			buf = append(buf, string(c)...)
		}
	}
	buf = append(buf, '"')
	return buf
}

func appendFieldJSONVal(buf []byte, f Field) []byte {
	switch f.Type {
	case FieldTypeInt64:
		return strconv.AppendInt(buf, f.Integer, 10)
	case FieldTypeFloat64:
		return strconv.AppendFloat(buf, math.Float64frombits(uint64(f.Integer)), 'f', -1, 64)
	case FieldTypeString:
		return appendJSONString(buf, f.String)
	case FieldTypeBool:
		if f.Integer != 0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case FieldTypeDuration:
		return strconv.AppendInt(buf, f.Integer, 10)
	case FieldTypeErr:
		if f.Interface != nil {
			return appendJSONString(buf, f.Interface.(error).Error())
		}
		return append(buf, "null"...)
	case FieldTypeAny:
		return appendJSONString(buf, fmt.Sprintf("%v", f.Interface))
	default:
		return append(buf, "null"...)
	}
}
```

Note: imports `"math"`, `"fmt"`, `"strconv"`, `"strings"`, `"time"`.

- [ ] **Step 2: Create `encoder_logfmt.go`**

```go
package mlog

import (
	"strconv"
	"strings"
	"time"
)

type logfmtEncoder struct{}

func (e *logfmtEncoder) EncodeEntry(entry *Entry) []byte {
	bp := getEncBuf()
	buf := *bp

	t := time.Unix(0, entry.Time)
	buf = append(buf, "ts="...)
	buf = append(buf, t.Format(time.RFC3339Nano)...)

	buf = append(buf, " level="...)
	buf = append(buf, entry.Severity.String()...)

	file := entry.File
	if i := strings.LastIndex(file, "/"); i >= 0 {
		file = file[i+1:]
	}
	buf = append(buf, " caller="...)
	buf = append(buf, file...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(entry.Line), 10)

	buf = append(buf, " msg="...)
	buf = appendLogfmtString(buf, entry.Message)

	for _, f := range entry.Fields {
		buf = append(buf, ' ')
		buf = append(buf, f.Key...)
		buf = append(buf, '=')
		buf = appendFieldLogfmtVal(buf, f)
	}

	buf = append(buf, '\n')

	*bp = buf
	return *bp
}

func appendLogfmtString(buf []byte, s string) []byte {
	needsQuote := strings.ContainsAny(s, " \t\n\r\"=")
	if needsQuote {
		buf = append(buf, '"')
		buf = append(buf, s...)
		buf = append(buf, '"')
		return buf
	}
	return append(buf, s...)
}

func appendFieldLogfmtVal(buf []byte, f Field) []byte {
	switch f.Type {
	case FieldTypeInt64:
		return strconv.AppendInt(buf, f.Integer, 10)
	case FieldTypeFloat64:
		return strconv.AppendFloat(buf, math.Float64frombits(uint64(f.Integer)), 'f', -1, 64)
	case FieldTypeString:
		return appendLogfmtString(buf, f.String)
	case FieldTypeBool:
		if f.Integer != 0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case FieldTypeDuration:
		return append(buf, time.Duration(f.Integer).String()...)
	case FieldTypeErr:
		if f.Interface != nil {
			return appendLogfmtString(buf, f.Interface.(error).Error())
		}
		return buf
	case FieldTypeAny:
		return appendLogfmtString(buf, fmt.Sprintf("%v", f.Interface))
	default:
		return buf
	}
}
```

Note: imports `"fmt"`, `"math"`, `"strconv"`, `"strings"`, `"time"`.

- [ ] **Step 3: Add tests to `encoder_test.go`**

Append these tests to the existing `encoder_test.go`:

```go
func TestJSONEncoderWithFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "hello",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("count", 42),
			String("name", "test"),
			Bool("ok", true),
			Float64("ratio", 3.14),
		},
	}

	enc := &jsonEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, `"count":42`) {
		t.Fatalf("missing int field in: %s", s)
	}
	if !strings.Contains(s, `"name":"test"`) {
		t.Fatalf("missing string field in: %s", s)
	}
	if !strings.Contains(s, `"ok":true`) {
		t.Fatalf("missing bool field in: %s", s)
	}
	if !strings.Contains(s, `"msg":"hello"`) {
		t.Fatalf("missing msg in: %s", s)
	}
	if !strings.HasPrefix(s, "{") || !strings.Contains(s, "}\n") {
		t.Fatalf("not JSON object: %s", s)
	}
}

func TestLogfmtEncoderWithFields(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "hello",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("count", 42),
			String("name", "test"),
		},
	}

	enc := &logfmtEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "level=INFO") {
		t.Fatalf("missing level in: %s", s)
	}
	if !strings.Contains(s, "count=42") {
		t.Fatalf("missing int field in: %s", s)
	}
	if !strings.Contains(s, "name=test") {
		t.Fatalf("missing string field in: %s", s)
	}
}
```

- [ ] **Step 4: Build and run tests**

Run: `go build ./... && go test -run "TestJSONEncoder|TestLogfmtEncoder" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add encoder_json.go encoder_logfmt.go encoder_test.go
git commit -m "feat(structured): add JSONEncoder and LogfmtEncoder implementations"
```

---

## Task 5: Integrate into Pipeline (logEntry + asyncWriter + flag)

**Files:**
- Modify: `logsink.go` — add `entry *Entry` to `logEntry`
- Modify: `async_writer.go` — `writeBatch()` handles entry path
- Modify: `mlog_flags.go` — add `-log_encoder` flag

- [ ] **Step 1: Modify `logsink.go`**

In `logsink.go`, find the `logEntry` struct (around line 165) and add the `entry` field:

Change:
```go
type logEntry struct {
	data   []byte
	meta   *LogsinkMeta
	ack    chan struct{}
	refCnt atomic.Int32
}
```
To:
```go
type logEntry struct {
	data   []byte     // old API: pre-formatted text
	entry  *Entry     // new API: unencoded Entry (data is nil)
	meta   *LogsinkMeta
	ack    chan struct{}
	refCnt atomic.Int32
}
```

- [ ] **Step 2: Modify `async_writer.go`**

In `async_writer.go`, replace the `writeBatch` method body (around line 39). The existing code writes `entry.data` directly. Change it to handle both paths:

Change the inner loop body from:
```go
if _, err := bw.buf.Write(entry.data); err != nil {
	return err
}
if entry.refCnt.Add(-1) == 0 {
	putEntryBuf(&entry.data)
	...
}
```
To:
```go
var data []byte
if entry.data != nil {
	data = entry.data
} else if entry.entry != nil {
	data = getEncoder().EncodeEntry(entry.entry)
}
if len(data) > 0 {
	if _, err := bw.buf.Write(data); err != nil {
		return err
	}
}
if entry.refCnt.Add(-1) == 0 {
	if entry.data != nil {
		putEntryBuf(&entry.data)
	}
	if entry.entry != nil {
		putEncBuf(&data)
		putEntry(entry.entry)
	}
	if entry.ack != nil {
		bw.pendingAck = append(bw.pendingAck, entry.ack)
	}
	logEntryPool.Put(entry)
}
```

- [ ] **Step 3: Modify `mlog_flags.go`**

Add `logEncoderFlag` to the existing flag block (near line 350):

```go
var (
	logEncoderFlag = flag.String("log_encoder", "text", "Log encoder: text, json, logfmt")
)
```

Add encoder init logic. In `encoder.go`, add an `initEncoder()` function and call it from `getEncoder()` (lazy, after flag.Parse):

```go
var encoderOnce sync.Once

func getEncoder() Encoder {
	encoderOnce.Do(func() {
		switch *logEncoderFlag {
		case "json":
			activeEncoder.Store(&jsonEncoder{})
		case "logfmt":
			activeEncoder.Store(&logfmtEncoder{})
		default:
			activeEncoder.Store(defaultTextEncoder)
		}
	})
	if v := activeEncoder.Load(); v != nil {
		return v.(Encoder)
	}
	return defaultTextEncoder
}
```

Note: `logEncoderFlag` is defined in `mlog_flags.go` and referenced in `encoder.go` — same package, no import needed. Remove the old `activeEncoder` Load from the existing `getEncoder()` and replace with the `encoderOnce` version.

- [ ] **Step 4: Build and run all existing tests**

Run: `go build ./... && go test -run "TestRingBuffer|TestAsyncWriter|TestSampler" -v -count=1 -race`
Expected: All existing tests still PASS (no regressions)

- [ ] **Step 5: Commit**

```bash
git add logsink.go async_writer.go mlog_flags.go encoder.go
git commit -m "feat(structured): integrate Entry into pipeline, add -log_encoder flag"
```

---

## Task 6: StructuredLogger API + S()

**Files:**
- Create: `structured.go`
- Modify: `mlog.go` — add `S()` function
- Create: `structured_test.go`

- [ ] **Step 1: Create `structured.go`**

```go
package mlog

import (
	"runtime"
	"sync/atomic"
	"time"
)

// StructuredLogger provides type-safe structured logging.
type StructuredLogger struct {
	fields []Field
}

var globalStructured = &StructuredLogger{}

// S returns the global StructuredLogger for structured log output.
func S() *StructuredLogger { return globalStructured }

// With returns a new StructuredLogger with pre-set fields appended to any existing ones.
func (s *StructuredLogger) With(fields ...Field) *StructuredLogger {
	merged := make([]Field, 0, len(s.fields)+len(fields))
	merged = append(merged, s.fields...)
	merged = append(merged, fields...)
	return &StructuredLogger{fields: merged}
}

func (s *StructuredLogger) Info(msg string, fields ...Field) {
	s.log(Severity_Info, msg, fields)
}

func (s *StructuredLogger) Warning(msg string, fields ...Field) {
	s.log(Severity_Warning, msg, fields)
}

func (s *StructuredLogger) Error(msg string, fields ...Field) {
	s.log(Severity_Error, msg, fields)
}

func (s *StructuredLogger) Fatal(msg string, fields ...Field) {
	s.log(Severity_Fatal, msg, fields)
}

func (s *StructuredLogger) log(sev Severity, msg string, fields []Field) {
	// Severity gate
	if sev < Severity_Debug || sev > Severity_Fatal {
		return
	}

	// Caller info
	pcs := [1]uintptr{}
	if runtime.Callers(3, pcs[:]) < 1 {
		return
	}
	frame, _ := runtime.CallersFrames(pcs[:]).Next()

	// Build Entry
	entry := getEntry()
	entry.Severity = sev
	entry.Time = timeNow().UnixNano()
	entry.Message = msg
	entry.File = frame.File
	entry.Line = frame.Line
	entry.Funcname = frame.Function
	entry.Thread = int64(pid)

	// Merge pre-set fields + call-site fields
	totalFields := len(s.fields) + len(fields)
	if totalFields > 0 {
		entry.Fields = append(entry.Fields[:0], s.fields...)
		entry.Fields = append(entry.Fields, fields...)
	} else {
		entry.Fields = entry.Fields[:0]
	}

	// Rate limit check
	if sampler := getSampler(); sampler != nil {
		if !sampler.allowSeverity(sev) {
			atomic.AddInt64(&Stats.Dropped.lines, 1)
			putEntry(entry)
			return
		}
	}

	// Emit through fileSinkSet
	structuredEmit(entry, sev)
}

func structuredEmit(entry *Entry, sev Severity) {
	fileSev := sev
	if fileSev >= Severity_Fatal {
		fileSev = Severity_Error
	}

	fss := &sinks.file
	fss.mu.Lock()
	for s := Severity_Debug; s <= fileSev; s++ {
		if fss.writers[s] == nil {
			fs := &fileSink{}
			sb := &syncBuffer{sink: fs, sev: s}
			if err := sb.rotateFile(timeNow()); err != nil {
				fss.mu.Unlock()
				return
			}
			fs.file = sb
			fss.sinks[s] = fs
			bw := newBatchWriter(s, fss.rings[s], sb, *batchSizeFlag)
			fss.writers[s] = newAsyncWriter(bw, *batchSizeFlag)
		}
	}
	fss.mu.Unlock()

	numRings := int(fileSev) + 1
	le := logEntryPool.Get().(*logEntry)
	le.data = nil
	le.entry = entry
	le.ack = nil
	le.refCnt.Store(int32(numRings))

	if sev >= Severity_Error {
		le.ack = make(chan struct{})
	}

	for s := Severity_Debug; s <= fileSev; s++ {
		if !fss.rings[s].tryPush(le) {
			atomic.AddInt64(&Stats.Dropped.lines, 1)
			fss.rings[s].dropped.Add(1)
		}
		fss.writers[s].wake()
	}

	if sev >= Severity_Error {
		select {
		case <-le.ack:
		case <-time.After(5 * time.Second):
		}
	}
}
```

- [ ] **Step 2: Add `S()` to `mlog.go`**

The `S()` function is already defined in `structured.go` as a package-level function. No modification to `mlog.go` is needed — `S()` is accessible via the package. Remove the `mlog.go` modification from the plan since it's already in `structured.go`.

- [ ] **Step 3: Create `structured_test.go`**

```go
package mlog

import (
	"strings"
	"testing"
)

func TestStructuredLogInfo(t *testing.T) {
	// Basic smoke test: S().Info should not panic
	S().Info("hello", String("key", "value"))
}

func TestStructuredLogWithFields(t *testing.T) {
	logger := S().With(String("request_id", "abc123"))
	logger.Info("handling request", Int("status", 200))
}

func TestStructuredLogAllSeverities(t *testing.T) {
	S().Info("info msg")
	S().Warning("warning msg")
	S().Error("error msg")
}

func TestTextEncoderIntegration(t *testing.T) {
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	e := &Entry{
		Severity: Severity_Info,
		Time:     now.UnixNano(),
		Message:  "test",
		File:     "main.go",
		Line:     1,
		Fields: []Field{
			Int("code", 200),
			Bool("ok", true),
			Duration(5e9),
		},
	}

	enc := getEncoder()
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, "code=200") {
		t.Fatalf("TextEncoder missing field: %s", s)
	}
	if !strings.Contains(s, "ok=true") {
		t.Fatalf("TextEncoder missing bool: %s", s)
	}
}

func TestJSONEncoderIntegration(t *testing.T) {
	SetEncoder(&jsonEncoder{})
	defer SetEncoder(defaultTextEncoder)

	e := &Entry{
		Severity: Severity_Info,
		Time:     time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC).UnixNano(),
		Message:  "test",
		File:     "main.go",
		Line:     1,
		Fields:   []Field{Int("code", 200)},
	}

	enc := getEncoder()
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)

	s := string(out)
	if !strings.Contains(s, `"code":200`) {
		t.Fatalf("JSONEncoder missing field: %s", s)
	}
}
```

Note: The `Duration` field constructor in the test needs a key: change `Duration(5e9)` to `Duration("elapsed", 5*time.Second)`.

- [ ] **Step 4: Build and run tests**

Run: `go build ./... && go test -run "TestStructured|TestTextEncoder|TestJSONEncoder" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add structured.go structured_test.go
git commit -m "feat(structured): add StructuredLogger API with S(), With(), and severity methods"
```

---

## Task 7: Benchmarks + Final Verification

**Files:**
- Create: `structured_bench_test.go`

- [ ] **Step 1: Create `structured_bench_test.go`**

```go
package mlog

import (
	"testing"
	"time"
)

func BenchmarkFieldConstruction(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Int("count", 42)
		_ = String("name", "test")
		_ = Bool("ok", true)
		_ = Float64("ratio", 3.14)
	}
}

func BenchmarkTextEncoderEncode(b *testing.B) {
	enc := &textEncoder{}
	entry := &Entry{
		Severity: Severity_Info,
		Time:     time.Now().UnixNano(),
		Message:  "benchmark log message",
		File:     "main.go",
		Line:     42,
		Funcname: "main.main",
		Thread:   12345,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
			Duration("elapsed", 5*time.Millisecond),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := enc.EncodeEntry(entry)
		putEncBuf(&out)
	}
}

func BenchmarkJSONEncoderEncode(b *testing.B) {
	enc := &jsonEncoder{}
	entry := &Entry{
		Severity: Severity_Info,
		Time:     time.Now().UnixNano(),
		Message:  "benchmark log message",
		File:     "main.go",
		Line:     42,
		Fields: []Field{
			Int("status", 200),
			String("method", "GET"),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := enc.EncodeEntry(entry)
		putEncBuf(&out)
	}
}

func BenchmarkEntryPool(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e := getEntry()
		putEntry(e)
	}
}
```

- [ ] **Step 2: Run benchmarks**

Run: `go test -bench="BenchmarkField|BenchmarkTextEncoder|BenchmarkJSONEncoder|BenchmarkEntryPool" -benchmem -run=^$ -count=1`
Expected:
- `BenchmarkFieldConstruction`: 0 allocs
- `BenchmarkTextEncoderEncode`: ~1 alloc (buffer from pool)
- `BenchmarkJSONEncoderEncode`: ~1 alloc (buffer from pool)
- `BenchmarkEntryPool`: 0 allocs

- [ ] **Step 3: Run full test suite with race detector**

Run: `go test ./... -race -count=1 2>&1 | tail -20`
Expected: All tests pass (pre-existing failures in TestCallerTextSkip/TestCallerPC are unrelated)

- [ ] **Step 4: Commit**

```bash
git add structured_bench_test.go
git commit -m "test(perf): add Phase 4 structured logging benchmarks"
```

---

## Self-Review

### Spec Coverage

| Spec Requirement | Task |
|---|---|
| Field tagged union + constructors | Task 1 |
| Entry struct + sync.Pool | Task 2 |
| Encoder interface | Task 3 |
| TextEncoder | Task 3 |
| JSONEncoder | Task 4 |
| LogfmtEncoder | Task 4 |
| `-log_encoder` flag | Task 5 |
| StructuredLogger API (S/With/Info/Error/Warning/Fatal) | Task 6 |
| logEntry `entry *Entry` field | Task 5 |
| asyncWriter writeBatch dual path | Task 5 |
| Rate limit check in structured path | Task 6 |
| Zero-alloc hot path | Task 7 (verified via benchmarks) |
| Benchmarks | Task 7 |

### Placeholder Scan

No TBD, TODO, "implement later", or vague steps found.

### Type Consistency

- `Field` struct fields match across `field.go`, `encoder_text.go`, `encoder_json.go`, `encoder_logfmt.go`
- `Entry` struct fields match between `entry.go` and `structured.go`
- `logEntry.entry` is `*Entry` — matches `getEntry()` return type
- `getEncoder()` returns `Encoder` — matches `textEncoder`, `jsonEncoder`, `logfmtEncoder` receivers
- `encBufPool` and `putEncBuf` used consistently in all encoders
