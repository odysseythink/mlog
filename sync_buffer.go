package mlog

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"time"
)

// syncBuffer joins a bufio.Writer to its underlying file, providing access to the
// file's Sync method and providing a wrapper for the Write method that provides log
// file rotation. There are conflicting methods, so the file cannot be embedded.
// s.mu is held for all its methods.
type syncBuffer struct {
	sink *fileSink
	*bufio.Writer
	file   *os.File
	names  []string
	nbytes uint64 // The number of bytes written to this file
	madeAt time.Time
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	// Rotate the file if it is too large, but ensure we only do so,
	// if rotate doesn't create a conflicting filename.
	if sb.nbytes+uint64(len(p)) >= MaxSize {
		now := timeNow()
		if now.After(sb.madeAt.Add(1*time.Second)) || now.Second() != sb.madeAt.Second() {
			if err := sb.rotateFile(now); err != nil {
				return 0, err
			}
		}
	}
	n, err = sb.Writer.Write(p)
	sb.nbytes += uint64(n)
	return n, err
}

func (sb *syncBuffer) Sync() error {
	return sb.file.Sync()
}

func (sb *syncBuffer) filenames() []string {
	return sb.names
}

// rotateFile closes the syncBuffer's file and starts a new one.
func (sb *syncBuffer) rotateFile(now time.Time) error {
	var err error
	pn := "<none>"
	file, name, err := create(now, "")
	sb.madeAt = now

	if sb.file != nil {
		// The current log file becomes the previous log at the end of
		// this block, so save its name for use in the header of the next
		// file.
		pn = sb.file.Name()
		sb.Flush()
		// If there's an existing file, write a footer with the name of
		// the next file in the chain, followed by the constant string
		// \nCONTINUED IN NEXT FILE\n to make continuation detection simple.
		sb.file.Write([]byte("Next log: "))
		sb.file.Write([]byte(name))
		sb.file.Write([]byte(footer))
		sb.file.Close()
	}

	sb.file = file
	sb.names = append(sb.names, name)
	sb.nbytes = 0
	if err != nil {
		return err
	}

	sb.Writer = bufio.NewWriterSize(sb.file, bufferSize)

	// Write header.
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Log file created at: %s\n", now.Format("2006/01/02 15:04:05"))
	fmt.Fprintf(&buf, "Running on machine: %s\n", host)
	fmt.Fprintf(&buf, "Binary: Built with %s %s for %s/%s\n", runtime.Compiler, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&buf, "Previous log: %s\n", pn)
	fmt.Fprintf(&buf, "Log line format: [yyyy-mm-dd hh:mm:ss.uuuuuu][IWEF][threadid][file function:line] msg\n")
	n, err := sb.file.Write(buf.Bytes())
	sb.nbytes += uint64(n)
	return err
}
