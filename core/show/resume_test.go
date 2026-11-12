package show_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func restartPhone(t *testing.T, ph *phone) *phone {
	t.Helper()
	ph.show.Close()
	if err := ph.Close(); err != nil {
		t.Fatal(err)
	}
	return openPhone(t, ph.dir, "android", ph.lib, node.DefaultBudget)
}

func showPhone(t *testing.T) (*phone, node.Pile) {
	t.Helper()
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	for i, local := range []string{"A", "B", "C", "D"} {
		a.lib.photo(local, i)
	}
	a.share(p)
	waitTimeline(t, p, 4, a)
	return a, p
}

func TestInterruptedShowIsKnownAfterRestart(t *testing.T) {
	a, p := showPhone(t)
	rx := newReceiver(t)
	tv := a.tv(rx)
	must(t)(a.show.Start(p.ID, tv, nil))
	must(t)(a.show.Next())
	if st := must(t)(a.show.Next()); current(st) != "C" {
		t.Fatalf("show is at %s, want C", current(st))
	}
	if _, err := a.show.Interrupted(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("running show is reported as interrupted: %v", err)
	}
	c := a.asset(p, "C")
	eventually(t, "screen shows C", func() bool {
		l := rx.loaded()
		return len(l) > 0 && a.shows(l[len(l)-1], p, "C")
	})
	loads := len(rx.loaded())

	a = restartPhone(t, a)
	in, err := a.show.Interrupted()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in.Pile, p.ID) || in.Screen.ID != tv || in.Screen.Name != "Гостиная" || !bytes.Equal(in.Asset, c.Meta.AssetID) {
		t.Fatalf("interrupted show %+v, want pile %x, screen %s, file C", in, p.ID, tv)
	}

	st := must(t)(a.show.Start(in.Pile, in.Screen.ID, in.Asset))
	if current(st) != "C" || st.Index != 2 {
		t.Fatalf("continued show starts at %s (%d), want C", current(st), st.Index)
	}
	if l := rx.received(t, loads+1, 5*time.Second); !a.shows(l, p, "C") {
		t.Fatalf("screen got %s after continuing, want C", l.url)
	}
	if _, err := a.show.Interrupted(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("continued show is still reported as interrupted: %v", err)
	}
}

func TestStoppedShowIsNotOffered(t *testing.T) {
	a, p := showPhone(t)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	must(t)(a.show.Next())
	a.show.Stop()

	a = restartPhone(t, a)
	if in, err := a.show.Interrupted(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("stopped show is offered: %+v, %v", in, err)
	}
}

func TestForgottenShowIsNotOffered(t *testing.T) {
	a, p := showPhone(t)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))

	a = restartPhone(t, a)
	if _, err := a.show.Interrupted(); err != nil {
		t.Fatal(err)
	}
	a.show.Forget()
	if in, err := a.show.Interrupted(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("forgotten show is offered: %+v, %v", in, err)
	}
	a = restartPhone(t, a)
	if in, err := a.show.Interrupted(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("forgotten show is offered after restart: %+v, %v", in, err)
	}
}

func TestInterruptedShowContinuesWithNextFileIfCurrentIsRemoved(t *testing.T) {
	a, p := showPhone(t)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), a.asset(p, "B").Meta.AssetID))
	a.show.Close()
	c := a.asset(p, "C")
	a.remove(p, "B")

	a = restartPhone(t, a)
	in, err := a.show.Interrupted()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in.Asset, c.Meta.AssetID) {
		t.Fatalf("interrupted show continues with %x, want C", in.Asset)
	}
}

func TestInterruptedShowConnectsToRememberedAddress(t *testing.T) {
	a, p := showPhone(t)
	rx := newReceiver(t)
	tv := a.tv(rx)
	must(t)(a.show.Start(p.ID, tv, nil))

	a = restartPhone(t, a)
	if len(a.Screens()) != 0 {
		t.Fatalf("screens are known right after restart: %+v", a.Screens())
	}
	in, err := a.show.Interrupted()
	if err != nil {
		t.Fatal(err)
	}
	rx.refusing(true)
	if _, err := a.show.Start(in.Pile, in.Screen.ID, in.Asset); !errors.Is(err, show.ErrUnreachable) {
		t.Fatalf("continuing on a screen that does not answer: %v, want unreachable", err)
	}
	rx.refusing(false)
	if st := must(t)(a.show.Start(in.Pile, in.Screen.ID, in.Asset)); current(st) != "A" {
		t.Fatalf("continued show starts at %s", current(st))
	}
	if _, err := a.show.Start(p.ID, "cast:Кухня", nil); !errors.Is(err, show.ErrUnknownScreen) {
		t.Fatalf("start on a screen that was never found: %v", err)
	}
}
