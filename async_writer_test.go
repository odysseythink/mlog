package mlog

import (
	"bufio"
	"bytes"
	"os"
	"testing"
	"time"
)

func TestBatchWriterBasic(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	entries := []*logEntry{
		{data: []byte("line1\n")},
		{data: []byte("line2\n")},
	}
	entries[0].refCnt.Store(1)
	entries[1].refCnt.Store(1)

	if err := bw.writeBatch(entries, 2); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}
	if err := bw.flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	want := "line1\nline2\n"
	if string(content) != want {
		t.Fatalf("file content = %q, want %q", content, want)
	}
}

func TestBatchWriterStructuredEntry(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-structured-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Ensure text encoder is active.
	SetEncoder(NewTextEncoder())

	entry := entryPool.Get().(*Entry)
	entry.Severity = Severity_Info
	entry.Time = time.Now().UnixNano()
	entry.Message = "structured message"
	entry.File = "test.go"
	entry.Line = 42
	entry.Funcname = "TestFunc"

	le := logEntryPool.Get().(*logEntry)
	le.entry = entry
	le.refCnt.Store(1)

	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}
	if err := bw.flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("structured message")) {
		t.Fatalf("file content missing structured message: %q", content)
	}
}

func TestBatchWriterEmptyData(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-empty-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	b := entryBufPool.Get().(*[]byte)
	*b = (*b)[:0]
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(1)

	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}
	if err := bw.flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("expected empty file, got %d bytes", len(content))
	}
}

func TestBatchWriterRefCount(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-refcnt-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], []byte("refcount test\n")...)
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(2)

	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}

	// Entry should NOT be recycled yet because refCnt was 2.
	if le.data == nil {
		t.Fatal("entry.data was nil after first writeBatch, expected still present")
	}
	if le.refCnt.Load() != 1 {
		t.Fatalf("entry.refCnt = %d, want 1", le.refCnt.Load())
	}

	// Second writeBatch decrements refCnt to 0 and recycles.
	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}
	if le.data != nil {
		t.Fatal("entry.data was not nil after second writeBatch")
	}
}

func TestBatchWriterHighWaterMark(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-hwm-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Write enough data to exceed bufioHighWaterMark (192KB).
	dataSize := 200 * 1024
	largeData := make([]byte, dataSize)
	for i := range largeData {
		largeData[i] = 'x'
	}
	largeData[dataSize-1] = '\n'

	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], largeData...)
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(1)

	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}

	// Auto-flush should have written data without an explicit flush().
	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != dataSize {
		t.Fatalf("file content len = %d, want %d", len(content), dataSize)
	}
}

func TestBatchWriterAckDelayedByRefCount(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-ackdelay-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Error}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Error, rb, sb, 8)

	ack := make(chan struct{})
	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], []byte("ack delay\n")...)
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(2)
	le.ack = ack

	// First writeBatch: refCnt 2 -> 1, ack should NOT be signaled.
	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}

	select {
	case <-ack:
		t.Fatal("ack signaled after first writeBatch, expected delayed")
	default:
	}

	// Second writeBatch: refCnt 1 -> 0, ack added to pendingAck.
	if err := bw.writeBatch([]*logEntry{le}, 1); err != nil {
		t.Fatalf("writeBatch failed: %v", err)
	}

	// Ack is only signaled on flush.
	if err := bw.flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	select {
	case <-ack:
	case <-time.After(time.Second):
		t.Fatal("ack not signaled after second writeBatch + flush")
	}
}

func TestAsyncWriterRoundTrip(t *testing.T) {
	rb := newRingBuffer(64)
	f, err := os.CreateTemp("", "asyncwriter-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)
	aw := newAsyncWriter(bw, 8)
	defer aw.close()

	for i := 0; i < 10; i++ {
		entry := logEntryPool.Get().(*logEntry)
		b := entryBufPool.Get().(*[]byte)
		*b = append((*b)[:0], []byte("test line\n")...)
		entry.data = *b
		entry.refCnt.Store(1)
		rb.tryPush(entry)
	}

	aw.wake()
	time.Sleep(100 * time.Millisecond)

	aw.flush()

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("no data written to file")
	}
}

func TestAsyncWriterGracefulShutdown(t *testing.T) {
	rb := newRingBuffer(64)
	f, err := os.CreateTemp("", "asyncwriter-shutdown-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)
	aw := newAsyncWriter(bw, 8)

	for i := 0; i < 5; i++ {
		entry := logEntryPool.Get().(*logEntry)
		b := entryBufPool.Get().(*[]byte)
		*b = append((*b)[:0], []byte("shutdown test\n")...)
		entry.data = *b
		entry.refCnt.Store(1)
		rb.tryPush(entry)
	}

	aw.wake()
	aw.close()

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(content, []byte("\n"))
	if lines != 5 {
		t.Fatalf("expected 5 lines, got %d", lines)
	}
}

func TestAsyncWriterErrorAck(t *testing.T) {
	rb := newRingBuffer(64)
	f, err := os.CreateTemp("", "asyncwriter-ack-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Error}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	bw := newBatchWriter(Severity_Error, rb, sb, 8)
	aw := newAsyncWriter(bw, 8)
	defer aw.close()

	ack := make(chan struct{})
	entry := logEntryPool.Get().(*logEntry)
	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], []byte("error entry\n")...)
	entry.data = *b
	entry.refCnt.Store(1)
	entry.ack = ack
	rb.tryPush(entry)

	aw.wake()

	select {
	case <-ack:
	case <-time.After(2 * time.Second):
		t.Fatal("ERROR ack timeout")
	}
}

func TestAsyncWriterPeriodicFlush(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-ticker-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Use a short flush interval so the ticker fires quickly.
	aw := &asyncWriter{
		bw:            bw,
		wakeCh:        make(chan struct{}, 1),
		flushReqCh:    make(chan chan error, 1),
		closeCh:       make(chan struct{}),
		doneCh:        make(chan struct{}),
		batchSize:     8,
		flushInterval: 50 * time.Millisecond,
	}
	aw.wg.Add(1)
	go aw.writerLoop()
	defer aw.close()

	entry := logEntryPool.Get().(*logEntry)
	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], []byte("ticker test\n")...)
	entry.data = *b
	entry.refCnt.Store(1)
	rb.tryPush(entry)

	aw.wake()

	// Wait long enough for the periodic ticker to fire.
	time.Sleep(150 * time.Millisecond)

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("ticker test")) {
		t.Fatalf("file content missing ticker test: %q", content)
	}
}

func TestAsyncWriterSpinWaitThenWake(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-spin-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	aw := newAsyncWriter(bw, 8)
	defer aw.close()

	// Allow the writer to spin and enter the blocking select.
	time.Sleep(50 * time.Millisecond)

	// Push entries and wake the sleeping writer.
	entry := logEntryPool.Get().(*logEntry)
	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], []byte("spin wake test\n")...)
	entry.data = *b
	entry.refCnt.Store(1)
	rb.tryPush(entry)

	aw.wake()

	// Wait for the writer to process and flush.
	time.Sleep(50 * time.Millisecond)
	aw.flush()

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("spin wake test")) {
		t.Fatalf("file content missing spin wake test: %q", content)
	}
}

func TestAsyncWriterCloseDuringSpinWait(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-close-spin-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	aw := newAsyncWriter(bw, 8)

	// Push entries without waking so they remain pending when we close.
	for i := 0; i < 5; i++ {
		entry := logEntryPool.Get().(*logEntry)
		b := entryBufPool.Get().(*[]byte)
		*b = append((*b)[:0], []byte("close spin test\n")...)
		entry.data = *b
		entry.refCnt.Store(1)
		rb.tryPush(entry)
	}

	// Let the writer spin and enter the inner select.
	time.Sleep(50 * time.Millisecond)

	// Close should drain pending entries and shut down gracefully.
	aw.close()

	select {
	case <-aw.doneCh:
	case <-time.After(time.Second):
		t.Fatal("doneCh not closed after close")
	}

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(content, []byte("\n"))
	if lines != 5 {
		t.Fatalf("expected 5 lines, got %d", lines)
	}
}

func TestAsyncWriterFlushRetry(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-flushretry-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Manually create and start the asyncWriter so we can pre-fill flushReqCh
	// while the writer is likely still in its spin loop.
	aw := &asyncWriter{
		bw:            bw,
		wakeCh:        make(chan struct{}, 1),
		flushReqCh:    make(chan chan error, 1),
		closeCh:       make(chan struct{}),
		doneCh:        make(chan struct{}),
		batchSize:     8,
		flushInterval: periodicFlushInterval,
	}
	aw.wg.Add(1)
	go aw.writerLoop()
	defer aw.close()

	// Pre-fill flushReqCh before the writer has a chance to enter the inner select.
	dummyCh := make(chan error, 1)
	aw.flushReqCh <- dummyCh

	done := make(chan struct{})
	go func() {
		aw.flush()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flush retry timed out")
	}

	// Ensure the writer eventually processes the dummy request.
	select {
	case <-dummyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dummy flush to be processed")
	}
}

func TestAsyncWriterDoubleClose(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-dblclose-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	aw := newAsyncWriter(bw, 8)

	aw.close()
	aw.close() // should not panic
}

func TestAsyncWriterMainSelectTicker(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-ticker-main-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Use a very short ticker so it fires frequently while the writer is
	// looping through batches in the main select.
	aw := &asyncWriter{
		bw:            bw,
		wakeCh:        make(chan struct{}, 1),
		flushReqCh:    make(chan chan error, 1),
		closeCh:       make(chan struct{}),
		doneCh:        make(chan struct{}),
		batchSize:     8,
		flushInterval: 1 * time.Millisecond,
	}
	aw.wg.Add(1)
	go aw.writerLoop()
	defer aw.close()

	// Push many entries so the writer repeatedly evaluates the main select.
	for i := 0; i < 500; i++ {
		entry := logEntryPool.Get().(*logEntry)
		// Use a fresh allocation to avoid data-race with pool reuse.
		data := []byte("main tick\n")
		entry.data = data
		entry.refCnt.Store(1)
		rb.tryPush(entry)
	}

	aw.wake()

	// Give the writer time to process and for the ticker to fire.
	time.Sleep(100 * time.Millisecond)

	aw.flush()
}

func TestBatchWriterWriteError(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-write-err-*.log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close() // close so writes fail
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Write data larger than bufferSize so bufio writes directly to sb.
	largeData := make([]byte, bufferSize+1)
	for i := range largeData {
		largeData[i] = 'x'
	}
	largeData[len(largeData)-1] = '\n'

	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], largeData...)
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(1)

	err = bw.writeBatch([]*logEntry{le}, 1)
	if err == nil {
		t.Fatal("expected writeBatch to return error for closed file")
	}
}

func TestBatchWriterAutoFlushError(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-autoflush-err-*.log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close() // close so flushes fail
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Fill the inner bufio so that flushing it to the closed file fails.
	sb.Write(make([]byte, bufferSize-1))

	// Write enough data to trigger auto-flush.
	dataSize := bufioHighWaterMark + 1
	largeData := make([]byte, dataSize)
	for i := range largeData {
		largeData[i] = 'y'
	}
	largeData[dataSize-1] = '\n'

	b := entryBufPool.Get().(*[]byte)
	*b = append((*b)[:0], largeData...)
	le := logEntryPool.Get().(*logEntry)
	le.data = *b
	le.refCnt.Store(1)

	err = bw.writeBatch([]*logEntry{le}, 1)
	if err == nil {
		t.Fatal("expected writeBatch to return error on auto-flush")
	}
}

func TestBatchWriterFlushBufErrors(t *testing.T) {
	f, err := os.CreateTemp("", "batchwriter-flushbuf-err-*.log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	// Test bw.buf.Flush() error path by filling inner bufio.
	sb.Write(make([]byte, bufferSize-1))
	bw.buf.Write([]byte("extra\n"))
	err = bw.flushBuf()
	if err == nil {
		t.Fatal("expected flushBuf error from buf.Flush")
	}

	// Test bw.sink.Flush() error path with empty outer bufio but data in inner bufio.
	f2, _ := os.CreateTemp("", "batchwriter-flushbuf-err2-*.log")
	defer os.Remove(f2.Name())

	sb2 := &syncBuffer{file: f2, sev: Severity_Info}
	sb2.Writer = bufio.NewWriterSize(f2, bufferSize)
	rb2 := newRingBuffer(64)
	bw2 := newBatchWriter(Severity_Info, rb2, sb2, 8)

	bw2.buf.Write([]byte("hello\n"))
	// bw2.buf.Flush() will succeed, then sb2.Flush() will fail because f2 is closed.
	f2.Close()
	err = bw2.flushBuf()
	if err == nil {
		t.Fatal("expected flushBuf error from sink.Flush")
	}
}

func TestAsyncWriterCloseDrainsPending(t *testing.T) {
	f, err := os.CreateTemp("", "asyncwriter-close-drain-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	sb := &syncBuffer{file: f, sev: Severity_Info}
	sb.Writer = bufio.NewWriterSize(f, bufferSize)
	rb := newRingBuffer(64)
	bw := newBatchWriter(Severity_Info, rb, sb, 8)

	aw := newAsyncWriter(bw, 8)

	for i := 0; i < 5; i++ {
		entry := logEntryPool.Get().(*logEntry)
		b := entryBufPool.Get().(*[]byte)
		*b = append((*b)[:0], []byte("drain test\n")...)
		entry.data = *b
		entry.refCnt.Store(1)
		rb.tryPush(entry)
	}

	// Close immediately without waking. The writer should drain pending
	// entries during the close case in writerLoop.
	aw.close()

	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(content, []byte("\n"))
	if lines != 5 {
		t.Fatalf("expected 5 lines, got %d", lines)
	}
}
