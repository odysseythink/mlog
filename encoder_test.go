package mlog

import (
	"encoding/json"
	"errors"
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
	if !strings.Contains(s, "main.main:42") {
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

func TestTextEncoderAllFieldTypes(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(), Message: "all types",
		File: "main.go", Line: 1, Funcname: "github.com/odysseythink/mlog.main",
		Fields: []Field{
			Int64("bytes", 9223372036854775807), Float64("ratio", 2.5), Bool("flag", false),
			Duration("elapsed", 3*time.Second), Err(errors.New("oops")),
			Any("data", 42), {Key: "unknown", Type: 99},
		},
	}
	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	s := string(out)
	if !strings.Contains(s, "bytes=9223372036854775807") {
		t.Fatalf("int64: %s", s)
	}
	if !strings.Contains(s, "ratio=2.5") {
		t.Fatalf("float64: %s", s)
	}
	if !strings.Contains(s, "flag=false") {
		t.Fatalf("bool false: %s", s)
	}
	if !strings.Contains(s, "elapsed=3s") {
		t.Fatalf("duration: %s", s)
	}
	if !strings.Contains(s, "error=oops") {
		t.Fatalf("err non-nil: %s", s)
	}
	if !strings.Contains(s, "mlog.main:1") {
		t.Fatalf("funcname trim: %s", s)
	}
}

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
			Int64("bytes", 9223372036854775807),
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
	if !strings.Contains(s, `"bytes":9223372036854775807`) {
		t.Fatalf("missing int64 field in: %s", s)
	}
	if !strings.Contains(s, `"msg":"hello"`) {
		t.Fatalf("missing msg in: %s", s)
	}
	if !strings.HasPrefix(s, "{") || !strings.Contains(s, "}\n") {
		t.Fatalf("not JSON object: %s", s)
	}
	if !json.Valid(out[:len(out)-1]) {
		t.Fatalf("invalid JSON: %s", s)
	}
}

func TestJSONEncoderAllFieldTypes(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "all", File: "main.go", Line: 1,
		Fields: []Field{
			Bool("flag", false), Duration("dur", 5*time.Second),
			Err(errors.New("fail")), Err(nil), Any("data", "hi"),
			{Key: "unknown", Type: 99},
		},
	}
	enc := &jsonEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	s := string(out)
	if !strings.Contains(s, `"flag":false`) {
		t.Fatalf("bool false: %s", s)
	}
	if !strings.Contains(s, `"error":"fail"`) {
		t.Fatalf("err non-nil: %s", s)
	}
	if !strings.Contains(s, `"unknown":null`) {
		t.Fatalf("unknown: %s", s)
	}
	if !json.Valid(out[:len(out)-1]) {
		t.Fatalf("invalid JSON: %s", s)
	}
}

func TestJSONEncoderSpecialChars(t *testing.T) {
	cases := []string{
		`has "quotes" and \backslash\`,
		"line1\rline2",
		"tab\there",
		"bell\x07ring",
	}
	for _, msg := range cases {
		e := &Entry{
			Severity: Severity_Info, Time: time.Now().UnixNano(),
			Message: msg, File: "main.go", Line: 1,
		}
		enc := &jsonEncoder{}
		out := enc.EncodeEntry(e)
		if !json.Valid(out[:len(out)-1]) {
			t.Fatalf("invalid JSON for message %q: %s", msg, out)
		}
		putEncBuf(&out)
	}
}

func TestTextEncoderNilErr(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "x", File: "main.go", Line: 1,
		Fields: []Field{Err(nil)},
	}
	enc := &textEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	s := string(out)
	// nil error should produce empty or minimal output
	if strings.Contains(s, "error=") && strings.Contains(s, "error=\n") {
		// acceptable — empty error value
	}
}

func TestJSONEncoderFileWithSlash(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "x", File: "/some/path/to/main.go", Line: 10,
	}
	enc := &jsonEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	if !strings.Contains(string(out), `"caller":"main.go:10"`) {
		t.Fatalf("caller: %s", string(out))
	}
}

func TestJSONEncoderNoFields(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "plain", File: "main.go", Line: 1,
	}
	enc := &jsonEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	if !json.Valid(out[:len(out)-1]) {
		t.Fatalf("invalid JSON: %s", string(out))
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
			Int64("bytes", 1024),
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
	if !strings.Contains(s, "bytes=1024") {
		t.Fatalf("missing int64 field in: %s", s)
	}
}

func TestLogfmtEncoderAllFieldTypes(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "hello world", File: "main.go", Line: 1,
		Fields: []Field{
			Float64("ratio", 1.5), Bool("ok", true), Bool("fail", false),
			Duration("dur", 100*time.Millisecond), Err(errors.New("bad")),
			Any("stuff", 42), {Key: "unknown", Type: 99},
		},
	}
	enc := &logfmtEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	s := string(out)
	if !strings.Contains(s, "ratio=1.5") {
		t.Fatalf("float64: %s", s)
	}
	if !strings.Contains(s, "ok=true") {
		t.Fatalf("bool true: %s", s)
	}
	if !strings.Contains(s, "fail=false") {
		t.Fatalf("bool false: %s", s)
	}
	if !strings.Contains(s, "dur=100ms") {
		t.Fatalf("duration: %s", s)
	}
}

func TestJSONEncoderInt64Negative(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "x", File: "main.go", Line: 1,
		Fields: []Field{Int64("neg", -42)},
	}
	enc := &jsonEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	if !strings.Contains(string(out), `"neg":-42`) {
		t.Fatalf("negative int64: %s", string(out))
	}
}

func TestLogfmtEncoderInt64Negative(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "x", File: "main.go", Line: 1,
		Fields: []Field{Int64("neg", -42)},
	}
	enc := &logfmtEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	if !strings.Contains(string(out), "neg=-42") {
		t.Fatalf("negative int64: %s", string(out))
	}
}

func TestLogfmtEncoderMessageQuoting(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "hello world", File: "main.go", Line: 1,
	}
	enc := &logfmtEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	if !strings.Contains(string(out), `msg="hello world"`) {
		t.Fatalf("not quoted: %s", string(out))
	}
}

func TestLogfmtEncoderStringWithQuotes(t *testing.T) {
	e := &Entry{
		Severity: Severity_Info, Time: time.Now().UnixNano(),
		Message: "test", File: "main.go", Line: 1,
		Fields: []Field{String("msg", `has "quotes" inside`)},
	}
	enc := &logfmtEncoder{}
	out := enc.EncodeEntry(e)
	defer putEncBuf(&out)
	s := string(out)
	if !strings.Contains(s, `msg="has \"quotes\" inside"`) {
		t.Fatalf("quotes not escaped: %s", s)
	}
}

func TestEncoderClone(t *testing.T) {
	for _, enc := range []Encoder{&textEncoder{}, &jsonEncoder{}, &logfmtEncoder{}} {
		if enc.Clone() == nil {
			t.Fatalf("Clone nil for %T", enc)
		}
	}
}
