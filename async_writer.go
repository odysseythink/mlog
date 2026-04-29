package mlog

import (
	"bufio"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type batchWriter struct {
	severity  Severity
	ring      *ringBuffer
	sink      *syncBuffer
	buf       *bufio.Writer
	batch     []*logEntry
	stats     *writerStats
	pendingAck []chan struct{}
}

type writerStats struct {
	written     atomic.Uint64
	flushed     atomic.Uint64
	dropped     atomic.Uint64
	blockWaitNs atomic.Uint64
}

func newBatchWriter(sev Severity, rb *ringBuffer, sb *syncBuffer, batchSize int) *batchWriter {
	return &batchWriter{
		severity: sev,
		ring:     rb,
		sink:     sb,
		buf:      bufio.NewWriterSize(sb, bufferSize),
		batch:    make([]*logEntry, batchSize),
		stats:    &writerStats{},
	}
}

func (bw *batchWriter) writeBatch(entries []*logEntry, n int) error {
	for i := 0; i < n; i++ {
		entry := entries[i]
		if _, err := bw.buf.Write(entry.data); err != nil {
			return err
		}
		if entry.refCnt.Add(-1) == 0 {
			putEntryBuf(&entry.data)
			if entry.ack != nil {
				bw.pendingAck = append(bw.pendingAck, entry.ack)
			}
			logEntryPool.Put(entry)
		}
		bw.stats.written.Add(1)
	}
	if bw.buf.Buffered() >= bufioHighWaterMark {
		if err := bw.flushBuf(); err != nil {
			return err
		}
	}
	return nil
}

// flushBuf flushes the bufio layers, signals pending acks, and syncs the file.
func (bw *batchWriter) flushBuf() error {
	if err := bw.buf.Flush(); err != nil {
		return err
	}
	if err := bw.sink.Flush(); err != nil {
		return err
	}
	bw.signalAcks()
	bw.stats.flushed.Add(1)
	return bw.sink.Sync()
}

// signalAcks closes all pending ack channels, unblocking callers waiting on ERROR ack.
func (bw *batchWriter) signalAcks() {
	for _, ack := range bw.pendingAck {
		close(ack)
	}
	bw.pendingAck = bw.pendingAck[:0]
}

func (bw *batchWriter) flush() error {
	return bw.flushBuf()
}

type asyncWriter struct {
	bw            *batchWriter
	wakeCh        chan struct{}
	flushReqCh    chan chan error
	closeCh       chan struct{}
	doneCh        chan struct{}
	closed        atomic.Bool
	wg            sync.WaitGroup
	batchSize     int
	flushInterval time.Duration
}

func newAsyncWriter(bw *batchWriter, batchSize int) *asyncWriter {
	aw := &asyncWriter{
		bw:            bw,
		wakeCh:        make(chan struct{}, 1),
		flushReqCh:    make(chan chan error, 1),
		closeCh:       make(chan struct{}),
		doneCh:        make(chan struct{}),
		batchSize:     batchSize,
		flushInterval: periodicFlushInterval,
	}
	aw.wg.Add(1)
	go aw.writerLoop()
	return aw
}

func (aw *asyncWriter) writerLoop() {
	defer aw.wg.Done()
	defer close(aw.doneCh)

	ticker := time.NewTicker(aw.flushInterval)
	defer ticker.Stop()

	needFlush := false
	spinCount := 0

	for {
		select {
		case <-aw.closeCh:
			for {
				n := aw.bw.ring.drainBatch(aw.bw.batch, aw.batchSize)
				if n == 0 {
					break
				}
				aw.bw.writeBatch(aw.bw.batch, n)
			}
			aw.bw.flush()
			return

		case respCh := <-aw.flushReqCh:
			err := aw.bw.flush()
			if respCh != nil {
				respCh <- err
			}
			needFlush = false

		case <-ticker.C:
			if needFlush {
				aw.bw.flush()
				needFlush = false
			}

		default:
			n := aw.bw.ring.drainBatch(aw.bw.batch, aw.batchSize)
			if n > 0 {
				aw.bw.writeBatch(aw.bw.batch, n)
				if len(aw.bw.pendingAck) > 0 {
					aw.bw.flush()
					needFlush = false
				} else {
					needFlush = true
				}
				spinCount = 0
			} else {
				if spinCount < 256 {
					spinCount++
					runtime.Gosched()
					continue
				}
				spinCount = 0
				select {
				case <-aw.wakeCh:
				case respCh := <-aw.flushReqCh:
					err := aw.bw.flush()
					if respCh != nil {
						respCh <- err
					}
					needFlush = false
				case <-ticker.C:
					if needFlush {
						aw.bw.flush()
						needFlush = false
					}
				case <-aw.closeCh:
				}
			}
		}
	}
}

func (aw *asyncWriter) wake() {
	select {
	case aw.wakeCh <- struct{}{}:
	default:
	}
}

// flush requests an immediate flush and waits for completion.
func (aw *asyncWriter) flush() error {
	respCh := make(chan error, 1)
	select {
	case aw.flushReqCh <- respCh:
		return <-respCh
	default:
		// A flush is already pending. Wait for the writer to drain its queue
		// by sending a second request via wake which triggers another drain cycle.
		aw.wake()
		// Retry sending the flush request.
		select {
		case aw.flushReqCh <- respCh:
			return <-respCh
		default:
			return nil
		}
	}
}

func (aw *asyncWriter) close() {
	if aw.closed.CompareAndSwap(false, true) {
		aw.bw.ring.close()
		close(aw.closeCh)
		aw.wg.Wait()
	}
}
