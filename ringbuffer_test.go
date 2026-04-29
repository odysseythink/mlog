package mlog

import (
	"runtime"
	"sync"
	"testing"
)

func TestRingBufferBasicWriteRead(t *testing.T) {
	rb := newRingBuffer(16)
	entry := &logEntry{data: []byte("hello")}

	if !rb.tryPush(entry) {
		t.Fatal("tryPush failed on empty buffer")
	}

	var batch [4]*logEntry
	n := rb.drainBatch(batch[:], 4)
	if n != 1 {
		t.Fatalf("drainBatch returned %d, want 1", n)
	}
	if string(batch[0].data) != "hello" {
		t.Fatalf("read wrong data: %q", batch[0].data)
	}
}

func TestRingBufferDropOnFull(t *testing.T) {
	rb := newRingBuffer(4)
	for i := 0; i < 4; i++ {
		if !rb.tryPush(&logEntry{data: []byte("x")}) {
			t.Fatalf("tryPush %d failed", i)
		}
	}
	if rb.tryPush(&logEntry{data: []byte("drop")}) {
		t.Fatal("tryPush should have failed on full buffer")
	}
}

func TestRingBufferBatchRead(t *testing.T) {
	rb := newRingBuffer(32)
	for i := 0; i < 10; i++ {
		rb.tryPush(&logEntry{data: []byte{byte(i)}})
	}

	var batch [16]*logEntry
	n := rb.drainBatch(batch[:], 16)
	if n != 10 {
		t.Fatalf("drainBatch returned %d, want 10", n)
	}
	for i := 0; i < 10; i++ {
		if batch[i].data[0] != byte(i) {
			t.Fatalf("batch[%d] = %d, want %d", i, batch[i].data[0], i)
		}
	}
}

func TestRingBufferClose(t *testing.T) {
	rb := newRingBuffer(8)
	rb.close()
	if rb.tryPush(&logEntry{}) {
		t.Fatal("tryPush should fail after close")
	}
}

func TestRingBufferPublicationRace(t *testing.T) {
	rb := newRingBuffer(8)

	entryA := &logEntry{data: []byte("A")}
	entryB := &logEntry{data: []byte("B")}

	wp := rb.writePos.Load()
	rb.writePos.Store(wp + 1)

	rb.writePos.Store(wp + 2)
	rb.slots[(wp+1)&rb.mask].entry = entryB
	rb.slots[(wp+1)&rb.mask].seq.Store(wp + 2)

	var batch [4]*logEntry
	n := rb.drainBatch(batch[:], 4)
	if n != 0 {
		t.Fatalf("consumer should see 0 entries, got %d (publication race violated)", n)
	}

	rb.slots[wp&rb.mask].entry = entryA
	rb.slots[wp&rb.mask].seq.Store(wp + 1)

	n = rb.drainBatch(batch[:], 4)
	if n != 2 {
		t.Fatalf("expected 2 entries after A publishes, got %d", n)
	}
	if string(batch[0].data) != "A" || string(batch[1].data) != "B" {
		t.Fatalf("wrong order: %s, %s", batch[0].data, batch[1].data)
	}
}

func TestRingBufferConcurrentProducers(t *testing.T) {
	rb := newRingBuffer(1024)
	const numProducers = 16
	const entriesPerProducer = 1000
	totalExpected := numProducers * entriesPerProducer

	// Drain goroutine: consumes entries so producers don't get stuck.
	drained := make(chan int, 1)
	go func() {
		var batch [64]*logEntry
		total := 0
		for total < totalExpected {
			n := rb.drainBatch(batch[:], 64)
			if n == 0 {
				runtime.Gosched()
				continue
			}
			total += n
		}
		drained <- total
	}()

	var wg sync.WaitGroup
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerProducer; j++ {
				for {
					if rb.tryPush(&logEntry{data: []byte("x")}) {
						break
					}
					runtime.Gosched()
				}
			}
		}(i)
	}
	wg.Wait()

	total := <-drained
	if total != totalExpected {
		t.Fatalf("drained %d entries, expected %d", total, totalExpected)
	}
}
