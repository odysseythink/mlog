package mlog

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type textEncoder struct{}

func (e *textEncoder) Clone() Encoder { return e }

func (e *textEncoder) EncodeEntry(entry *Entry) []byte {
	bp := getEncBuf()
	buf := *bp

	// Header: [YYYY-MM-DD HH:MM:SS.uuuuuu][S][PID][file func:line]
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
	buf = append(buf, strconv.FormatInt(int64(entry.Line), 10)...)
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

// appendDigits appends an n-digit integer to buf, left-padded with pad.
// This is the []byte equivalent of nDigits (which operates on *bytes.Buffer).
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
