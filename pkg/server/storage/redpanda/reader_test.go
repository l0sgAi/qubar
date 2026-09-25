package redpanda

import (
	"testing"
	"time"
)

func TestReadBackoff_DoublesAndCaps(t *testing.T) {
	var b readBackoff
	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 30 * time.Second, 30 * time.Second,
	}
	for i, w := range want {
		if got := b.Next(); got != w {
			t.Fatalf("step %d: got %v, want %v", i, got, w)
		}
	}
}

func TestReadBackoff_ResetStartsOver(t *testing.T) {
	var b readBackoff
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != readBackoffMin {
		t.Fatalf("after reset got %v, want %v", got, readBackoffMin)
	}
}
