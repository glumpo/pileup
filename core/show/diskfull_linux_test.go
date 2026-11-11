package show_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/glumpo/pileup/core/node"
)

func smallDisk(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "size=32m"); err != nil {
		t.Skipf("cannot mount a small tmpfs to fill: %v", err)
	}
	t.Cleanup(func() { syscall.Unmount(dir, syscall.MNT_DETACH) })
	filler := filepath.Join(dir, "filler")
	return dir, func() {
		f, err := os.Create(filler)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		t.Cleanup(func() { os.Remove(filler) })
		for _, chunk := range []int{1 << 20, 4 << 10, 1} {
			b := bytes.Repeat([]byte{0}, chunk)
			for {
				if _, err := f.Write(b); err != nil {
					if !errors.Is(err, syscall.ENOSPC) {
						t.Fatal(err)
					}
					break
				}
			}
		}
	}
}

func TestFullDiskOfHostIsNotBlamedOnTheOwner(t *testing.T) {
	dir, fill := smallDisk(t)
	a := newPhone(t, "android", node.DefaultBudget)
	b := openPhone(t, dir, "ios", newLibrary(), node.DefaultBudget)
	p := pileOf(t, a, b)
	for i := range 3 {
		a.lib.photo(fmt.Sprintf("A%d", i), i)
	}
	a.share(p)
	waitTimeline(t, p, 3, b)
	eventually(t, "b connects to a", func() bool { return b.Online(p.ID)[a.Author()] })
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	a.lib.mu.Lock()
	a.lib.gate = gate
	a.lib.mu.Unlock()
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	if _, err := b.show.Pin(p.ID, member(a)); err != nil {
		t.Fatal(err)
	}
	fill()
	release()

	eventually(t, "the host names its own full disk", func() bool {
		st := must(t)(b.show.State())
		for _, h := range st.Hints {
			if h.Member == a.Author() {
				t.Fatalf("hint blames the owner for the host's full disk: %+v", st.Hints)
			}
		}
		if r := progressOf(t, b, p.ID)["Аня"].Reason; r != "" && r != "disk_full" {
			t.Fatalf("pin progress of the owner: reason %q, want disk_full", r)
		}
		return len(st.Hints) == 1 && st.Hints[0].Member == b.Author() && st.Hints[0].Reason == "disk_full" &&
			progressOf(t, b, p.ID)["Аня"].Reason == "disk_full"
	})
}
