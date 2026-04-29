package mlog

import (
	"runtime"
	"strings"
	"sync"
)

// callerInfo holds cached file, function name, and line for a given PC.
type callerInfo struct {
	file     string
	funcname string
	line     int
}

// callerCache maps PC (program counter) to *callerInfo.
var callerCache sync.Map

// trimSrcPath strips path prefix from a source file path, keeping only the basename.
func trimSrcPath(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// trimFuncName strips package path from a fully qualified function name.
func trimFuncName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// getCallerInfo returns cached caller info for the PC at the given skip depth.
// skip=0 means the caller of getCallerInfo itself.
func getCallerInfo(skip int) (file string, line int, funcname string) {
	var pcs [1]uintptr
	n := runtime.Callers(skip+1, pcs[:])
	if n == 0 {
		return "???", 0, "???"
	}
	pc := pcs[0]
	if v, ok := callerCache.Load(pc); ok {
		ci := v.(*callerInfo)
		return ci.file, ci.line, ci.funcname
	}
	// Cold path: resolve via CallersFrames, cache result.
	frames := runtime.CallersFrames(pcs[:n])
	fr, _ := frames.Next()
	ci := &callerInfo{
		file:     trimSrcPath(fr.File),
		line:     fr.Line,
		funcname: trimFuncName(fr.Function),
	}
	callerCache.Store(pc, ci)
	return ci.file, ci.line, ci.funcname
}
