package mlog

import (
	"runtime"
	"sync/atomic"
	"time"
)

// StructuredLogger provides a fluent API for structured logging.
// Use S() to obtain the global instance, and With() to bind persistent fields.
type StructuredLogger struct {
	fields []Field
}

// globalStructured is the default StructuredLogger with no bound fields.
var globalStructured = &StructuredLogger{}

// S returns the global StructuredLogger.
func S() *StructuredLogger { return globalStructured }

// With returns a new StructuredLogger that merges the given fields with
// any fields already bound to the receiver.
func (s *StructuredLogger) With(fields ...Field) *StructuredLogger {
	merged := make([]Field, 0, len(s.fields)+len(fields))
	merged = append(merged, s.fields...)
	merged = append(merged, fields...)
	return &StructuredLogger{fields: merged}
}

// Info logs a structured message at INFO severity.
func (s *StructuredLogger) Info(msg string, fields ...Field) {
	s.log(Severity_Info, msg, fields)
}

// Warning logs a structured message at WARNING severity.
func (s *StructuredLogger) Warning(msg string, fields ...Field) {
	s.log(Severity_Warning, msg, fields)
}

// Error logs a structured message at ERROR severity.
func (s *StructuredLogger) Error(msg string, fields ...Field) {
	s.log(Severity_Error, msg, fields)
}

// Fatal logs a structured message at FATAL severity.
func (s *StructuredLogger) Fatal(msg string, fields ...Field) {
	s.log(Severity_Fatal, msg, fields)
}

func (s *StructuredLogger) log(sev Severity, msg string, fields []Field) {
	if sev < Severity_Debug || sev > Severity_Fatal {
		return
	}

	pcs := [1]uintptr{}
	if runtime.Callers(3, pcs[:]) < 1 {
		return
	}
	frame, _ := runtime.CallersFrames(pcs[:]).Next()

	entry := getEntry()
	entry.Severity = sev
	entry.Time = timeNow().UnixNano()
	entry.Message = msg
	entry.File = frame.File
	entry.Line = frame.Line
	entry.Funcname = frame.Function
	entry.Thread = int64(pid)

	totalFields := len(s.fields) + len(fields)
	if totalFields > 0 {
		entry.Fields = append(entry.Fields[:0], s.fields...)
		entry.Fields = append(entry.Fields, fields...)
	} else {
		entry.Fields = entry.Fields[:0]
	}

	// Check rate limiter before emitting
	if sampler := getSampler(); sampler != nil {
		if !sampler.allowSeverity(sev) {
			atomic.AddInt64(&Stats.Dropped.lines, 1)
			putEntry(entry)
			return
		}
	}

	structuredEmit(entry, sev)
}

// structuredEmit sends a structured Entry through the file sink pipeline.
// It mirrors fileSinkSet.Emit but uses the entry *Entry path instead of
// pre-formatted data []byte.
func structuredEmit(entry *Entry, sev Severity) {
	fileSev := sev
	if fileSev >= Severity_Fatal {
		fileSev = Severity_Error
	}

	fss := &sinks.file

	// Lazy-init writers and files on first use (same pattern as fileSinkSet.Emit)
	fss.mu.Lock()
	for s := Severity_Debug; s <= fileSev; s++ {
		if fss.writers[s] == nil {
			fs := &fileSink{}
			sb := &syncBuffer{sink: fs, sev: s}
			if err := sb.rotateFile(timeNow()); err != nil {
				fss.mu.Unlock()
				putEntry(entry)
				return
			}
			fs.file = sb
			fss.sinks[s] = fs
			bw := newBatchWriter(s, fss.rings[s], sb, *batchSizeFlag)
			fss.writers[s] = newAsyncWriter(bw, *batchSizeFlag)
		}
	}
	fss.mu.Unlock()

	// Acquire a pooled logEntry and set refCount = number of rings it will be pushed to
	numRings := int(fileSev) + 1
	le := logEntryPool.Get().(*logEntry)
	le.data = nil
	le.entry = entry
	le.meta = nil
	le.ack = nil
	le.refCnt.Store(int32(numRings))

	if sev >= Severity_Error {
		le.ack = make(chan struct{})
	}

	dropped := false
	for s := Severity_Debug; s <= fileSev; s++ {
		if !fss.rings[s].tryPush(le) {
			dropped = true
			fss.rings[s].dropped.Add(1)
		}
		fss.writers[s].wake()
	}

	if dropped {
		atomic.AddInt64(&Stats.Dropped.lines, 1)
	}

	// ERROR and above: block on ack for durable visibility
	if sev >= Severity_Error {
		select {
		case <-le.ack:
		case <-time.After(5 * time.Second):
		}
	}
}
