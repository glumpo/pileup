package node_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/node"
)

func TestRenditionIsKeptWhenMemberGivesUp(t *testing.T) {
	m := newMedia()
	a := openSource(t, t.TempDir(), "ios", m)
	p := create(t, a, "Грузия", "Аня")
	m.add(photo("IMG_1", day), []byte("jpeg"))
	img := share(t, a, p)["IMG_1"]
	hold := make(chan chan struct{})
	m.mu.Lock()
	m.hold = hold
	m.mu.Unlock()
	b := memberOf(t, a, p)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+b.addr+blobPath(img, node.T2), nil)
	if err != nil {
		t.Fatal(err)
	}
	gaveUp := make(chan error, 1)
	go func() {
		resp, err := b.http.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		gaveUp <- err
	}()
	release := <-hold
	cancel()
	if err := <-gaveUp; !errors.Is(err, context.Canceled) {
		t.Fatalf("request that gave up: err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	m.mu.Lock()
	m.hold = nil
	m.mu.Unlock()
	close(release)

	eventually(t, "the owner caches T2 the member gave up on", func() bool { return a.Stored(p.ID, img.AssetID, img.Revision, node.T2) })
	resp, body := b.get(blobPath(img, node.T2))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_1", node.T2)) {
		t.Fatalf("T2 asked again: status %d, %q", resp.StatusCode, body)
	}
	if r, _ := m.calls(); r[node.T2] != 1 {
		t.Fatalf("T2 rendered %d times, want once", r[node.T2])
	}
}

type turns struct {
	*media
	started chan string
	gate    chan struct{}
}

func (q *turns) Rendition(local string, tier int) ([]byte, error) {
	q.started <- fmt.Sprintf("T%d of %s", tier, local)
	<-q.gate
	return q.media.Rendition(local, tier)
}

func TestOwnerRendersWaitingDisplayBeforeWaitingThumbnails(t *testing.T) {
	q := &turns{media: newMedia(), started: make(chan string, 16), gate: make(chan struct{})}
	dir := t.TempDir()
	a, err := node.Open(node.Config{Dir: dir, Keystore: device.File(filepath.Join(dir, "device.key")), Platform: "ios", Addrs: loopback, Media: q, Renditions: q})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	p := create(t, a, "Грузия", "Аня")
	for i := range 4 {
		q.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("jpeg"))
	}
	shared := share(t, a, p)
	first := <-q.started
	b := memberOf(t, a, p)

	done := make(chan struct{})
	ask := func(path string) {
		go func() {
			resp, err := b.http.Get("http://" + b.addr + path)
			if err == nil {
				resp.Body.Close()
			}
			done <- struct{}{}
		}()
	}
	for _, m := range shared {
		ask(blobPath(m, node.T1))
	}
	time.Sleep(300 * time.Millisecond)
	ask(blobPath(shared["IMG_0"], node.T2))
	time.Sleep(300 * time.Millisecond)

	q.gate <- struct{}{}
	next := <-q.started
	close(q.gate)
	for range len(shared) + 1 {
		<-done
	}
	if next != "T2 of IMG_0" {
		t.Fatalf("after %s the owner rendered %s, want T2 of IMG_0 before the waiting thumbnails", first, next)
	}
}
