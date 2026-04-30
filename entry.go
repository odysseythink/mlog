package mlog

import "sync"

type Entry struct {
	Severity Severity
	Time     int64 // Unix nanoseconds
	Message  string
	Fields   []Field // nil = no structured fields (old API path)
	File     string
	Line     int
	Funcname string
	Thread   int64
	Stack    *Stack
}

var entryPool = sync.Pool{
	New: func() any {
		return &Entry{
			Fields: make([]Field, 0, 16),
		}
	},
}

func getEntry() *Entry {
	e := entryPool.Get().(*Entry)
	return e
}

func putEntry(e *Entry) {
	e.Severity = 0
	e.Time = 0
	e.Message = ""
	for i := range e.Fields {
		e.Fields[i] = Field{}
	}
	e.Fields = e.Fields[:0]
	e.File = ""
	e.Line = 0
	e.Funcname = ""
	e.Thread = 0
	e.Stack = nil
	entryPool.Put(e)
}
