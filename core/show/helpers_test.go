package show_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

var base = time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

type library struct {
	mu        sync.Mutex
	items     []node.MediaItem
	originals map[string][]byte
	displays  map[string]int
	reads     map[string]int
	broken    map[string]bool
	lost      map[string]string
	content   map[string]string
	gate      chan struct{}
}

func newLibrary() *library {
	return &library{originals: map[string][]byte{}, displays: map[string]int{}, reads: map[string]int{}, broken: map[string]bool{}, lost: map[string]string{}, content: map[string]string{}}
}

func (l *library) add(local, kind, mime string, minute int, original []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := base.Add(time.Duration(minute) * time.Minute).UnixMilli()
	l.items = append(l.items, node.MediaItem{
		LocalID:      local,
		Kind:         kind,
		PlatformTime: &at,
		Width:        1920,
		Height:       1080,
		SizeBytes:    uint64(len(original)),
		MIME:         mime,
		OriginalName: local,
		Modified:     at,
	})
	l.originals[local] = original
}

func (l *library) codec(local, codec string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.items {
		if l.items[i].LocalID == local {
			l.items[i].Codec = codec
		}
	}
}

func (l *library) duration(local string, ms uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.items {
		if l.items[i].LocalID == local {
			l.items[i].DurationMs = &ms
		}
	}
}

func (l *library) lose(local, reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if reason == "" {
		delete(l.lost, local)
	} else {
		l.lost[local] = reason
	}
}

func (l *library) photo(local string, minute int) {
	l.add(local, "photo", "image/heic", minute, []byte("original of "+local))
}

func (l *library) Media(from, to int64) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return json.Marshal(l.items)
}

func display(local string) string { return "T2 of " + local }

func (l *library) Rendition(local string, tier int) ([]byte, error) {
	l.mu.Lock()
	gate := l.gate
	l.mu.Unlock()
	if gate != nil && tier == node.T2 {
		<-gate
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.originals[local]; !ok || tier == node.T2 && l.broken[local] {
		return nil, errors.New("no such file")
	}
	if c, ok := l.content[local]; ok {
		return []byte(c), nil
	}
	if tier == node.T2 {
		l.displays[local]++
		return []byte(display(local)), nil
	}
	return []byte(fmt.Sprintf("T%d of %s", tier, local)), nil
}

func (l *library) OriginalSize(local string) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if reason, ok := l.lost[local]; ok {
		return 0, errors.New(reason)
	}
	return int64(len(l.originals[local])), nil
}

func (l *library) ReadOriginal(local string, offset, length int64) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads[local]++
	b := l.originals[local]
	return b[offset:min(offset+length, int64(len(b)))], nil
}

func (l *library) read(local string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reads[local]
}

func (l *library) displayed(local string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.displays[local]
}

type lan struct {
	mu       sync.Mutex
	browsing map[string]node.DiscoveryHandler
}

func (l *lan) Advertise(service, name string, port int, txt []byte) error { return nil }

func (l *lan) Browse(service string, h node.DiscoveryHandler) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.browsing[service] = h
	return nil
}

func (l *lan) Stop() error { return nil }

type phone struct {
	*node.Node
	t    *testing.T
	dir  string
	lib  *library
	lan  *lan
	show *show.Host
}

func newPhone(t *testing.T, platform string, budget int64) *phone {
	t.Helper()
	return openPhone(t, t.TempDir(), platform, newLibrary(), budget)
}

func openPhone(t *testing.T, dir, platform string, lib *library, budget int64) *phone {
	t.Helper()
	l := &lan{browsing: map[string]node.DiscoveryHandler{}}
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   platform,
		Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
		Media:      lib,
		Renditions: lib,
		Discovery:  l,
		Budget:     budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := show.New(n)
	t.Cleanup(func() {
		h.Close()
		n.Close()
	})
	return &phone{Node: n, t: t, dir: dir, lib: lib, lan: l, show: h}
}

func (ph *phone) found(service, name string, addr string) string {
	ph.t.Helper()
	ap := netip.MustParseAddrPort(addr)
	ph.lan.mu.Lock()
	h := ph.lan.browsing[service]
	ph.lan.mu.Unlock()
	h.Found(name, ap.Addr().String(), int(ap.Port()), nil)
	for _, s := range ph.Screens() {
		if s.Name == name {
			return s.ID
		}
	}
	ph.t.Fatalf("screen %s is not listed", name)
	return ""
}

func (ph *phone) tv(r *receiver) string {
	ph.t.Helper()
	return ph.found("_googlecast._tcp", "Гостиная", r.addr)
}

func (ph *phone) share(p node.Pile) {
	ph.t.Helper()
	if err := ph.Select(context.Background(), p.ID, base.Add(-time.Hour).UnixMilli(), base.Add(24*time.Hour).UnixMilli()); err != nil {
		ph.t.Fatal(err)
	}
}

func (ph *phone) timeline(p node.Pile) []state.Asset {
	ph.t.Helper()
	_, items, err := ph.Timeline(p.ID)
	if err != nil {
		ph.t.Fatal(err)
	}
	return items
}

func (ph *phone) asset(p node.Pile, local string) state.Asset {
	ph.t.Helper()
	for _, a := range ph.timeline(p) {
		if a.Meta.OriginalName == local {
			return a
		}
	}
	ph.t.Fatalf("%s is not in the timeline", local)
	return state.Asset{}
}

func (ph *phone) cached(p node.Pile, local string) bool {
	a := ph.asset(p, local)
	_, ok := ph.Cached(p.ID, a.Meta.AssetID, a.Meta.Revision, node.T2)
	return ok
}

func pileOf(t *testing.T, first *phone, others ...*phone) node.Pile {
	t.Helper()
	p, err := first.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := first.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range others {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := o.Join(ctx, inv, "участник "+strconv.Itoa(i))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func waitTimeline(t *testing.T, p node.Pile, n int, phones ...*phone) {
	t.Helper()
	for _, ph := range phones {
		eventually(t, "timeline", func() bool { return len(ph.timeline(p)) == n })
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func get(t *testing.T, url string, header ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func must(t *testing.T) func(show.State, error) show.State {
	return func(st show.State, err error) show.State {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
}

func current(st show.State) string { return st.Current.Meta.OriginalName }
