package show

import (
	"testing"
	"time"
)

const Reload = reload

func Settle(t testing.TB, d time.Duration) {
	old := settle
	settle = d
	t.Cleanup(func() { settle = old })
}

func Pauses(t testing.TB, first, max time.Duration) {
	oldFirst, oldMax := firstPause, maxPause
	firstPause, maxPause = first, max
	t.Cleanup(func() { firstPause, maxPause = oldFirst, oldMax })
}
