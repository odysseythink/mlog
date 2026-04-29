package mlog

import (
	"sync"
)

// Encoder serializes an Entry into a byte slice.
type Encoder interface {
	EncodeEntry(entry *Entry) []byte
}

// encoderHolder wraps the active Encoder with a mutex for thread-safe swapping.
type encoderHolder struct {
	mu      sync.RWMutex
	encoder Encoder
}

var activeEncoder = &encoderHolder{}

var encoderOnce sync.Once

func getEncoder() Encoder {
	encoderOnce.Do(func() {
		switch *logEncoderFlag {
		case "json":
			activeEncoder.encoder = &jsonEncoder{}
		case "logfmt":
			activeEncoder.encoder = &logfmtEncoder{}
		default:
			activeEncoder.encoder = defaultTextEncoder
		}
	})
	activeEncoder.mu.RLock()
	enc := activeEncoder.encoder
	activeEncoder.mu.RUnlock()
	if enc != nil {
		return enc
	}
	return defaultTextEncoder
}

// SetEncoder replaces the active encoder used by the logging pipeline.
func SetEncoder(enc Encoder) {
	encoderOnce.Do(func() {
		activeEncoder.encoder = defaultTextEncoder
	})
	activeEncoder.mu.Lock()
	activeEncoder.encoder = enc
	activeEncoder.mu.Unlock()
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
