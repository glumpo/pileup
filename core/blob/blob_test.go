package blob_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/glumpo/pileup/core/blob"
)

const budget = 1 << 20

func open(t *testing.T, dir string, budget int64) *blob.Store {
	t.Helper()
	s, err := blob.Open(dir, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

type call struct {
	ctx      context.Context
	reply    chan reply
	returned chan struct{}
}

type reply struct {
	content string
	err     error
}

type source struct {
	calls    chan *call
	stubborn bool
}

func newSource() *source { return &source{calls: make(chan *call)} }

func (src *source) load(ctx context.Context, w io.Writer) error {
	c := &call{ctx: ctx, reply: make(chan reply), returned: make(chan struct{})}
	defer close(c.returned)
	src.calls <- c
	var r reply
	if src.stubborn {
		r = <-c.reply
	} else {
		select {
		case r = <-c.reply:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if _, err := io.WriteString(w, r.content); err != nil {
		return err
	}
	return r.err
}

func (src *source) idle(t *testing.T) {
	t.Helper()
	synctest.Wait()
	select {
	case <-src.calls:
		t.Fatal("a load started")
	default:
	}
}

var errOffline = errors.New("source is offline")

func offline(context.Context, io.Writer) error { return errOffline }

func fixed(content string) blob.Load {
	return func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, content)
		return err
	}
}

func valid() error { return nil }

func key(asset string, tier int) blob.Key {
	return blob.Key{Owner: "ann", Asset: asset, Revision: "r1", Tier: tier}
}

func req(pile string, k blob.Key, takenAt int64, load blob.Load) blob.Request {
	return blob.Request{Pile: pile, Key: k, TakenAt: takenAt, Valid: valid, Load: load}
}

type result struct {
	content string
	err     error
}

func get(ctx context.Context, s *blob.Store, r blob.Request) result {
	f, _, err := s.Get(ctx, r)
	if err != nil {
		return result{err: err}
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return result{content: string(b), err: err}
}

func async(ctx context.Context, s *blob.Store, r blob.Request) chan result {
	out := make(chan result, 1)
	go func() { out <- get(ctx, s, r) }()
	return out
}

func read(t *testing.T, s *blob.Store, r blob.Request) string {
	t.Helper()
	got := get(context.Background(), s, r)
	if got.err != nil {
		t.Fatalf("%+v: %v", r.Key, got.err)
	}
	return got.content
}

func contents(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), "cache.db") {
			return err
		}
		b, err := os.ReadFile(path)
		out = append(out, string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func held(t *testing.T, s *blob.Store, pile string, tier int) []string {
	t.Helper()
	list, err := s.Held(pile, tier)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, h := range list {
		out = append(out, h.Key.Asset)
	}
	slices.Sort(out)
	return out
}

func usage(t *testing.T, s *blob.Store, pile string) int64 {
	t.Helper()
	n, err := s.Usage(pile)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func drop(t *testing.T, s *blob.Store, pile string, keys ...blob.Key) {
	t.Helper()
	if err := s.Retain(pile, func(k blob.Key) bool { return !slices.Contains(keys, k) }); err != nil {
		t.Fatal(err)
	}
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func TestObjectIsAddressedByContentAndSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, budget)
	content := "превью 256px"
	f, h, err := s.Get(context.Background(), req("A", key("x", 1), 1, fixed(content)))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if h != sha256.Sum256([]byte(content)) {
		t.Fatalf("hash %s, want SHA-256 of the content", h)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = open(t, dir, budget)
	if got := read(t, s, req("A", key("x", 1), 1, offline)); got != content {
		t.Fatalf("after reopen: %q", got)
	}
	equal(t, "files", contents(t, dir), []string{content})
}

func TestFailedLoadLeavesNothingAndIsRetriedAfresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		src := newSource()
		r := req("A", key("x", 2), 1, src.load)
		first := async(context.Background(), s, r)
		c1 := <-src.calls
		c1.reply <- reply{content: "начало файла", err: errOffline}
		if got := <-first; !errors.Is(got.err, errOffline) {
			t.Fatalf("failed load: %+v", got)
		}
		if c1.ctx.Err() == nil {
			t.Fatal("context of the failed attempt is not released")
		}
		equal(t, "files after a failed load", contents(t, dir), nil)
		equal(t, "held after a failed load", held(t, s, "A", 2), nil)

		second := async(context.Background(), s, r)
		c2 := <-src.calls
		if c2.ctx.Err() != nil {
			t.Fatal("retry inherited a cancelled context")
		}
		c2.reply <- reply{content: "целиком"}
		if got := <-second; got.err != nil || got.content != "целиком" {
			t.Fatalf("retry: %+v", got)
		}
	})
}

func TestLastConsumerLeavingStopsLoadAndLeavesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		src := newSource()
		ctx, cancel := context.WithCancel(context.Background())
		r := req("A", key("x", 2), 1, src.load)
		res := async(ctx, s, r)
		c := <-src.calls
		cancel()
		if got := <-res; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancelled request: %+v", got)
		}
		<-c.returned
		synctest.Wait()
		equal(t, "files", contents(t, dir), nil)
		equal(t, "held", held(t, s, "A", 2), nil)

		again := async(context.Background(), s, r)
		(<-src.calls).reply <- reply{content: "T2"}
		if got := <-again; got.content != "T2" {
			t.Fatalf("request after cancellation: %+v", got)
		}
	})
}

func TestLateResultOfUncancellableLoadIsDiscarded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := open(t, t.TempDir(), budget)
		src := newSource()
		src.stubborn = true
		r := req("A", key("x", 2), 1, src.load)
		ctx, cancel := context.WithCancel(context.Background())
		first := async(ctx, s, r)
		c1 := <-src.calls
		cancel()
		if got := <-first; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancelled request: %+v", got)
		}
		if c1.ctx.Err() == nil {
			t.Fatal("load of the only consumer is not cancelled")
		}

		second, third := async(context.Background(), s, r), async(context.Background(), s, r)
		impatient, giveUp := context.WithCancel(context.Background())
		fourth := async(impatient, s, r)
		src.idle(t)
		giveUp()
		if got := <-fourth; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("request that gave up waiting: %+v", got)
		}
		src.idle(t)

		c1.reply <- reply{content: "stale"}
		c2 := <-src.calls
		src.idle(t)
		c2.reply <- reply{content: "fresh"}
		for _, res := range []chan result{second, third} {
			if got := <-res; got.err != nil || got.content != "fresh" {
				t.Fatalf("request after a cancelled load: %+v", got)
			}
		}
	})
}

func TestConsumersShareOneLoadAndOneLeavingDoesNotStopIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := open(t, t.TempDir(), budget)
		src := newSource()
		ctx, cancel := context.WithCancel(context.Background())
		leaving := async(ctx, s, req("A", key("x", 2), 1, src.load))
		staying := async(context.Background(), s, req("B", key("x", 2), 1, src.load))
		c := <-src.calls
		src.idle(t)
		cancel()
		if got := <-leaving; !errors.Is(got.err, context.Canceled) {
			t.Fatalf("leaving consumer: %+v", got)
		}
		synctest.Wait()
		if c.ctx.Err() != nil {
			t.Fatal("load cancelled while another consumer waits")
		}
		c.reply <- reply{content: "shared"}
		if got := <-staying; got.err != nil || got.content != "shared" {
			t.Fatalf("remaining consumer: %+v", got)
		}
		equal(t, "held in the pile that stayed", held(t, s, "B", 2), []string{"x"})
		equal(t, "held in the pile that left", held(t, s, "A", 2), nil)
	})
}

func TestObjectIsKeptWhileHeldAndRemovedWithLastHold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		src := newSource()
		r := req("A", key("x", 2), 1, src.load)
		res := async(context.Background(), s, r)
		c := <-src.calls
		c.reply <- reply{content: "T2"}
		<-res
		if c.ctx.Err() == nil {
			t.Fatal("context of a finished load is not released")
		}
		if got := read(t, s, req("A", key("x", 2), 1, offline)); got != "T2" {
			t.Fatalf("held object: %q", got)
		}
		drop(t, s, "A", key("x", 2))
		equal(t, "files after the last hold", contents(t, dir), nil)
		if got := get(context.Background(), s, req("A", key("x", 2), 1, offline)); !errors.Is(got.err, errOffline) {
			t.Fatalf("removed object: %+v", got)
		}
	})
}

func TestSharedContentLivesUntilItsLastHolder(t *testing.T) {
	t.Run("one object in two piles", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		read(t, s, req("A", key("x", 2), 1, fixed("T2")))
		read(t, s, req("B", key("x", 2), 1, offline))
		drop(t, s, "B", key("x", 2))
		if got := read(t, s, req("A", key("x", 2), 1, offline)); got != "T2" {
			t.Fatalf("pile A after pile B released: %q", got)
		}
		drop(t, s, "A", key("x", 2))
		equal(t, "files", contents(t, dir), nil)
	})
	t.Run("two assets with the same bytes", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		read(t, s, req("A", key("x", 2), 1, fixed("same")))
		read(t, s, req("A", key("z", 2), 2, fixed("same")))
		equal(t, "files", contents(t, dir), []string{"same"})
		drop(t, s, "A", key("x", 2))
		if got := read(t, s, req("A", key("z", 2), 2, offline)); got != "same" {
			t.Fatalf("other asset: %q", got)
		}
		drop(t, s, "A", key("z", 2))
		equal(t, "files", contents(t, dir), nil)
	})
	t.Run("T1 and T2 with the same bytes", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, int64(len("larger T2 of y")))
		read(t, s, req("A", key("x", 1), 1, fixed("tiny")))
		read(t, s, req("A", key("x", 2), 1, fixed("tiny")))
		read(t, s, req("A", key("y", 2), 2, fixed("larger T2 of y")))
		equal(t, "T2 held", held(t, s, "A", 2), []string{"y"})
		if got := read(t, s, req("A", key("x", 1), 1, offline)); got != "tiny" {
			t.Fatalf("T1 after its T2 with the same bytes was evicted: %q", got)
		}
		drop(t, s, "A", key("x", 1), key("y", 2))
		equal(t, "files", contents(t, dir), nil)
	})
}

func TestSameContentPublishedWhileLastHolderIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := open(t, t.TempDir(), budget)
		read(t, s, req("A", key("x", 2), 1, fixed("same")))
		src := newSource()
		res := async(context.Background(), s, req("A", key("z", 2), 2, src.load))
		c := <-src.calls
		drop(t, s, "A", key("x", 2))
		c.reply <- reply{content: "same"}
		if got := <-res; got.content != "same" {
			t.Fatalf("published after the drop: %+v", got)
		}
		if got := read(t, s, req("A", key("z", 2), 2, offline)); got != "same" {
			t.Fatalf("object published after the drop: %q", got)
		}

		read(t, s, req("A", key("y", 2), 3, fixed("same")))
		drop(t, s, "A", key("z", 2))
		if got := read(t, s, req("A", key("y", 2), 3, offline)); got != "same" {
			t.Fatalf("object published before the drop: %q", got)
		}
	})
}

func TestRequestWaitingForCleanupChecksTheAssetAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := open(t, t.TempDir(), budget)
		src := newSource()
		src.stubborn = true
		var removed atomic.Bool
		errGone := errors.New("asset removed from the pile")
		inA := req("A", key("x", 2), 1, src.load)
		inA.Valid = func() error {
			if removed.Load() {
				return errGone
			}
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		first := async(ctx, s, inA)
		c1 := <-src.calls
		cancel()
		<-first
		waitingA := async(context.Background(), s, inA)
		waitingB := async(context.Background(), s, req("B", key("x", 2), 1, src.load))
		src.idle(t)

		newRevision := key("x", 2)
		newRevision.Revision = "r2"
		other := async(context.Background(), s, req("A", newRevision, 1, src.load))
		(<-src.calls).reply <- reply{content: "T2 r2"}
		if got := <-other; got.content != "T2 r2" {
			t.Fatalf("new revision while the old one is cleaned: %+v", got)
		}

		removed.Store(true)
		c1.reply <- reply{content: "stale"}
		if got := <-waitingA; !errors.Is(got.err, errGone) {
			t.Fatalf("request of the pile where the asset is removed: %+v", got)
		}
		c2 := <-src.calls
		src.idle(t)
		c2.reply <- reply{content: "T2 r1"}
		if got := <-waitingB; got.content != "T2 r1" {
			t.Fatalf("request of the other pile: %+v", got)
		}
		equal(t, "held in B", held(t, s, "B", 2), []string{"x"})
		if list, _ := s.Held("A", 2); len(list) != 1 || list[0].Key != newRevision {
			t.Fatalf("pile A holds %+v, want only the new revision", list)
		}
	})
}

func TestAssetRemovedDuringLoadIsNotPublished(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir, budget)
		src := newSource()
		var removed atomic.Bool
		r := req("A", key("x", 2), 1, src.load)
		r.Valid = func() error {
			if removed.Load() {
				return fs.ErrNotExist
			}
			return nil
		}
		res := async(context.Background(), s, r)
		c := <-src.calls
		removed.Store(true)
		c.reply <- reply{content: "T2"}
		if got := <-res; !errors.Is(got.err, fs.ErrNotExist) {
			t.Fatalf("request for a removed asset: %+v", got)
		}
		equal(t, "files", contents(t, dir), nil)
		equal(t, "held", held(t, s, "A", 2), nil)
	})
}

func TestCloseStopsLoadsAndReleasesWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		s, err := blob.Open(dir, budget)
		if err != nil {
			t.Fatal(err)
		}
		network, native := newSource(), newSource()
		native.stubborn = true
		fetching := async(context.Background(), s, req("A", key("x", 2), 1, network.load))
		<-network.calls
		rendering := async(context.Background(), s, req("A", key("y", 2), 1, native.load))
		c := <-native.calls
		closed := make(chan error)
		go func() { closed <- s.Close() }()
		if got := <-fetching; !errors.Is(got.err, blob.ErrClosed) {
			t.Fatalf("fetch during close: %+v", got)
		}
		synctest.Wait()
		select {
		case <-closed:
			t.Fatal("close returned before the native load did")
		default:
		}
		c.reply <- reply{content: "late"}
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		if got := <-rendering; !errors.Is(got.err, blob.ErrClosed) {
			t.Fatalf("render during close: %+v", got)
		}
		if got := get(context.Background(), s, req("A", key("x", 2), 1, network.load)); !errors.Is(got.err, blob.ErrClosed) {
			t.Fatalf("request after close: %+v", got)
		}
		equal(t, "files", contents(t, dir), nil)
	})
}

func TestBudgetEvictsEarliestTakenAtFirst(t *testing.T) {
	s := open(t, t.TempDir(), 3*10)
	name := func(at int64) string { return string(rune('a' + at)) }
	for _, at := range []int64{5, 1, 4, 2, 3} {
		read(t, s, req("A", key(name(at), 2), at, fixed(strings.Repeat(name(at), 10))))
	}
	equal(t, "held", held(t, s, "A", 2), []string{"d", "e", "f"})
	if n := usage(t, s, "A"); n != 30 {
		t.Fatalf("usage %d, want 30", n)
	}
	for at := range int64(5) {
		read(t, s, req("A", key(name(at), 1), at, fixed(strings.Repeat("t", int(at)+20))))
	}
	equal(t, "T1 held", held(t, s, "A", 1), []string{"a", "b", "c", "d", "e"})
	equal(t, "T2 held", held(t, s, "A", 2), []string{"d", "e", "f"})
}

func TestRepeatedRequestsAndSameBytesAreCountedOnce(t *testing.T) {
	s := open(t, t.TempDir(), 2*10)
	read(t, s, req("A", key("x", 2), 1, fixed("xxxxxxxxxx")))
	read(t, s, req("A", key("x", 2), 1, fixed("xxxxxxxxxx")))
	read(t, s, req("A", key("z", 2), 2, fixed("xxxxxxxxxx")))
	read(t, s, req("A", key("y", 2), 3, fixed("yyyyyyyyyy")))
	equal(t, "held", held(t, s, "A", 2), []string{"x", "y", "z"})
	if n := usage(t, s, "A"); n != 20 {
		t.Fatalf("usage %d, want 20", n)
	}
	read(t, s, req("B", key("x", 2), 1, offline))
	if n := usage(t, s, "A"); n != 20 {
		t.Fatalf("usage of A after B took the same object: %d", n)
	}
}

func TestProtectedAndPinnedExceedBudgetUntilReleased(t *testing.T) {
	s := open(t, t.TempDir(), 10)
	if err := s.Protect("A", []blob.Key{key("w2", 2), key("w3", 2)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPins("A", []string{"p1"}); err != nil {
		t.Fatal(err)
	}
	for i, a := range []string{"p1", "w2", "w3", "x4"} {
		read(t, s, req("A", key(a, 2), int64(i+1), fixed(strings.Repeat(a, 5))))
	}
	equal(t, "held over budget", held(t, s, "A", 2), []string{"p1", "w2", "w3"})
	if n := usage(t, s, "A"); n != 30 {
		t.Fatalf("usage %d, want 30 over the budget of 10", n)
	}
	if err := s.Protect("A", nil); err != nil {
		t.Fatal(err)
	}
	equal(t, "held after the window moved", held(t, s, "A", 2), []string{"p1"})
	if err := s.SetPins("A", nil); err != nil {
		t.Fatal(err)
	}
	equal(t, "held after unpinning within budget", held(t, s, "A", 2), []string{"p1"})
	read(t, s, req("A", key("y5", 2), 5, fixed("yyyyyyyyyy")))
	equal(t, "held after a later file", held(t, s, "A", 2), []string{"y5"})
}

func TestAccountingAndPinsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, 2*10)
	read(t, s, req("A", key("x", 2), 1, fixed("xxxxxxxxxx")))
	if err := s.SetPins("A", []string{"x"}); err != nil {
		t.Fatal(err)
	}
	read(t, s, req("A", key("y", 2), 2, fixed("yyyyyyyyyy")))
	read(t, s, req("A", key("v", 2), 3, fixed("vvvvvvvvvv")))
	equal(t, "held", held(t, s, "A", 2), []string{"v", "x"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, strings.Repeat("ab", sha256.Size))
	if err := os.WriteFile(stray, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}

	s = open(t, dir, 2*10)
	if n := usage(t, s, "A"); n != 20 {
		t.Fatalf("usage after restart %d, want 20", n)
	}
	if pins, err := s.Pins("A"); err != nil || !slices.Equal(pins, []string{"x"}) {
		t.Fatalf("pins after restart: %q, %v", pins, err)
	}
	equal(t, "files after restart", contents(t, dir), []string{"vvvvvvvvvv", "xxxxxxxxxx"})
	read(t, s, req("A", key("z", 2), 4, fixed("zzzzzzzzzz")))
	equal(t, "held after a new file", held(t, s, "A", 2), []string{"x", "z"})
}

func TestObjectWhoseFileVanishedIsDroppedOnOpen(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, budget)
	f, h, err := s.Get(context.Background(), req("A", key("x", 2), 1, fixed("T2")))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, h.String())); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir, budget)
	equal(t, "held", held(t, s, "A", 2), nil)
	if got := read(t, s, req("A", key("x", 2), 1, fixed("T2"))); got != "T2" {
		t.Fatalf("object loaded again: %q", got)
	}
}

func TestCacheIsConsistentAfterLosingLastCommits(t *testing.T) {
	dir, snapshot := t.TempDir(), t.TempDir()
	s := open(t, dir, budget)
	read(t, s, req("A", key("x", 2), 1, fixed("x")))
	for _, name := range []string{"cache.db", "cache.db-wal"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snapshot, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read(t, s, req("A", key("y", 2), 2, fixed("y")))
	drop(t, s, "A", key("x", 2))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cache.db", "cache.db-wal", "cache.db-shm"} {
		os.Remove(filepath.Join(dir, name))
		if b, err := os.ReadFile(filepath.Join(snapshot, name)); err == nil {
			if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	s = open(t, dir, budget)
	equal(t, "held after the lost commits", held(t, s, "A", 2), nil)
	equal(t, "files after the lost commits", contents(t, dir), nil)
	if got := read(t, s, req("A", key("y", 2), 2, fixed("y"))); got != "y" {
		t.Fatalf("object loaded again: %q", got)
	}
}

func TestPanicWhileCheckingRequestDoesNotLockStore(t *testing.T) {
	s := open(t, t.TempDir(), budget)
	func() {
		defer func() { recover() }()
		r := req("A", key("x", 1), 1, fixed("превью"))
		r.Valid = func() error { panic("broken asset") }
		s.Get(context.Background(), r)
	}()
	done := async(context.Background(), s, req("A", key("y", 1), 1, fixed("другое превью")))
	select {
	case got := <-done:
		if got.err != nil || got.content != "другое превью" {
			t.Fatalf("next request: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next request hangs after a panic inside the store")
	}
}
