package httpapi_test

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
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

func TestFullDiskHasItsOwnCode(t *testing.T) {
	dir, fill := smallDisk(t)
	n, c := serve(t, dir, &gallery{list: string(photos)})
	p, photo := sharedPhoto(t, n)
	fill()

	resp, body := getBlob(t, c.url, blobPath(p, photo, "2"), "secret")
	if resp.StatusCode != http.StatusInsufficientStorage || errorCode(t, body) != "disk_full" {
		t.Fatalf("own T2 on a full disk: status %d, body %s", resp.StatusCode, body)
	}
	journaled(t, n.Diag(), p.ID, "GET /piles/{id}/blob/", "no space left on device")
	c.fails("POST", "/piles", map[string]string{"name": "Армения", "member": "Аня"}, http.StatusInsufficientStorage, "disk_full")
}
