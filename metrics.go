package mlog

import "sync/atomic"

type ringStats struct {
	size         uint64
	used         uint64
	droppedTotal uint64
}

type writerStatsSnapshot struct {
	written uint64
	flushed uint64
	dropped uint64
}

var ringDroppedWarn atomic.Bool
