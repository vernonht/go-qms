package order

import (
	"testing"
	"time"
)

func TestNewSetsFieldsCorrectly(t *testing.T) {
	before := time.Now()
	o := New(42, VIP)
	after := time.Now()

	if o.ID != 42 {
		t.Errorf("ID: want 42, got %d", o.ID)
	}
	if o.Type != VIP {
		t.Errorf("Type: want VIP, got %v", o.Type)
	}
	if o.Status != Pending {
		t.Errorf("Status: want Pending, got %v", o.Status)
	}
	if o.CreatedAt.Before(before) || o.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v is outside expected window [%v, %v]", o.CreatedAt, before, after)
	}
}

func TestTypeString(t *testing.T) {
	tests := []struct {
		t    Type
		want string
	}{
		{Normal, "Normal"},
		{VIP, "VIP"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("Type(%d).String() = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Pending, "PENDING"},
		{Processing, "PROCESSING"},
		{Complete, "COMPLETE"},
		{Status(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}
