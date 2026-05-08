package mlog

import (
	"strings"
	"testing"
)

func TestGetCaller(t *testing.T) {
	file, line := getCaller(0)
	if file == "" || file == "???" {
		t.Errorf("getCaller(0) returned empty/unknown file: %q", file)
	}
	if line == 0 {
		t.Error("getCaller(0) returned line 0")
	}
	if !strings.Contains(file, "utils_test.go") {
		t.Errorf("getCaller(0) file = %q, want to contain utils_test.go", file)
	}
}

func TestGetCallerWithSuffixIgnore(t *testing.T) {
	// Call from a known file; ignore suffixes that don't match.
	file, line := getCaller(0, "/nonexistent.go")
	if file == "" || file == "???" {
		t.Errorf("getCaller returned empty/unknown file: %q", file)
	}
	_ = line

	// Call ignoring the current file suffix — should skip and return deeper frame.
	file2, line2 := getCaller(0, "/utils_test.go")
	if file2 == "" || file2 == "???" {
		t.Errorf("getCaller with suffix ignore returned empty/unknown file: %q", file2)
	}
	_ = line2
}

func TestGetCallerIgnoringLogMulti(t *testing.T) {
	file, line := getCallerIgnoringLogMulti(0)
	if file == "" || file == "???" {
		t.Errorf("getCallerIgnoringLogMulti(0) returned empty/unknown file: %q", file)
	}
	if line == 0 {
		t.Error("getCallerIgnoringLogMulti(0) returned line 0")
	}
}
