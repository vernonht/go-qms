package order

import "time"

// Type distinguishes Normal and VIP orders.
type Type int

const (
	Normal Type = iota // iota assigns successive integer values, so Normal=0, VIP=1
	VIP
)

func (t Type) String() string {
	if t == VIP {
		return "VIP"
	}
	return "Normal"
}

// Status tracks where an order is in its lifecycle.
type Status int

const (
	Pending    Status = iota 
	Processing Status = iota
	Complete   Status = iota
)

func (s Status) String() string {
	switch s {
	case Pending:
		return "PENDING"
	case Processing:
		return "PROCESSING"
	case Complete:
		return "COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// Order represents a single customer order.
type Order struct {
	ID        int
	Type      Type
	Status    Status
	CreatedAt time.Time
}

// New creates a new Pending order with the given id and type.
// New creates and returns a new Order instance with the given id and type.
// The order is initialized with a Pending status and the current timestamp.
func New(id int, t Type) *Order {
	return &Order{
		ID:        id,
		Type:      t,
		Status:    Pending,
		CreatedAt: time.Now(),
	}
}
