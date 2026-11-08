package diag_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
)

type view struct {
	Kind    diag.Kind
	Pile    string
	Peer    string
	Message string
	Count   int
}

func views(l *diag.Log) []view {
	var out []view
	for _, e := range l.Events() {
		out = append(out, view{e.Kind, string(e.Pile), e.Peer, e.Message, e.Count})
	}
	return out
}

func equal(t *testing.T, got, want []view) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events\n%v\nwant\n%v", got, want)
	}
}

func TestEventsNewestFirst(t *testing.T) {
	l := diag.New(10)
	before := time.Now()
	l.Add(diag.Discovery, nil, "10.0.0.2:4000", "found")
	l.Add(diag.Sync, []byte("p1"), "10.0.0.2:4000", "received 3 ops")
	l.Add(diag.Renderer, nil, "", "cast lost")
	equal(t, views(l), []view{
		{diag.Renderer, "", "", "cast lost", 1},
		{diag.Sync, "p1", "10.0.0.2:4000", "received 3 ops", 1},
		{diag.Discovery, "", "10.0.0.2:4000", "found", 1},
	})
	es := l.Events()
	if es[2].Time.Before(before) || es[0].Time.After(time.Now()) || es[0].Time.Before(es[2].Time) {
		t.Fatalf("times %v, %v are not when the events happened", es[2].Time, es[0].Time)
	}
}

func TestOldestEventsAreDropped(t *testing.T) {
	l := diag.New(3)
	for i := range 5 {
		l.Add(diag.Connection, nil, "", fmt.Sprint(i))
	}
	equal(t, views(l), []view{
		{diag.Connection, "", "", "4", 1},
		{diag.Connection, "", "", "3", 1},
		{diag.Connection, "", "", "2", 1},
	})
}

func TestRepeatedEventIsCountedAndMovedToTop(t *testing.T) {
	l := diag.New(3)
	l.Add(diag.Sync, []byte("p1"), "a", "refused")
	l.Add(diag.Sync, []byte("p1"), "b", "refused")
	l.Add(diag.Sync, []byte("p2"), "a", "refused")
	l.Add(diag.Sync, []byte("p1"), "a", "refused")
	l.Add(diag.Sync, []byte("p1"), "a", "refused")
	l.Add(diag.Connection, []byte("p1"), "a", "refused")
	equal(t, views(l), []view{
		{diag.Connection, "p1", "a", "refused", 1},
		{diag.Sync, "p1", "a", "refused", 3},
		{diag.Sync, "p2", "a", "refused", 1},
	})
}

func TestTimeoutsAreTheirOwnKind(t *testing.T) {
	l := diag.New(10)
	err := &net.DNSError{Err: "no answer", Name: "tv.local", IsTimeout: true}
	l.Error(diag.Sync, nil, "x", fmt.Errorf("pull: %w", context.DeadlineExceeded))
	l.Error(diag.Connection, nil, "y", err)
	l.Error(diag.Media, nil, "z", fmt.Errorf("read: %w", os.ErrDeadlineExceeded))
	l.Error(diag.Sync, nil, "w", errors.New("author log forked"))
	equal(t, views(l), []view{
		{diag.Sync, "", "w", "author log forked", 1},
		{diag.Timeout, "", "z", "read: i/o timeout", 1},
		{diag.Timeout, "", "y", err.Error(), 1},
		{diag.Timeout, "", "x", "pull: context deadline exceeded", 1},
	})
}

func TestExportIsChronologicalText(t *testing.T) {
	l := diag.New(10)
	l.Add(diag.Connection, []byte{0xab, 0x01}, "10.0.0.2:4000", "announce: connection refused")
	l.Add(diag.Renderer, nil, "", "cast lost")
	l.Add(diag.Connection, []byte{0xab, 0x01}, "10.0.0.2:4000", "announce: connection refused")
	var b strings.Builder
	if err := l.Export(&b); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	es := l.Events()
	want := []string{
		"renderer cast lost",
		"connection pile=ab01 peer=10.0.0.2:4000 x2 announce: connection refused",
	}
	if len(lines) != len(want) {
		t.Fatalf("export\n%s", b.String())
	}
	for i, line := range lines {
		stamp, rest, _ := strings.Cut(line, " ")
		parsed, err := time.Parse(time.RFC3339, stamp)
		if err != nil || !parsed.Equal(es[1-i].Time.Truncate(time.Millisecond)) {
			t.Fatalf("line %q: time %v, %v, want %v", line, parsed, err, es[1-i].Time)
		}
		if rest != want[i] {
			t.Fatalf("line %q, want %q", rest, want[i])
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	l := diag.New(50)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 100 {
				l.Add(diag.Sync, nil, fmt.Sprint(g), fmt.Sprint(i%10))
				l.Events()
			}
		})
	}
	wg.Wait()
	total := 0
	for _, e := range l.Events() {
		total += e.Count
	}
	if n := len(l.Events()); n != 50 {
		t.Fatalf("%d events kept, want 50", n)
	}
	if total > 800 || total < 50 {
		t.Fatalf("counts add up to %d", total)
	}
}
