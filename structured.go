package mlog

import (
	"runtime"
	"sync/atomic"
	"time"
)

// Logger provides a fluent API for structured logging.
// Use With() to bind persistent fields.
type Logger struct {
	fields []Field
}

// globalLogger is the default Logger with no bound fields.
var globalLogger = &Logger{}

// With returns a new Logger that merges the given fields with any fields
// already bound to the global logger.
func With(fields ...Field) *Logger {
	return globalLogger.With(fields...)
}

// With returns a new Logger that merges the given fields with
// any fields already bound to the receiver.
func (l *Logger) With(fields ...Field) *Logger {
	merged := make([]Field, 0, len(l.fields)+len(fields))
	merged = append(merged, l.fields...)
	merged = append(merged, fields...)
	return &Logger{fields: merged}
}

// Info logs a structured message at INFO severity.
func (l *Logger) Info(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Info, msg, fields)
	} else {
		InfoDepth(1, msg)
	}
}

// Warning logs a structured message at WARNING severity.
func (l *Logger) Warning(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Warning, msg, fields)
	} else {
		WarningDepth(1, msg)
	}
}

// Error logs a structured message at ERROR severity.
func (l *Logger) Error(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Error, msg, fields)
	} else {
		ErrorDepth(1, msg)
	}
}

// Fatal logs a structured message at FATAL severity.
func (l *Logger) Fatal(msg string, fields ...Field) {
	if getMode() == LogModeStructured {
		l.log(Severity_Fatal, msg, fields)
	} else {
		FatalDepth(1, msg)
	}
}

func (l *Logger) log(sev Severity, msg string, fields []Field) {
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

	totalFields := len(l.fields) + len(fields)
	if totalFields > 0 {
		entry.Fields = append(entry.Fields[:0], l.fields...)
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
			if le.refCnt.Add(-1) == 0 {
				putEntry(le.entry)
				le.entry = nil
				le.data = nil
				le.meta = nil
				le.ack = nil
				logEntryPool.Put(le)
			}
		} else {
			fss.writers[s].wake()
		}
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
