package mlog

import (
	"runtime"
	"sync/atomic"
)

type slot struct {
	seq   atomic.Uint64
	entry *logEntry
}

type ringBuffer struct {
	_pad0    [64]byte
	writePos atomic.Uint64
	_pad1    [56]byte
	readPos  atomic.Uint64
	_pad2    [56]byte

	slots   []slot
	mask    uint64
	cap     uint64
	dropped atomic.Uint64
	closed  atomic.Bool
}

func newRingBuffer(capacity int) *ringBuffer {
	cap64 := uint64(1)
	for cap64 < uint64(capacity) {
		cap64 <<= 1
	}
	rb := &ringBuffer{
		slots: make([]slot, cap64),
		cap:   cap64,
		mask:  cap64 - 1,
	}
	return rb
}

func (rb *ringBuffer) tryPush(entry *logEntry) bool {
	for {
		if rb.closed.Load() {
			return false
		}
		wp := rb.writePos.Load()
		rp := rb.readPos.Load()
		if wp-rp >= rb.cap {
			return false
		}
		if rb.writePos.CompareAndSwap(wp, wp+1) {
			s := &rb.slots[wp&rb.mask]
			s.entry = entry
			s.seq.Store(wp + 1)
			return true
		}
	}
}

func (rb *ringBuffer) drainBatch(out []*logEntry, maxBatch int) int {
	rp := rb.readPos.Load()
	n := 0
	for n < maxBatch {
		s := &rb.slots[rp&rb.mask]
		seq := s.seq.Load()
		expected := rp + 1
		if seq != expected {
			if seq < expected {
				if !spinWaitSeq(&s.seq, expected, 64) {
					break
				}
			} else {
				break
			}
		}
		out[n] = s.entry
		s.entry = nil
		n++
		rp++
	}
	if n > 0 {
		rb.readPos.Store(rp)
	}
	return n
}

func spinWaitSeq(seq *atomic.Uint64, expected uint64, maxIter int) bool {
	for i := 0; i < maxIter; i++ {
		if seq.Load() == expected {
			return true
		}
		runtime.Gosched()
	}
	return false
}

func (rb *ringBuffer) len() int {
	return int(rb.writePos.Load() - rb.readPos.Load())
}

func (rb *ringBuffer) close() {
	rb.closed.Store(true)
}
