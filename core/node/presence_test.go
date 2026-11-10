package node_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/node"
)

func presenceIs(n *node.Node, id []byte, who op.Author, want node.Presence) func() bool {
	return func() bool { return n.Presence(id, who) == want }
}

func TestMemberIsOnlineUntilItsPileupCloses(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, nil)
	s.write(t, s.history(t, 1), at.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)

	before := time.Now()
	got := n.Presence(s.id, o.author())
	if !got.Online || got.LastError != "" || got.LastSeen.Before(before) || got.LastSeen.After(time.Now()) {
		t.Fatalf("connected member: %+v, want online now without an error", got)
	}

	before = time.Now()
	at.stop()
	eventually(t, "the member is offline with Pileup closed", func() bool {
		got = n.Presence(s.id, o.author())
		return !got.Online && got.LastError == node.Refused
	})
	if got.LastSeen.Before(before) || got.LastSeen.After(time.Now()) {
		t.Errorf("last seen %v, want the end of the connection after %v", got.LastSeen, before)
	}
}

type listedAt func(t *testing.T) string

func refusedAt(t *testing.T) string { return closedAddr(t).String() }

func silentAt(t *testing.T) string { return listenSilent(t).Addr().String() }

func unroutedAt(t *testing.T) string { return fmt.Sprintf("255.255.255.255:%d", closedAddr(t).Port()) }

func impostorAt(key crypto.Signer) listedAt {
	return func(t *testing.T) string { return serveImpostor(t, selfSigned(t, key)).Addr().String() }
}

func closingAt(t *testing.T) string { return serveRefusing(t).Addr().String() }

func listedMember(t *testing.T, addrs ...listedAt) (*node.Node, []byte, op.Author) {
	t.Helper()
	t.Cleanup(node.SetAnnounceTimeout(time.Second))
	s := newSaved(t)
	_, o := newKey(t)
	var entries []listed
	for _, at := range addrs {
		entries = append(entries, listed{Key: keyOf(o), Addr: at(t)})
	}
	keyF, _ := newKey(t)
	f := serveAs(t, keyF, s.id, s.secret, nil, map[string]http.HandlerFunc{
		"POST /announce": func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(entries) },
	})
	s.write(t, s.history(t, 1), f.addr())
	return s.open(t), s.id, o
}

func TestLastConnectionErrorOfMemberIsClassified(t *testing.T) {
	_, edKey, _ := ed25519.GenerateKey(nil)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		at   listedAt
		want string
	}{
		{"nothing listens: Pileup is closed", refusedAt, node.Refused},
		{"no answer: the phone sleeps or is busy", silentAt, node.TimedOut},
		{"no route: not in this network", unroutedAt, node.NoRoute},
		{"another device without the pile secret", impostorAt(edKey), node.OtherDevice},
		{"another device with a foreign certificate", impostorAt(ecKey), node.OtherDevice},
		{"the device closes the connection", closingAt, node.Failed},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, id, o := listedMember(t, c.at)
			eventually(t, "the member's error is "+c.want, presenceIs(n, id, o, node.Presence{LastError: c.want}))
		})
	}
}

func TestMostHopefulErrorOfMemberAddressesIsReported(t *testing.T) {
	_, edKey, _ := ed25519.GenerateKey(nil)
	for _, c := range []struct {
		name  string
		addrs []listedAt
		want  string
	}{
		{"refused over timeout", []listedAt{silentAt, refusedAt}, node.Refused},
		{"timeout over no route", []listedAt{unroutedAt, silentAt}, node.TimedOut},
		{"no route over another device", []listedAt{impostorAt(edKey), unroutedAt}, node.NoRoute},
		{"another device over other errors", []listedAt{closingAt, impostorAt(edKey)}, node.OtherDevice},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, id, o := listedMember(t, c.addrs...)
			eventually(t, "the member's error is "+c.want, presenceIs(n, id, o, node.Presence{LastError: c.want}))
		})
	}
}

func TestMemberWithoutAddressAndThisDevice(t *testing.T) {
	s := newSaved(t)
	s.write(t, s.history(t, 1))
	n := s.open(t)
	_, stranger := newKey(t)
	if got := n.Presence(s.id, stranger); got != (node.Presence{LastError: node.NoAddress}) {
		t.Errorf("member without a known address: %+v", got)
	}
	before := time.Now()
	if got := n.Presence(s.id, n.Author()); !got.Online || got.LastError != "" || got.LastSeen.Before(before) {
		t.Errorf("this device: %+v, want online now", got)
	}
}
