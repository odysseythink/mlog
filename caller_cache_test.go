package mlog

import (
	"strings"
	"testing"
)

func TestTrimSrcPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/a/b/c.go", "c.go"},
		{"c.go", "c.go"},
		{"", ""},
	}
	for _, tc := range tests {
		got := trimSrcPath(tc.input)
		if got != tc.want {
			t.Errorf("trimSrcPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTrimFuncName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/odysseythink/mlog.TestFunc", "TestFunc"},
		{"pkg.TestFunc", "TestFunc"},
		{"TestFunc", "TestFunc"},
		{"", ""},
	}
	for _, tc := range tests {
		got := trimFuncName(tc.input)
		if got != tc.want {
			t.Errorf("trimFuncName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGetCallerInfo(t *testing.T) {
	// skip=1 means the caller of getCallerInfo (this test function).
	file, line, funcname := getCallerInfo(1)
	if file == "" || file == "???" {
		t.Errorf("getCallerInfo(1) returned empty/unknown file: %q", file)
	}
	if line == 0 {
		t.Error("getCallerInfo(1) returned line 0")
	}
	if funcname == "" || funcname == "???" {
		t.Errorf("getCallerInfo(1) returned empty/unknown funcname: %q", funcname)
	}
	if !strings.Contains(funcname, "TestGetCallerInfo") {
		t.Errorf("getCallerInfo(1) funcname = %q, want to contain TestGetCallerInfo", funcname)
	}

	// Second call should hit cache.
	file2, _, funcname2 := getCallerInfo(1)
	if file2 != file || funcname2 != funcname {
		t.Errorf("cached result differs: got (%q,%q), want (%q,%q)", file2, funcname2, file, funcname)
	}
}

func TestGetCallerInfoDeepStack(t *testing.T) {
	callGetCallerInfo := func() (string, int, string) {
		return getCallerInfo(2)
	}
	file, line, funcname := callGetCallerInfo()
	if file == "" || file == "???" {
		t.Errorf("getCallerInfo(2) returned empty/unknown file: %q", file)
	}
	if !strings.Contains(funcname, "TestGetCallerInfoDeepStack") {
		t.Errorf("getCallerInfo(2) funcname = %q, want to contain TestGetCallerInfoDeepStack", funcname)
	}
	_ = line
}
