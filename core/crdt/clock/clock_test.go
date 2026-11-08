package clock_test

import (
	"testing"

	"github.com/glumpo/pileup/core/crdt/clock"
)

func TestClock(t *testing.T) {
	var c clock.Clock
	if c.Now() != 0 || c.Next() != 1 {
		t.Fatalf("fresh clock: now %d next %d", c.Now(), c.Next())
	}
	c.Observe(c.Next())
	if c.Now() != 1 || c.Next() != 2 {
		t.Fatalf("after own op: now %d next %d", c.Now(), c.Next())
	}
	c.Observe(10)
	if c.Now() != 10 || c.Next() != 11 {
		t.Fatalf("after foreign 10: now %d next %d", c.Now(), c.Next())
	}
	c.Observe(3)
	if c.Now() != 10 {
		t.Fatalf("after foreign 3: now %d, want 10", c.Now())
	}
}
