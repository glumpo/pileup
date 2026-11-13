package node_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
)

func browsed(t *testing.T, b *bus, service string) node.DiscoveryHandler {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, w := range b.watches {
		if w.service == service {
			return w.handler
		}
	}
	t.Fatalf("%s is not browsed", service)
	return nil
}

func TestLocalNetworkDenialIsKeptUntilAllowed(t *testing.T) {
	b := &bus{}
	n := openOn(t, t.TempDir(), "ios", b.join("127.0.0.1"))
	if n.LocalNetworkDenied() {
		t.Fatal("denied before the platform said so")
	}
	browsed(t, b, "_googlecast._tcp").Denied(true)
	if !n.LocalNetworkDenied() {
		t.Fatal("not denied after the platform reported a denial")
	}
	if !slices.ContainsFunc(n.Diag().Events(), func(e diag.Event) bool {
		return e.Kind == diag.Discovery && strings.Contains(e.Message, "denied")
	}) {
		t.Fatalf("denial is not in the journal: %+v", n.Diag().Events())
	}
	browsed(t, b, "_pileup._tcp").Denied(false)
	if n.LocalNetworkDenied() {
		t.Fatal("still denied after the platform reported access")
	}
}

func TestJoinWithDeniedLocalNetworkNamesTheCause(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	i, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	i.Secret = make([]byte, 32)
	bs := &bus{}
	j := openOn(t, t.TempDir(), "ios", bs.join("127.0.0.1"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := j.Join(ctx, i.String(), "Боря"); !errors.Is(err, node.ErrUnreachable) || errors.Is(err, node.ErrLocalNetwork) {
		t.Fatalf("join with access: err = %v, want ErrUnreachable only", err)
	}
	browsed(t, bs, "_pileup._tcp").Denied(true)
	if _, err := j.Join(ctx, i.String(), "Боря"); !errors.Is(err, node.ErrLocalNetwork) {
		t.Fatalf("join without access: err = %v, want ErrLocalNetwork", err)
	}
}
