// File I/O for logs.

package mlog

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// logDirs lists the candidate directories for new log files.
var logDirs []string

var (
	// If non-empty, overrides the choice of directory in which to write logs.
	// See createLogDirs for the full list of possible destinations.
	logDir      = flag.String("log_dir", "", "If non-empty, write log files in this directory")
	logLink     = flag.String("log_link", "", "If non-empty, add symbolic links in this directory to the log files")
	logBufLevel = flag.Int("logbuflevel", int(Severity_Info), "Buffer log messages logged at this level or lower"+
		" (-1 means don't buffer; 0 means buffer INFO only; ...). Has limited applicability on non-prod platforms.")
)

func SetLogDir(path string) {
	*logDir = path
}

func SetLogLevel[T int | int16 | int32 | int64 | uint | uint16 | uint32 | uint64](level T) {
	*logBufLevel = int(level)
}

// 设置最大日志文件的大小,单位为M
func SetMaxLogSize(sz int) {
	MaxSize = uint64(sz * 1024 * 1024)
}

func pathExist(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		if os.IsNotExist(err) {
			return false
		}
		return false
	}
	return true
}

func createLogDirs() {
	if *logDir != "" {
		if !pathExist(*logDir) {
			err := os.MkdirAll(*logDir, os.ModePerm)
			if err != nil {
				fmt.Printf("createLogDirs(%s) falied:%v", *logDir, err)
			}
		}
		logDirs = append(logDirs, *logDir)
	}
	logDirs = append(logDirs, os.TempDir())
}

var (
	pid      = os.Getpid()
	program  = filepath.Base(os.Args[0])
	host     = "unknownhost"
	userName = "unknownuser"
)

func init() {
	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}

	if u := lookupUser(); u != "" {
		userName = u
	}
	// Sanitize userName since it is used to construct file paths.
	userName = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		default:
			return '_'
		}
		return r
	}, userName)
}

// shortHostname returns its argument, truncating at the first period.
// For instance, given "www.google.com" it returns "www".
func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

// logName returns a new log file name containing tag, with start time t, and
// the name for the symlink for tag.
func logName(tag string, t time.Time) (name, link string) {
	shortprogram := program
	if strings.HasSuffix(program, ".exe") {
		shortprogram = strings.TrimSuffix(program, ".exe")
	}

	name = fmt.Sprintf("%s-%04d%02d%02d-%02d%02d%02d.log",
		shortprogram,
		t.Year(),
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second())

	return name, program + "." + tag
}

var onceLogDirs sync.Once

// create creates a new log file and returns the file and its filename, which
// contains tag ("INFO", "FATAL", etc.) and t.  If the file is created
// successfully, create also attempts to update the symlink for that tag, ignoring
// errors.
func create(tag string, t time.Time, dir string) (f *os.File, filename string, err error) {
	if dir != "" {
		f, name, err := createInDir(dir, tag, t)
		if err == nil {
			return f, name, err
		}
		return nil, "", fmt.Errorf("log: cannot create log: %v", err)
	}

	onceLogDirs.Do(createLogDirs)
	if len(logDirs) == 0 {
		return nil, "", errors.New("log: no log dirs")
	}
	var lastErr error
	for _, dir := range logDirs {
		f, name, err := createInDir(dir, tag, t)
		if err == nil {
			return f, name, err
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("log: cannot create log: %v", lastErr)
}

func createInDir(dir, tag string, t time.Time) (f *os.File, name string, err error) {
	name, link := logName(tag, t)
	fname := filepath.Join(dir, name)
	// O_EXCL is important here, as it prevents a vulnerability. The general idea is that logs often
	// live in an insecure directory (like /tmp), so an unprivileged attacker could create fname in
	// advance as a symlink to a file the logging process can access, but the attacker cannot. O_EXCL
	// fails the open if it already exists, thus prevent our this code from opening the existing file
	// the attacker points us to.
	f, err = os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err == nil {
		symlink := filepath.Join(dir, link)
		os.Remove(symlink)        // ignore err
		os.Symlink(name, symlink) // ignore err
		if *logLink != "" {
			lsymlink := filepath.Join(*logLink, link)
			os.Remove(lsymlink)         // ignore err
			os.Symlink(fname, lsymlink) // ignore err
		}
		return f, fname, nil
	}
	return nil, "", err
}

// flushSyncWriter is the interface satisfied by logging destinations.
type flushSyncWriter interface {
	Flush() error
	Sync() error
	io.Writer
	filenames() []string
}

// fileSinkSet manages per-severity ring buffers, async writers, and file sinks.
type fileSinkSet struct {
	rings   [numSeverity]*ringBuffer
	writers [numSeverity]*asyncWriter
	sinks   [numSeverity]*fileSink
	mu      sync.Mutex
}

var sinks struct {
	stderr stderrSink
	file   fileSinkSet
}

var logSampler atomic.Value // *sampler, nil when disabled

func getSampler() *sampler {
	if s := logSampler.Load(); s != nil {
		return s.(*sampler)
	}
	return nil
}

func initSampler() {
	if *logRateLimit > 0 {
		rate := int64(*logRateLimit)
		logSampler.Store(newSampler(rate, rate))
	}
}

func init() {
	// Register stderr first: that way if we crash during file-writing at least
	// the log will have gone somewhere.
	if shouldRegisterStderrSink() {
		TextSinks = append(TextSinks, &sinks.stderr)
	}
	TextSinks = append(TextSinks, &sinks.file)

	// Initialize per-severity rings
	for i := 0; i < numSeverity; i++ {
		sinks.file.rings[i] = newRingBuffer(*ringSizeFlag)
	}

	initSampler()
}

// stderrSink is a TextSink that writes log entries to stderr
// if they meet certain conditions.
type stderrSink struct {
	mu sync.Mutex
	w  io.Writer // if nil Emit uses os.Stderr directly
}

// Enabled implements TextSink.Enabled.  It returns true if any of the
// various stderr flags are enabled for logs of the given severity, if the log
// message is from the standard "log" package, or if google.Init has not yet run
// (and hence file logging is not yet initialized).
func (s *stderrSink) Enabled(m *LogsinkMeta) bool {
	return toStderr || alsoToStderr || m.Severity >= stderrThreshold.get()
}

// Emit implements TextSink.Emit.
func (s *stderrSink) Emit(m *LogsinkMeta, data []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.w
	if w == nil {
		w = os.Stderr
	}
	dn, err := w.Write(data)
	n += dn
	return n, err
}

// fileSink is a per-severity file sink used by fileSinkSet.
type fileSink struct {
	mu   sync.Mutex
	file flushSyncWriter
}

// Enabled implements TextSink.Enabled.
func (fss *fileSinkSet) Enabled(m *LogsinkMeta) bool {
	return !toStderr
}

// Emit implements TextSink.Emit with multi-sink fan-out.
func (fss *fileSinkSet) Emit(m *LogsinkMeta, data []byte) (n int, err error) {
	sev := m.Severity
	if sev >= Severity_Fatal {
		sev = Severity_Error
	}

	// Lazy-init writers and files on first use
	fss.mu.Lock()
	for s := Severity_Debug; s <= sev; s++ {
		if fss.writers[s] == nil {
			fs := &fileSink{}
			sb := &syncBuffer{sink: fs, sev: s}
			if err := sb.rotateFile(timeNow()); err != nil {
				fss.mu.Unlock()
				return 0, err
			}
			fs.file = sb
			fss.sinks[s] = fs
			bw := newBatchWriter(s, fss.rings[s], sb, *batchSizeFlag)
			fss.writers[s] = newAsyncWriter(bw, *batchSizeFlag)
		}
	}
	fss.mu.Unlock()

	// Acquire entry and set refCount = number of rings it will be pushed to
	numRings := int(sev) + 1
	entry := acquireEntry(data, m, numRings)

	// Publish into rings in ascending severity order
	dropped := false
	for s := Severity_Debug; s <= sev; s++ {
		if !fss.rings[s].tryPush(entry) {
			dropped = true
			fss.rings[s].dropped.Add(1)
		}
		fss.writers[s].wake()
	}

	if dropped {
		atomic.AddInt64(&Stats.Dropped.lines, 1)
		atomic.AddInt64(&Stats.Dropped.bytes, int64(len(data)))
	}

	// ERROR and above: block on ack for durable visibility
	if m.Severity >= Severity_Error {
		select {
		case <-entry.ack:
		case <-time.After(5 * time.Second):
			fmt.Fprintf(os.Stderr, "mlog: ERROR ack timeout\n")
		}
	}

	return len(data), nil
}

// acquireEntry gets a pooled logEntry and initializes it.
func acquireEntry(data []byte, meta *LogsinkMeta, refCount int) *logEntry {
	entry := logEntryPool.Get().(*logEntry)
	bp := entryBufPool.Get().(*[]byte)
	*bp = append((*bp)[:0], data...)
	entry.data = *bp
	entry.meta = meta
	entry.refCnt.Store(int32(refCount))
	if meta.Severity >= Severity_Error {
		entry.ack = make(chan struct{})
	} else {
		entry.ack = nil
	}
	return entry
}

// Flush flushes all pending log I/O.
func Flush() {
	for i := 0; i < numSeverity; i++ {
		if w := sinks.file.writers[i]; w != nil {
			w.flush()
		}
	}
}

// Close gracefully shuts down all writers.
func Close() error {
	for i := 0; i < numSeverity; i++ {
		if w := sinks.file.writers[i]; w != nil {
			w.close()
		}
	}
	return nil
}

// FatalShutdown drains all rings, writes FATAL entry to all files, fsyncs, and exits.
// Must be called before os.Exit on Fatal paths.
func FatalShutdown(fatalData []byte) {
	for i := 0; i < numSeverity; i++ {
		if sinks.file.writers[i] != nil {
			sinks.file.writers[i].close()
		}
	}

	// Write FATAL entry synchronously to all severity files
	for s := Severity_Debug; s < numSeverity; s++ {
		if sink := sinks.file.sinks[s]; sink != nil && sink.file != nil {
			sink.mu.Lock()
			sink.file.Write(fatalData)
			sink.file.Flush()
			sink.file.Sync()
			sink.mu.Unlock()
		}
	}
}

// Names returns the names of the log files holding the FATAL, ERROR,
// WARNING, or INFO logs. Returns ErrNoLog if the log for the given
// level doesn't exist (e.g. because no messages of that level have been
// written). This may return multiple names if the log type requested
// has rolled over.
func Names(s string) ([]string, error) {
	sev, err := ParseSeverity(s)
	if err != nil {
		return nil, err
	}
	if sev >= Severity_Fatal {
		sev = Severity_Error
	}

	sinks.file.mu.Lock()
	defer sinks.file.mu.Unlock()
	f := sinks.file.sinks[sev]
	if f == nil || f.file == nil {
		return nil, ErrNoLog
	}

	return f.file.filenames(), nil
}
