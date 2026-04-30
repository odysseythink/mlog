package mlog

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type jsonEncoder struct{}

func (e *jsonEncoder) Clone() Encoder { return e }

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
	buf = append(buf, strconv.FormatInt(int64(entry.Line), 10)...)
	buf = append(buf, '"', ',')

	// msg
	buf = append(buf, `"msg":`...)
	buf = appendJSONString(buf, entry.Message)

	// fields
	for _, f := range entry.Fields {
		buf = append(buf, ',')
		buf = append(buf, '"')
		buf = append(buf, f.Key...)
		buf = append(buf, '"', ':')
		buf = appendFieldJSONVal(buf, f)
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
		case '\r':
			buf = append(buf, '\\', 'r')
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				buf = append(buf, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
			} else {
				var runeBuf [4]byte
				n := utf8.EncodeRune(runeBuf[:], c)
				buf = append(buf, runeBuf[:n]...)
			}
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
