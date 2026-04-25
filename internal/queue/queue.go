package queue

import (
	"sync"

	"qms/internal/order"
)

// Queue is a thread-safe priority queue. VIP orders precede Normal orders;
// within each group orders are served FIFO.
type Queue struct {
	mu     sync.Mutex
	orders []*order.Order
}

// Enqueue inserts o in its priority position:
//   - VIP  → after the last existing VIP, before any Normal
//   - Normal → at the tail
func (q *Queue) Enqueue(o *order.Order) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if o.Type == order.VIP {
		q.insert(o, q.lastVIPIdx()+1)
	} else {
		q.orders = append(q.orders, o)
	}
}

// EnqueueFront inserts a returned order at the head of its priority group:
//   - VIP    → position 0 (before all other VIPs)
//   - Normal → right after all VIPs (before other Normals)
//
// This is used when a bot is removed mid-processing so the order keeps
// its relative priority without being pushed to the back.
func (q *Queue) EnqueueFront(o *order.Order) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if o.Type == order.VIP {
		q.insert(o, 0)
	} else {
		q.insert(o, q.lastVIPIdx()+1)
	}
}

// Dequeue removes and returns the highest-priority order, or nil when empty.
func (q *Queue) Dequeue() *order.Order {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.orders) == 0 {
		return nil
	}
	o := q.orders[0]
	q.orders = q.orders[1:]
	return o
}

// Len returns the number of pending orders.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.orders)
}

// Snapshot returns a shallow copy of the current order slice for display.
func (q *Queue) Snapshot() []*order.Order {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*order.Order, len(q.orders))
	copy(out, q.orders)
	return out
}

// lastVIPIdx returns the index of the last VIP entry, or -1 if none.
// Must be called with q.mu held.
func (q *Queue) lastVIPIdx() int {
	last := -1
	for i, o := range q.orders {
		if o.Type == order.VIP {
			last = i
		}
	}
	return last
}

// insert places o at position idx, shifting existing entries right.
// Must be called with q.mu held.
func (q *Queue) insert(o *order.Order, idx int) {
	q.orders = append(q.orders, nil)
	copy(q.orders[idx+1:], q.orders[idx:])
	q.orders[idx] = o
}
