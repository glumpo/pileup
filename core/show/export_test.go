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
