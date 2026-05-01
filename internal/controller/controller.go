package controller

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"qms/internal/order"
	"qms/internal/queue"
)

const defaultProcessDuration = 10 * time.Second

// Logger is satisfied by anything that can write formatted log messages.
type Logger interface {
	Printf(format string, v ...interface{})
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...interface{}) {}

type writerLogger struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *writerLogger) Printf(format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(l.w, "[%s] "+format+"\n", append([]interface{}{ts}, v...)...)
}

// NewWriterLogger returns a Logger that prepends HH:MM:SS timestamps to each line.
func NewWriterLogger(w io.Writer) Logger { return &writerLogger{w: w} }

// Controller orchestrates the pending queue, bot pool, and completed list.
type Controller struct {
	mu              sync.Mutex
	queue           *queue.Queue
	completed       []*order.Order
	bots            []*bot
	orderSeq        int32 // accessed via atomic
	botSeq          int32 // accessed via atomic
	workCh          chan struct{}
	processDuration time.Duration
	logger          Logger
	subMu           sync.Mutex
	subs            map[chan struct{}]struct{}
}

type bot struct {
	id           int
	ctrl         *Controller
	stopCh       chan struct{}
	mu           sync.Mutex
	currentOrder *order.Order
}

// Option configures a Controller at construction time.
type Option func(*Controller)

// WithProcessDuration overrides the 10-second per-order processing time.
// Useful in tests or demo mode.
func WithProcessDuration(d time.Duration) Option {
	return func(c *Controller) { c.processDuration = d }
}

// WithLogger sets the controller's logger (default: silent).
func WithLogger(l Logger) Option {
	return func(c *Controller) { c.logger = l }
}

// New returns a ready-to-use Controller.
func New(opts ...Option) *Controller {
	c := &Controller{
		queue:           &queue.Queue{},
		workCh:          make(chan struct{}, 1000), // buffered to avoid blocking on signals
		processDuration: defaultProcessDuration,
		logger:          noopLogger{},
		subs:            make(map[chan struct{}]struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewOrder enqueues a new order and returns it. Thread-safe.
func (c *Controller) NewOrder(t order.Type) *order.Order {
	id := int(atomic.AddInt32(&c.orderSeq, 1))
	o := order.New(id, t)
	c.queue.Enqueue(o)
	c.logger.Printf("Order #%d (%s) created → PENDING", id, t)
	c.signal()
	c.notify()
	return o
}

// AddBot creates a new bot and starts it immediately. Thread-safe.
func (c *Controller) AddBot() *bot {
	id := int(atomic.AddInt32(&c.botSeq, 1))
	b := &bot{
		id:     id,
		ctrl:   c,
		stopCh: make(chan struct{}),
	}
	c.mu.Lock()
	c.bots = append(c.bots, b)
	c.mu.Unlock()
	c.logger.Printf("Bot #%d added", id)
	go b.run()
	c.signal() // wake bot in case orders are already waiting
	c.notify()
	return b
}

// RemoveBot destroys the newest bot. If it is processing an order the order
// is returned to the front of its priority group in PENDING. Thread-safe.
func (c *Controller) RemoveBot() bool {
	c.mu.Lock()
	if len(c.bots) == 0 {
		c.mu.Unlock()
		c.logger.Printf("No bots to remove")
		return false
	}
	b := c.bots[len(c.bots)-1]
	c.bots = c.bots[:len(c.bots)-1]
	c.mu.Unlock()

	close(b.stopCh)
	c.logger.Printf("Bot #%d removed", b.id)
	c.notify()
	return true
}

// State returns a consistent point-in-time snapshot of the system. Thread-safe.
func (c *Controller) State() State {
	c.mu.Lock()
	bots := make([]BotInfo, len(c.bots))
	for i, b := range c.bots {
		b.mu.Lock()
		bots[i] = BotInfo{ID: b.id, CurrentOrder: b.currentOrder}
		b.mu.Unlock()
	}
	completed := make([]*order.Order, len(c.completed))
	copy(completed, c.completed)
	c.mu.Unlock()

	return State{
		Pending:   c.queue.Snapshot(),
		Completed: completed,
		Bots:      bots,
	}
}

// PendingCount returns the number of unprocessed orders. Thread-safe.
func (c *Controller) PendingCount() int { return c.queue.Len() }

// CompletedCount returns the number of completed orders. Thread-safe.
func (c *Controller) CompletedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.completed)
}

// BotCount returns the number of active bots. Thread-safe.
func (c *Controller) BotCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bots)
}

func (c *Controller) signal() {
	select {
	case c.workCh <- struct{}{}:
	default:
	}
}

// Subscribe returns a channel that receives a signal whenever system state changes.
// The caller must call Unsubscribe when done to free resources.
func (c *Controller) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	c.subMu.Lock()
	c.subs[ch] = struct{}{}
	c.subMu.Unlock()
	return ch
}

// Unsubscribe removes and closes a channel previously returned by Subscribe.
func (c *Controller) Unsubscribe(ch chan struct{}) {
	c.subMu.Lock()
	delete(c.subs, ch)
	c.subMu.Unlock()
	close(ch)
}

func (c *Controller) notify() {
	c.subMu.Lock()
	for ch := range c.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	c.subMu.Unlock()
}

func (c *Controller) completeOrder(b *bot, o *order.Order) {
	o.Status = order.Complete
	c.mu.Lock()
	c.completed = append(c.completed, o)
	c.mu.Unlock()
	c.logger.Printf("Order #%d (%s) completed by Bot #%d → COMPLETE", o.ID, o.Type, b.id)
	c.notify()
}

func (c *Controller) returnOrder(o *order.Order) {
	o.Status = order.Pending
	c.queue.EnqueueFront(o)
	c.logger.Printf("Order #%d (%s) returned to PENDING (bot interrupted)", o.ID, o.Type)
	c.notify()
}

func (b *bot) run() {
	for {
		o := b.ctrl.queue.Dequeue()
		if o == nil {
			select {
			case <-b.ctrl.workCh:
				continue
			case <-b.stopCh:
				return
			}
		}

		b.mu.Lock()
		b.currentOrder = o
		b.mu.Unlock()
		b.ctrl.notify()

		b.ctrl.logger.Printf("Bot #%d picking up Order #%d (%s)", b.id, o.ID, o.Type)

		select {
		case <-time.After(b.ctrl.processDuration):
			b.ctrl.completeOrder(b, o)
			b.mu.Lock()
			b.currentOrder = nil
			b.mu.Unlock()
		case <-b.stopCh:
			b.ctrl.returnOrder(o)
			b.mu.Lock()
			b.currentOrder = nil
			b.mu.Unlock()
			return
		}
	}
}

// BotInfo is a snapshot of a single bot's state.
type BotInfo struct {
	ID           int
	CurrentOrder *order.Order // nil when idle
}

// State is a snapshot of the entire controller at a point in time.
type State struct {
	Pending   []*order.Order
	Completed []*order.Order
	Bots      []BotInfo
}
