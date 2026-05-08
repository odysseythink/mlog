package mlog

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestModulePatMatch(t *testing.T) {
	tests := []struct {
		name string
		pat  modulePat
		full string
		file string
		want bool
	}{
		{"literal full match", modulePat{pattern: "foo/bar", literal: true, full: true}, "foo/bar", "bar", true},
		{"literal full no match", modulePat{pattern: "foo/bar", literal: true, full: true}, "foo/baz", "baz", false},
		{"literal file match", modulePat{pattern: "bar", literal: true, full: false}, "foo/bar", "bar", true},
		{"literal file no match", modulePat{pattern: "bar", literal: true, full: false}, "foo/bar", "baz", false},
		{"wildcard full match", modulePat{pattern: "foo/*", literal: false, full: true}, "foo/bar", "bar", true},
		{"wildcard full no match", modulePat{pattern: "foo/*", literal: false, full: true}, "baz/bar", "bar", false},
		{"wildcard file match", modulePat{pattern: "b*r", literal: false, full: false}, "foo/bar", "bar", true},
		{"wildcard file no match", modulePat{pattern: "b*r", literal: false, full: false}, "foo/bar", "baz", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pat.match(tc.full, tc.file)
			if got != tc.want {
				t.Errorf("match(%q, %q) = %v, want %v", tc.full, tc.file, got, tc.want)
			}
		})
	}
}

func TestIsLiteral(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"foo", true},
		{"foo.go", true},
		{"foo*", false},
		{"foo?", false},
		{"foo[ab]", false},
		{`foo\?`, false},
	}
	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			got := isLiteral(tc.pattern)
			if got != tc.want {
				t.Errorf("isLiteral(%q) = %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestIsFull(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"foo", false},
		{"foo/bar", true},
		{"/foo", true},
	}
	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			got := isFull(tc.pattern)
			if got != tc.want {
				t.Errorf("isFull(%q) = %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	l := Level(42)
	if got := l.String(); got != "42" {
		t.Errorf("Level(42).String() = %q, want %q", got, "42")
	}
}

func TestLevelGetSet(t *testing.T) {
	// Test Level that is NOT the -v flag
	var l Level
	if got := l.Get(); got != Level(0) {
		t.Errorf("Get() = %v, want 0", got)
	}
	if err := l.Set("5"); err != nil {
		t.Errorf("Set(5) error: %v", err)
	}
	if got := l.Get(); got != Level(5) {
		t.Errorf("Get() = %v, want 5", got)
	}

	// Invalid set
	if err := l.Set("abc"); err == nil {
		t.Error("Set(abc) expected error")
	}

	// Test Level that IS the -v flag
	origV := vflags.v
	defer func() { vflags.v = origV }()
	vflags.moduleLevelCache.Store(&sync.Map{})
	if err := vflags.v.Set("3"); err != nil {
		t.Errorf("Set(3) error: %v", err)
	}
	if got := vflags.v.Get(); got != Level(3) {
		t.Errorf("Get() = %v, want 3", got)
	}
}

func TestVerboseEnabled(t *testing.T) {
	// Save and restore vflags state
	origV := vflags.v
	origModule := vflags.module
	origModuleLength := vflags.moduleLength
	defer func() {
		vflags.v = origV
		vflags.module = origModule
		atomic.StoreInt32(&vflags.moduleLength, origModuleLength)
		vflags.moduleLevelCache.Store(&sync.Map{})
	}()

	vflags.module = nil
	atomic.StoreInt32(&vflags.moduleLength, 0)

	// v=0: level 0 enabled, level 1 disabled
	atomic.StoreInt32((*int32)(&vflags.v), 0)
	vflags.moduleLevelCache.Store(&sync.Map{})
	if !verboseEnabled(0, 0) {
		t.Error("verboseEnabled(0, 0) should be true when v=0")
	}
	if verboseEnabled(0, 1) {
		t.Error("verboseEnabled(0, 1) should be false when v=0")
	}

	// v=2: levels 0,1,2 enabled
	atomic.StoreInt32((*int32)(&vflags.v), 2)
	vflags.moduleLevelCache.Store(&sync.Map{})
	if !verboseEnabled(0, 0) {
		t.Error("verboseEnabled(0, 0) should be true when v=2")
	}
	if !verboseEnabled(0, 2) {
		t.Error("verboseEnabled(0, 2) should be true when v=2")
	}
	if verboseEnabled(0, 3) {
		t.Error("verboseEnabled(0, 3) should be false when v=2")
	}
}
