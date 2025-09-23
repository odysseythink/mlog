//go:build !(unix || windows)

package mlog

import (
	"fmt"
	"runtime"
)

// abortProcess returns an error on platforms that presumably don't support signals.
func abortProcess() error {
	return fmt.Errorf("not sending SIGABRT (%s/%s does not support signals), falling back", runtime.GOOS, runtime.GOARCH)

}
