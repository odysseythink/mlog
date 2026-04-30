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
	val := map[string]int{"a": 1}
	f := Any("data", val)
	if f.Type != FieldTypeAny || f.Interface.(map[string]int)["a"] != 1 {
		t.Fatalf("Any field wrong: %+v", f)
	}
}
