package mlog

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type logfmtEncoder struct{}

func (e *logfmtEncoder) Clone() Encoder { return e }

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
	buf = append(buf, strconv.FormatInt(int64(entry.Line), 10)...)

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
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '"':
				buf = append(buf, '\\', '"')
			case '\\':
				buf = append(buf, '\\', '\\')
			default:
				buf = append(buf, s[i])
			}
		}
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
