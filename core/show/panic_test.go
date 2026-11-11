package show_test

import (
	"testing"
	"time"

	"github.com/glumpo/pileup/core/show"
)

func TestPanicInShowCommandDoesNotLockShow(t *testing.T) {
	a := newPhone(t, "android", 1<<30)
	p := pileOf(t, a)
	a.lib.photo("A1", 0)
	a.share(p)
	waitTimeline(t, p, 1, a)
	must(t)(a.show.Start(p.ID, a.tv(newReceiver(t)), nil))
	func() {
		defer func() { recover() }()
		show.Crash(a.show)
	}()
	done := make(chan error, 1)
	go func() {
		_, err := a.show.State()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("show state hangs after a panic inside a command")
	}
}
