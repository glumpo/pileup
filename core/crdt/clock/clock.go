package clock

type Clock struct {
	l uint64
}

func (c Clock) Now() uint64 { return c.l }

func (c Clock) Next() uint64 { return c.l + 1 }

func (c *Clock) Observe(lamport uint64) { c.l = max(c.l, lamport) }
