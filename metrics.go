package mlog

import "sync/atomic"

type RingStats struct {
	Size         uint64
	Used         uint64
	DroppedTotal uint64
}

type WriterStatsSnapshot struct {
	Written uint64
	Flushed uint64
	Dropped uint64
}

var ringDroppedWarn atomic.Bool
