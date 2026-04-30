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
