package mlog

import (
	"sync"
	"sync/atomic"
)

// Encoder serializes an Entry into a byte slice.
type Encoder interface {
	EncodeEntry(entry *Entry) []byte
}

// activeEncoder holds the currently configured Encoder (thread-safe).
var activeEncoder atomic.Value // stores Encoder

// getEncoder returns the active encoder, falling back to the default text encoder.
func getEncoder() Encoder {
	if v := activeEncoder.Load(); v != nil {
		return v.(Encoder)
	}
	return defaultTextEncoder
}

// SetEncoder replaces the active encoder used by the logging pipeline.
func SetEncoder(enc Encoder) {
	activeEncoder.Store(enc)
}

// encBufPool reuses []byte buffers for encoder output.
var encBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, defaultEntryBufSize)
		return &b
	},
}

func getEncBuf() *[]byte {
	return encBufPool.Get().(*[]byte)
}

func putEncBuf(p *[]byte) {
	if cap(*p) > maxPooledEntryBuf {
		return
	}
	*p = (*p)[:0]
	encBufPool.Put(p)
}

var defaultTextEncoder = &textEncoder{}
