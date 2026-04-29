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
