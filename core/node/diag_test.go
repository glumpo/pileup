package node_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
)

func logged(n *node.Node, k diag.Kind, p node.Pile, peer, text string) func() bool {
	return func() bool {
		for _, e := range n.Diag().Events() {
			if e.Kind == k && bytes.Equal(e.Pile, p.ID) && e.Peer == peer && strings.Contains(e.Message, text) {
				return true
			}
		}
		return false
	}
}

func dump(t *testing.T, n *node.Node) {
	t.Helper()
	for _, e := range n.Diag().Events() {
		t.Logf("%s %x %s %s x%d", e.Kind, e.Pile, e.Peer, e.Message, e.Count)
	}
}

func inviterAddr(t *testing.T, inv string) string {
	t.Helper()
	i, err := invite.Parse(inv)
	if err != nil {
		t.Fatal(err)
	}
	return i.Addrs[0].String()
}

func TestSyncAndLostConnectionAreLogged(t *testing.T) {
	a, b := open(t, t.TempDir(), "ios"), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)
	addr := inviterAddr(t, inv)
	join(t, b, inv, "Боря")
	eventually(t, "b logs sync with a", logged(b, diag.Sync, p, addr, ""))
	eventually(t, "a logs the address of b", func() bool {
		for _, e := range a.Diag().Events() {
			if e.Kind == diag.Discovery && bytes.Equal(e.Pile, p.ID) && strings.HasPrefix(e.Peer, "127.0.0.1:") {
				return true
			}
		}
		return false
	})

	a.Close()
	eventually(t, "b logs that a is unreachable", logged(b, diag.Connection, p, addr, ""))
}

func TestRejectedIncomingConnectionIsLogged(t *testing.T) {
	a := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	_, key, _ := ed25519.GenerateKey(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := peer.Dial(ctx, inviterAddr(t, inviteTo(t, a, p)), key, p.ID, []byte("wrong secret")); err == nil {
		c.Close()
		t.Fatal("a accepted a wrong secret")
	}
	eventually(t, "a logs the rejected handshake", logged(a, diag.Connection, p, "127.0.0.1", ""))
}

func TestForkIsLogged(t *testing.T) {
	m := newMedia()
	m.add(photo("IMG_1", day), []byte("jpeg"))
	a := open(t, t.TempDir(), "ios")
	bdir := t.TempDir()
	b := openSource(t, bdir, "android", m)
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)
	join(t, b, inv, "Боря")
	share(t, b, p)
	eventually(t, "a gets the asset of b", func() bool { return len(assetsOn(t, a, p.ID)) == 1 })

	restored, err := node.Open(node.Config{
		Dir:      t.TempDir(),
		Keystore: device.File(filepath.Join(bdir, "device.key")),
		Platform: "android",
		Addrs:    loopback,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	join(t, restored, inv, "Борис")
	eventually(t, "the restored device logs the fork", logged(restored, diag.Sync, p, inviterAddr(t, inv), replica.ErrFork.Error()))
}

func TestBlobFailuresAreLogged(t *testing.T) {
	g := newGallery()
	a, b := openGallery(t, t.TempDir(), g), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	g.put(photo("IMG_0", day), tint(0))
	g.put(photo("IMG_1", day), tint(1))
	g.breakFile("IMG_1", true)
	shared := share(t, a, p)
	broken, healthy := shared["IMG_1"], shared["IMG_0"]
	eventually(t, "a logs the failed rendition", logged(a, diag.Media, p, "", hex.EncodeToString(broken.AssetID)))

	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "b learns the assets", func() bool { return len(assetsOn(t, b, p.ID)) == 2 })
	a.Close()
	if _, _, err := b.Blob(context.Background(), p.ID, healthy.AssetID, healthy.Revision, node.T2); err == nil {
		t.Fatal("b got a T2 from an owner that is gone")
	}
	author := a.Author()
	owner := hex.EncodeToString(author[:])
	if !logged(b, diag.Media, p, owner, hex.EncodeToString(healthy.AssetID))() {
		dump(t, b)
		t.Fatal("b did not log the failed fetch")
	}
}
