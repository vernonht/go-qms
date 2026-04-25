package controller

import (
	"testing"
	"time"

	"qms/internal/order"
)

const testDur = 80 * time.Millisecond

func testCtrl() *Controller { return New(WithProcessDuration(testDur)) }

// --- Order creation ---

func TestNewOrderUniqueIncreasingIDs(t *testing.T) {
	c := testCtrl()
	ids := make([]int, 5)
	for i := range ids {
		ids[i] = c.NewOrder(order.Normal).ID
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("IDs not strictly increasing: %v", ids)
		}
	}
}

func TestNewOrderAppearsInPending(t *testing.T) {
	c := testCtrl()
	c.NewOrder(order.Normal)
	if got := c.PendingCount(); got != 1 {
		t.Errorf("PendingCount: want 1, got %d", got)
	}
}

func TestNewOrderInitialStatusIsPending(t *testing.T) {
	c := testCtrl()
	o := c.NewOrder(order.VIP)
	if o.Status != order.Pending {
		t.Errorf("Status: want Pending, got %v", o.Status)
	}
}

// --- VIP priority ---

func TestNewOrderVIPJumpsBeforeNormals(t *testing.T) {
	c := testCtrl()
	c.NewOrder(order.Normal) // #1
	c.NewOrder(order.Normal) // #2
	c.NewOrder(order.VIP)    // #3 – should be first in queue

	state := c.State()
	if len(state.Pending) < 3 {
		t.Fatalf("want 3 pending, got %d", len(state.Pending))
	}
	if state.Pending[0].Type != order.VIP {
		t.Errorf("first pending order should be VIP, got %s", state.Pending[0].Type)
	}
}

func TestNewOrderSecondVIPQueuesBehindFirstVIP(t *testing.T) {
	c := testCtrl()
	c.NewOrder(order.VIP)    // #1
	c.NewOrder(order.Normal) // #2
	c.NewOrder(order.VIP)    // #3 – after #1, before #2

	snap := c.State().Pending
	wantOrder := []struct{ id int; t order.Type }{
		{1, order.VIP},
		{3, order.VIP},
		{2, order.Normal},
	}
	for i, w := range wantOrder {
		if snap[i].ID != w.id || snap[i].Type != w.t {
			t.Errorf("position %d: want #%d(%s), got #%d(%s)",
				i, w.id, w.t, snap[i].ID, snap[i].Type)
		}
	}
}

// --- Bot management ---

func TestAddBotIncreasesBotCount(t *testing.T) {
	c := testCtrl()
	c.AddBot()
	c.AddBot()
	if got := c.BotCount(); got != 2 {
		t.Errorf("BotCount: want 2, got %d", got)
	}
}

func TestAddBotProcessesPendingOrder(t *testing.T) {
	c := testCtrl()
	c.NewOrder(order.Normal)
	c.AddBot()

	time.Sleep(testDur * 3)

	if got := c.CompletedCount(); got != 1 {
		t.Errorf("CompletedCount: want 1, got %d", got)
	}
	if got := c.PendingCount(); got != 0 {
		t.Errorf("PendingCount: want 0, got %d", got)
	}
}

func TestAddBotIdleWhenNoPendingOrders(t *testing.T) {
	c := testCtrl()
	c.AddBot()
	time.Sleep(20 * time.Millisecond)

	state := c.State()
	if len(state.Bots) == 0 {
		t.Fatal("expected 1 bot in state")
	}
	if state.Bots[0].CurrentOrder != nil {
		t.Errorf("bot should be idle, but has order #%d", state.Bots[0].CurrentOrder.ID)
	}
}

func TestAddBotIdleBotPicksUpNewOrder(t *testing.T) {
	c := testCtrl()
	c.AddBot()
	time.Sleep(20 * time.Millisecond) // let bot settle into idle

	c.NewOrder(order.Normal)
	time.Sleep(testDur * 3)

	if got := c.CompletedCount(); got != 1 {
		t.Errorf("idle bot should have processed the order; CompletedCount=%d", got)
	}
}

func TestAddBotTwoBotsProcessInParallel(t *testing.T) {
	c := New(WithProcessDuration(200 * time.Millisecond))
	c.AddBot()
	c.AddBot()
	c.NewOrder(order.Normal)
	c.NewOrder(order.Normal)

	// Both orders should complete close to one duration, not two durations
	time.Sleep(300 * time.Millisecond)

	if got := c.CompletedCount(); got != 2 {
		t.Errorf("2 bots should complete 2 orders in parallel; CompletedCount=%d", got)
	}
}

// --- Bot removal ---

func TestRemoveBotDecreasesBotCount(t *testing.T) {
	c := testCtrl()
	c.AddBot()
	c.AddBot()
	c.RemoveBot()

	if got := c.BotCount(); got != 1 {
		t.Errorf("BotCount after removal: want 1, got %d", got)
	}
}

func TestRemoveBotRemovesNewest(t *testing.T) {
	c := testCtrl()
	c.AddBot() // #1
	c.AddBot() // #2
	c.RemoveBot()

	state := c.State()
	if len(state.Bots) != 1 || state.Bots[0].ID != 1 {
		t.Errorf("oldest bot (#1) should remain; got %v", state.Bots)
	}
}

func TestRemoveBotReturnsFalseWhenNoBots(t *testing.T) {
	c := testCtrl()
	if c.RemoveBot() {
		t.Error("RemoveBot on empty pool should return false")
	}
}

func TestRemoveBotProcessingOrderReturnsToPending(t *testing.T) {
	// Long duration ensures the bot is still processing when we remove it.
	c := New(WithProcessDuration(5 * time.Second))
	c.NewOrder(order.Normal)
	c.AddBot()

	time.Sleep(50 * time.Millisecond) // let bot pick up the order

	state := c.State()
	if len(state.Bots) == 0 || state.Bots[0].CurrentOrder == nil {
		t.Skip("bot did not pick up order in time – test is timing-sensitive")
	}

	c.RemoveBot()
	time.Sleep(50 * time.Millisecond) // let goroutine hand order back

	if got := c.PendingCount(); got != 1 {
		t.Errorf("interrupted order should return to pending; PendingCount=%d", got)
	}
	if got := c.CompletedCount(); got != 0 {
		t.Errorf("interrupted order should not be completed; CompletedCount=%d", got)
	}
}

func TestRemoveBotReturnedOrderMaintainsVIPPriority(t *testing.T) {
	c := New(WithProcessDuration(5 * time.Second))

	c.NewOrder(order.VIP) // #1 – bot picks this up
	c.AddBot()
	time.Sleep(50 * time.Millisecond) // let bot start processing #1

	c.NewOrder(order.Normal) // #2 – in queue
	c.NewOrder(order.Normal) // #3 – in queue

	state := c.State()
	if len(state.Bots) == 0 || state.Bots[0].CurrentOrder == nil {
		t.Skip("bot did not pick up order in time")
	}

	c.RemoveBot()
	time.Sleep(50 * time.Millisecond)

	// Returned VIP #1 should be at position 0
	snap := c.State().Pending
	if len(snap) == 0 {
		t.Fatal("expected orders in pending queue")
	}
	if snap[0].ID != 1 || snap[0].Type != order.VIP {
		t.Errorf("returned VIP should be at front; got #%d(%s)", snap[0].ID, snap[0].Type)
	}
}

// --- End-to-end flow ---

func TestFullFlowVIPProcessedBeforeNormal(t *testing.T) {
	// One bot, orders added before bot starts so we control queue state.
	c := New(WithProcessDuration(100 * time.Millisecond))
	c.NewOrder(order.Normal) // #1
	c.NewOrder(order.Normal) // #2
	c.NewOrder(order.VIP)    // #3 – jumps to front: queue = [#3, #1, #2]
	c.AddBot()

	// Wait for two orders to complete
	time.Sleep(250 * time.Millisecond)

	state := c.State()
	if len(state.Completed) < 2 {
		t.Fatalf("want ≥2 completed, got %d", len(state.Completed))
	}
	if state.Completed[0].Type != order.VIP {
		t.Errorf("first completed should be VIP; got %s", state.Completed[0].Type)
	}
}

func TestFullFlowBotBecomesIdleAndResumes(t *testing.T) {
	c := testCtrl()
	c.NewOrder(order.Normal)
	c.AddBot()

	time.Sleep(testDur * 3) // order completes, bot becomes idle

	if c.PendingCount() != 0 {
		t.Errorf("expected empty pending after processing")
	}

	// Add another order – idle bot should pick it up
	c.NewOrder(order.Normal)
	time.Sleep(testDur * 3)

	if got := c.CompletedCount(); got != 2 {
		t.Errorf("bot should process second order; CompletedCount=%d", got)
	}
}
