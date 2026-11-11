package node_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
)

var garbage = bytes.Repeat([]byte("not a database "), 512)

func TestCorruptCacheIsSetAsideAndRecreated(t *testing.T) {
	s := newSaved(t)
	s.write(t, s.history(t, 1))
	cache := filepath.Join(s.dir, "blobs", "cache.db")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := node.Open(node.Config{Dir: s.dir, Keystore: keystoreOf(s.dir), Platform: "android", Addrs: loopback})
	if err != nil {
		t.Fatalf("node with a corrupt cache.db: %v", err)
	}
	if ps, err := n.Piles(); err != nil || len(ps) != 1 || ps[0].Broken != "" {
		t.Errorf("piles %+v, %v; want the saved pile opened", ps, err)
	}
	aside, _ := filepath.Glob(cache + ".broken-*")
	if len(aside) != 1 {
		t.Fatalf("set aside %v, want one copy", aside)
	}
	if got, err := os.ReadFile(aside[0]); err != nil || !bytes.Equal(got, garbage) {
		t.Errorf("set aside copy differs from the corrupt cache.db (%v)", err)
	}
	eventually(t, "the set aside cache is logged", logged(n, diag.Sync, node.Pile{}, "", filepath.Base(aside[0])))
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	n = s.open(t)
	if again, _ := filepath.Glob(cache + ".broken-*"); len(again) != 1 {
		t.Errorf("after reopening set aside %v, want the new cache.db to open as is", again)
	}
}

func keystoreOf(dir string) device.Keystore {
	return device.File(filepath.Join(dir, "device.key"))
}

type savedPile struct {
	ID     []byte `json:"id"`
	Secret []byte `json:"secret"`
}

func savedPiles(t *testing.T, dir string) []savedPile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "piles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []savedPile
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPileWithUnreadableDatabaseDoesNotStopOthers(t *testing.T) {
	dir := t.TempDir()
	n := openWith(t, dir, "android", shelf())
	georgia, err := n.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	armenia, err := n.Create("Армения", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "piles", hex.EncodeToString(armenia.ID)+".db")
	if err := os.WriteFile(db, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		os.Remove(db + suffix)
	}

	n, err = node.Open(node.Config{Dir: dir, Keystore: keystoreOf(dir), Platform: "android", Addrs: loopback})
	if err != nil {
		t.Fatalf("node with one unreadable pile database: %v", err)
	}
	ps, err := n.Piles()
	if err != nil || len(ps) != 2 {
		t.Fatalf("piles %+v, %v; want both piles listed", ps, err)
	}
	byID := map[string]node.Pile{}
	for _, p := range ps {
		byID[string(p.ID)] = p
	}
	if p := byID[string(georgia.ID)]; p.Name != "Грузия" || p.Broken != "" || len(p.Members) != 1 {
		t.Errorf("working pile listed as %+v", p)
	}
	if p := byID[string(armenia.ID)]; p.Name != "Армения" || p.Broken != "corrupt" {
		t.Errorf("unreadable pile listed as %+v, want its last known name and reason corrupt", p)
	}
	if _, _, err := n.Timeline(georgia.ID); err != nil {
		t.Errorf("timeline of the working pile: %v", err)
	}
	if _, _, err := n.Timeline(armenia.ID); !errors.Is(err, node.ErrBrokenPile) {
		t.Errorf("timeline of the unreadable pile: %v, want ErrBrokenPile", err)
	}
	if _, err := n.Invite(armenia.ID); !errors.Is(err, node.ErrBrokenPile) {
		t.Errorf("invite to the unreadable pile: %v, want ErrBrokenPile", err)
	}
	var secret []byte
	for _, e := range savedPiles(t, dir) {
		if bytes.Equal(e.ID, armenia.ID) {
			secret = e.Secret
		}
	}
	inv := invite.Invite{PileID: armenia.ID, Secret: secret, Addrs: []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:1")}}
	if _, err := n.Join(context.Background(), inv.String(), "Аня"); !errors.Is(err, node.ErrBrokenPile) {
		t.Errorf("joining the unreadable pile again: %v, want ErrBrokenPile", err)
	}
	eventually(t, "the unreadable pile is logged", logged(n, diag.Sync, armenia, "", "not a database"))
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	if got := savedPiles(t, dir); len(got) != 2 {
		t.Errorf("piles.json keeps %d piles, want both", len(got))
	}
	if got, err := os.ReadFile(db); err != nil || !bytes.Equal(got, garbage) {
		t.Errorf("unreadable pile database was changed (%v)", err)
	}
	n = openWith(t, dir, "android", shelf())
	if ps, err := n.Piles(); err != nil || len(ps) != 2 {
		t.Errorf("after reopening piles %+v, %v; want both", ps, err)
	}
}

type failingKeystore struct{ saves int }

var errKeystore = errors.New("keystore: cannot decrypt the seed")

func (k *failingKeystore) Load() ([]byte, error) { return nil, errKeystore }

func (k *failingKeystore) Save([]byte) error {
	k.saves++
	return nil
}

func TestUnreadableDeviceKeyIsNamedAndKept(t *testing.T) {
	ks := &failingKeystore{}
	_, err := node.Open(node.Config{Dir: t.TempDir(), Keystore: ks, Platform: "android", Addrs: loopback})
	if !errors.Is(err, errKeystore) || !strings.Contains(err.Error(), "device key") {
		t.Fatalf("open with an unreadable device key: %v, want the keystore error named as the device key", err)
	}
	if ks.saves != 0 {
		t.Errorf("a new device key was saved %d times", ks.saves)
	}
}
