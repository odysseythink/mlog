//go:build (unix || windows) && !linux

package mlog

import (
	"os"
	"syscall"
	"time"
)

// abortProcess attempts to kill the current process in a way that will dump the
// currently-running goroutines someplace useful (like stderr).
//
// It does this by sending SIGABRT to the current process. Unfortunately, the
// signal may or may not be delivered to the current thread; in order to do that
// portably, we would need to add a cgo dependency and call pthread_kill.
//
// If successful, abortProcess does not return.
func abortProcess() error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGABRT); err != nil {
		return err
	}

	// Sent the signal.  Now we wait for it to arrive and any SIGABRT handlers to
	// run (and eventually terminate the process themselves).
	//
	// We could just "select{}" here, but there's an outside chance that would
	// trigger the runtime's deadlock detector if there happen not to be any
	// background goroutines running.  So we'll sleep a while first to give
	// the signal some time.
	time.Sleep(10 * time.Second)
	select {}
}
