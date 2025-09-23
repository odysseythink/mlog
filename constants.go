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
