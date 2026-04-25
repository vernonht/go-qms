package queue

import (
	"sync"
	"testing"

	"qms/internal/order"
)

// helpers

func newNormal(id int) *order.Order { return order.New(id, order.Normal) }
func newVIP(id int) *order.Order    { return order.New(id, order.VIP) }

// --- Enqueue ---

func TestEnqueueNormalOrdersFIFO(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newNormal(1))
	q.Enqueue(newNormal(2))
	q.Enqueue(newNormal(3))

	for _, want := range []int{1, 2, 3} {
		got := q.Dequeue()
		if got == nil || got.ID != want {
			t.Fatalf("Dequeue: want ID %d, got %v", want, got)
		}
	}
}

func TestEnqueueVIPJumpsBehindExistingVIPsButBeforeNormals(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newNormal(1))
	q.Enqueue(newVIP(2))
	q.Enqueue(newNormal(3))
	q.Enqueue(newVIP(4)) // should be after VIP #2, before Normal #1 and #3

	snap := q.Snapshot()
	wantIDs := []int{2, 4, 1, 3}
	if len(snap) != len(wantIDs) {
		t.Fatalf("len: want %d, got %d", len(wantIDs), len(snap))
	}
	for i, want := range wantIDs {
		if snap[i].ID != want {
			t.Errorf("position %d: want #%d, got #%d", i, want, snap[i].ID)
		}
	}
}

func TestEnqueueVIPWithNoExistingVIPGoesToFront(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newNormal(1))
	q.Enqueue(newNormal(2))
	q.Enqueue(newVIP(3))

	snap := q.Snapshot()
	if snap[0].ID != 3 || snap[0].Type != order.VIP {
		t.Errorf("VIP should be first; got #%d (%s)", snap[0].ID, snap[0].Type)
	}
}

func TestEnqueueMultipleVIPsFIFOAmongThemselves(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newVIP(1))
	q.Enqueue(newVIP(2))
	q.Enqueue(newVIP(3))

	for _, want := range []int{1, 2, 3} {
		got := q.Dequeue()
		if got.ID != want {
			t.Fatalf("VIP FIFO: want #%d, got #%d", want, got.ID)
		}
	}
}

// --- Dequeue ---

func TestDequeueEmptyQueueReturnsNil(t *testing.T) {
	q := &Queue{}
	if got := q.Dequeue(); got != nil {
		t.Errorf("empty Dequeue: want nil, got %v", got)
	}
}

func TestDequeueReducesLen(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newNormal(1))
	q.Enqueue(newNormal(2))

	q.Dequeue()
	if q.Len() != 1 {
		t.Errorf("Len after Dequeue: want 1, got %d", q.Len())
	}
}

// --- EnqueueFront ---

func TestEnqueueFrontVIPGoesToPositionZero(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newVIP(1))
	q.Enqueue(newVIP(2))
	q.Enqueue(newNormal(3))

	q.EnqueueFront(newVIP(99))

	snap := q.Snapshot()
	if snap[0].ID != 99 {
		t.Errorf("returned VIP should be at position 0, got #%d", snap[0].ID)
	}
}

func TestEnqueueFrontNormalGoesAfterAllVIPs(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newVIP(1))
	q.Enqueue(newNormal(2))
	q.Enqueue(newNormal(3))

	q.EnqueueFront(newNormal(99))

	snap := q.Snapshot()
	// Expected: [VIP#1, Normal#99, Normal#2, Normal#3]
	if snap[1].ID != 99 {
		t.Errorf("returned Normal should be at position 1 (after VIPs), got #%d", snap[1].ID)
	}
}

func TestEnqueueFrontNormalNoVIPsGoesToFront(t *testing.T) {
	q := &Queue{}
	q.Enqueue(newNormal(1))
	q.Enqueue(newNormal(2))

	q.EnqueueFront(newNormal(99))

	snap := q.Snapshot()
	if snap[0].ID != 99 {
		t.Errorf("returned Normal should be at position 0 when no VIPs, got #%d", snap[0].ID)
	}
}

// --- Concurrent safety ---

func TestQueueConcurrentEnqueueDequeueNoDataRace(t *testing.T) {
	q := &Queue{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				q.Enqueue(newNormal(id))
			} else {
				q.Enqueue(newVIP(id))
			}
		}(i)
	}

	// Dequeue concurrently while enqueueing
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Dequeue() // may return nil, that's fine
		}()
	}

	wg.Wait()
}
