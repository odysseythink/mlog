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
	"time"

	"mlib.com/mlog/internal/logsink"
)

// logDirs lists the candidate directories for new log files.
var logDirs []string

var (
	// If non-empty, overrides the choice of directory in which to write logs.
	// See createLogDirs for the full list of possible destinations.
	logDir      = flag.String("log_dir", "", "If non-empty, write log files in this directory")
	logLink     = flag.String("log_link", "", "If non-empty, add symbolic links in this directory to the log files")
	logBufLevel = flag.Int("logbuflevel", int(logsink.Info), "Buffer log messages logged at this level or lower"+
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

var sinks struct {
	stderr stderrSink
	file   fileSink
}

func init() {
	// Register stderr first: that way if we crash during file-writing at least
	// the log will have gone somewhere.
	if shouldRegisterStderrSink() {
		logsink.TextSinks = append(logsink.TextSinks, &sinks.stderr)
	}
	logsink.TextSinks = append(logsink.TextSinks, &sinks.file)
	logsink.TextSinks = append(logsink.TextSinks, mRemoteWriter)

	sinks.file.flushChan = make(chan logsink.Severity, 1)
	go sinks.file.flushDaemon()
}

// stderrSink is a logsink.Text that writes log entries to stderr
// if they meet certain conditions.
type stderrSink struct {
	mu sync.Mutex
	w  io.Writer // if nil Emit uses os.Stderr directly
}

// Enabled implements logsink.Text.Enabled.  It returns true if any of the
// various stderr flags are enabled for logs of the given severity, if the log
// message is from the standard "log" package, or if google.Init has not yet run
// (and hence file logging is not yet initialized).
func (s *stderrSink) Enabled(m *logsink.Meta) bool {
	return toStderr || alsoToStderr || m.Severity >= stderrThreshold.get()
}

// Emit implements logsink.Text.Emit.
func (s *stderrSink) Emit(m *logsink.Meta, data []byte) (n int, err error) {
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

// fileSink is a logsink.Text that prints to a set of Google log files.
type fileSink struct {
	mu sync.Mutex
	// file holds writer for each of the log types.
	file      flushSyncWriter
	flushChan chan logsink.Severity
}

// Enabled implements logsink.Text.Enabled.  It returns true if google.Init
// has run and both --disable_log_to_disk and --logtostderr are false.
func (s *fileSink) Enabled(m *logsink.Meta) bool {
	return !toStderr
}

// Emit implements logsink.Text.Emit
func (s *fileSink) Emit(m *logsink.Meta, data []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err = s.createMissingFiles(m.Severity); err != nil {
		return 0, err
	}

	n = len(data)
	if int(m.Severity) >= *logBufLevel {
		if _, fErr := s.file.Write(data); fErr != nil && err == nil {
			err = fErr // Take the first error.
		}
		select {
		case s.flushChan <- m.Severity:
		default:
		}
	}

	return n, err
}

// createMissingFiles creates all the log files for severity from infoLog up to
// upTo that have not already been created.
// s.mu is held.
func (s *fileSink) createMissingFiles(upTo logsink.Severity) error {
	if s.file != nil {
		return nil
	}
	now := time.Now()
	// Files are created in increasing severity order, so we can be assured that
	// if a high severity logfile exists, then so do all of lower severity.

	sb := &syncBuffer{
		sink: s,
		sev:  logsink.Debug,
	}
	if err := sb.rotateFile(now); err != nil {
		return err
	}
	s.file = sb

	return nil
}

// flushDaemon periodically flushes the log file buffers.
func (s *fileSink) flushDaemon() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			s.Flush()
		case sev := <-s.flushChan:
			s.flush(sev)
		}
	}
}

// Flush flushes all pending log I/O.
func Flush() {
	sinks.file.Flush()
}

// Flush flushes all the logs and attempts to "sync" their data to disk.
func (s *fileSink) Flush() error {
	return s.flush(logsink.Severity(*logBufLevel))
}

// flush flushes all logs of severity threshold or greater.
func (s *fileSink) flush(threshold logsink.Severity) error {
	var firstErr error
	updateErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Remember where we flushed, so we can call sync without holding
	// the lock.
	var files []flushSyncWriter
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Flush from fatal down, in case there's trouble flushing.
		if file := s.file; file != nil {
			updateErr(file.Flush())
			files = append(files, file)
		}
	}()

	for _, file := range files {
		updateErr(file.Sync())
	}

	return firstErr
}

// Names returns the names of the log files holding the FATAL, ERROR,
// WARNING, or INFO logs. Returns ErrNoLog if the log for the given
// level doesn't exist (e.g. because no messages of that level have been
// written). This may return multiple names if the log type requested
// has rolled over.
func Names(s string) ([]string, error) {
	_, err := logsink.ParseSeverity(s)
	if err != nil {
		return nil, err
	}

	sinks.file.mu.Lock()
	defer sinks.file.mu.Unlock()
	f := sinks.file.file
	if f == nil {
		return nil, ErrNoLog
	}

	return f.filenames(), nil
}
