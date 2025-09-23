//go:build linux

package mlog

import (
	"errors"
	"runtime"
	"syscall"
)

// abortProcess attempts to kill the current process in a way that will dump the
// currently-running goroutines someplace useful (like stderr).
//
// It does this by sending SIGABRT to the current thread.
//
// If successful, abortProcess does not return.
func abortProcess() error {
	runtime.LockOSThread()
	if err := syscall.Tgkill(syscall.Getpid(), syscall.Gettid(), syscall.SIGABRT); err != nil {
		return err
	}
	return errors.New("log: killed current thread with SIGABRT, but still running")
}
