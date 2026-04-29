package mlog

import "time"

const (
	message = "benchmark log message"

	// bufferSize sizes the buffer associated with each log file. It's large
	// so that log records can accumulate without the logging thread blocking
	// on disk I/O. The flushDaemon will block instead.
	bufferSize = 256 * 1024

	severityChar = "DIWEF"

	digits = "0123456789"

	flushInterval = 30 * time.Second

	footer = "\nCONTINUED IN NEXT FILE\n"

	defaultEntryBufSize = 512  // Pre-allocated buffer size for log entries, covers header + average message
	maxPooledEntryBuf   = 8192 // Discard buffers above this size to prevent slab bloat

	defaultRingSize       = 4096              // Must be power of 2 for bitwise modulo
	defaultBatchSize      = 64
	periodicFlushInterval = 30 * time.Second
	bufioHighWaterMark    = 192 * 1024 // 75% of 256KB buffer
)

// severity identifies the sort of log: info, warning etc. It also implements
// the flag.Value interface. The -stderrthreshold flag is of type severity and
// should be modified only through the flag.Value interface. The values match
// the corresponding constants in C++.
type severity int32 // sync/atomic int32

// These constants identify the log levels in order of increasing severity.
// A message written to a high-severity log file is also written to each
// lower-severity log file.
const (
	debugLog severity = iota
	infoLog
	warningLog
	errorLog
	fatalLog
	numSeverity = 4
)

type stack bool

const (
	noStack   = stack(false)
	withStack = stack(true)
)
