package controller

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"qms/internal/order"
)

// TestLoadConcurrentOrderSubmission verifies that concurrent order creation
// produces unique, strictly increasing IDs and that all orders are queued.
func TestLoadConcurrentOrderSubmission(t *testing.T) {
	const numOrders = 500
	c := New(WithProcessDuration(time.Hour)) // no bots will be added; orders just queue

	var wg sync.WaitGroup
	ids := make([]int32, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			t := order.Normal
			if idx%3 == 0 {
				t = order.VIP
			}
			o := c.NewOrder(t)
			atomic.StoreInt32(&ids[idx], int32(o.ID))
		}(i)
	}
	wg.Wait()

	// Verify uniqueness
	seen := make(map[int32]bool, numOrders)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate order ID %d", id)
		}
		seen[id] = true
	}

	if got := c.PendingCount(); got != numOrders {
		t.Errorf("PendingCount: want %d, got %d", numOrders, got)
	}
}

// TestLoad_AllOrdersEventuallyComplete verifies that with N bots and M orders,
// every order reaches COMPLETE and none are lost.
func TestLoad_AllOrdersEventuallyComplete(t *testing.T) {
	const (
		numOrders = 100
		numBots   = 5
	)
	c := New(WithProcessDuration(5 * time.Millisecond))

	for i := 0; i < numBots; i++ {
		c.AddBot()
	}
	for i := 0; i < numOrders; i++ {
		if i%4 == 0 {
			c.NewOrder(order.VIP)
		} else {
			c.NewOrder(order.Normal)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.CompletedCount() == numOrders {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := c.CompletedCount(); got != numOrders {
		t.Errorf("CompletedCount: want %d, got %d (pending=%d)", numOrders, got, c.PendingCount())
	}
}

// TestLoadConcurrentBotChurn adds and removes bots rapidly while orders flow in
// and verifies the system reaches a stable state (no lost orders).
func TestLoadConcurrentBotChurn(t *testing.T) {
	const numOrders = 50
	c := New(WithProcessDuration(10 * time.Millisecond))

	// Seed initial bots
	for i := 0; i < 3; i++ {
		c.AddBot()
	}

	// Submit all orders
	for i := 0; i < numOrders; i++ {
		c.NewOrder(order.Normal)
	}

	// Churn bots while orders are being processed
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			time.Sleep(5 * time.Millisecond)
			c.AddBot()
			time.Sleep(5 * time.Millisecond)
			c.RemoveBot()
		}
	}()
	<-done

	// Add final bots to drain any remaining orders
	for i := 0; i < 3; i++ {
		c.AddBot()
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		pending := c.PendingCount()
		processing := 0
		for _, b := range c.State().Bots {
			if b.CurrentOrder != nil {
				processing++
			}
		}
		if pending == 0 && processing == 0 && c.CompletedCount()+pending == numOrders {
			break
		}
		if c.CompletedCount() == numOrders {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	total := c.CompletedCount() + c.PendingCount()
	if total != numOrders {
		t.Errorf("order count mismatch: completed=%d pending=%d total=%d want=%d",
			c.CompletedCount(), c.PendingCount(), total, numOrders)
	}
}

// TestLoadHighConcurrencyMixedOrders stress-tests queue ordering under load.
// VIP orders must always precede Normal orders in the pending queue snapshot.
func TestLoadHighConcurrencyMixedOrders(t *testing.T) {
	const numOrders = 200
	c := New(WithProcessDuration(time.Hour)) // freeze processing; inspect queue only

	var wg sync.WaitGroup
	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				c.NewOrder(order.VIP)
			} else {
				c.NewOrder(order.Normal)
			}
		}(i)
	}
	wg.Wait()

	snap := c.State().Pending
	if len(snap) != numOrders {
		t.Fatalf("pending count: want %d, got %d", numOrders, len(snap))
	}

	// All VIPs must appear before any Normal
	seenNormal := false
	for _, o := range snap {
		if o.Type == order.Normal {
			seenNormal = true
		}
		if seenNormal && o.Type == order.VIP {
			t.Errorf("VIP order #%d found after a Normal order – priority violated", o.ID)
			break
		}
	}
}

// TestLoadConcurrentBotAddRemove verifies that add/remove bot operations are
// safe under concurrent access and leave BotCount consistent.
func TestLoadConcurrentBotAddRemove(t *testing.T) {
	c := New(WithProcessDuration(time.Hour))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.AddBot()
		}()
	}
	wg.Wait()

	if got := c.BotCount(); got != 50 {
		t.Fatalf("BotCount after 50 adds: want 50, got %d", got)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RemoveBot()
		}()
	}
	wg.Wait()

	if got := c.BotCount(); got != 0 {
		t.Errorf("BotCount after 50 removes: want 0, got %d", got)
	}
}

// TestLoadManyBotsOneOrder checks that exactly one bot picks up a single order.
func TestLoadManyBotsOneOrder(t *testing.T) {
	c := New(WithProcessDuration(50 * time.Millisecond))

	for i := 0; i < 20; i++ {
		c.AddBot()
	}
	time.Sleep(20 * time.Millisecond) // let all bots settle idle

	c.NewOrder(order.Normal)
	time.Sleep(200 * time.Millisecond)

	if got := c.CompletedCount(); got != 1 {
		t.Errorf("exactly one order should complete; CompletedCount=%d", got)
	}
	if got := c.PendingCount(); got != 0 {
		t.Errorf("no order should remain pending; PendingCount=%d", got)
	}
}

// TestLoadOrdersCompletedInPriorityOrder verifies end-to-end that VIP orders
// are completed before Normal orders when using a single sequential bot.
func TestLoadOrdersCompletedInPriorityOrder(t *testing.T) {
	const processDur = 20 * time.Millisecond
	c := New(WithProcessDuration(processDur))

	// Fill queue with no bot so ordering is deterministic
	for i := 0; i < 5; i++ {
		c.NewOrder(order.Normal)
	}
	for i := 0; i < 5; i++ {
		c.NewOrder(order.VIP)
	}

	c.AddBot()

	// Wait for all to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.CompletedCount() < 10 {
		time.Sleep(5 * time.Millisecond)
	}

	completed := c.State().Completed
	if len(completed) != 10 {
		t.Fatalf("want 10 completed, got %d", len(completed))
	}

	// First 5 must be VIP
	for i := 0; i < 5; i++ {
		if completed[i].Type != order.VIP {
			t.Errorf("completed[%d] should be VIP, got %s (ID=%d)",
				i, completed[i].Type, completed[i].ID)
		}
	}
	// Last 5 must be Normal
	for i := 5; i < 10; i++ {
		if completed[i].Type != order.Normal {
			t.Errorf("completed[%d] should be Normal, got %s (ID=%d)",
				i, completed[i].Type, completed[i].ID)
		}
	}
}
