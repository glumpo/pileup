package diag_test

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
)

func reopen(t *testing.T, dir string, size int) *diag.Log {
	t.Helper()
	l := diag.Open(dir, size)
	t.Cleanup(func() { l.Close() })
	return l
}

func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	return total
}

func TestEventsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 10)
	l.Add(diag.Discovery, nil, "10.0.0.2:4000", "found")
	l.Add(diag.Sync, []byte("p1"), "10.0.0.2:4000", "received 3 ops")
	l.Add(diag.Renderer, nil, "", "cast lost")
	l.Add(diag.Renderer, nil, "", "cast lost")
	l.Add(diag.Renderer, nil, "", "cast lost")
	before := l.Events()
	l.Close()

	again := reopen(t, dir, 10)
	equal(t, views(again), []view{
		{diag.Renderer, "", "", "cast lost", 3},
		{diag.Sync, "p1", "10.0.0.2:4000", "received 3 ops", 1},
		{diag.Discovery, "", "10.0.0.2:4000", "found", 1},
	})
	for i, e := range again.Events() {
		if !e.Time.Equal(before[i].Time.Truncate(time.Millisecond)) {
			t.Fatalf("event %d time %v after reopen, want %v", i, e.Time, before[i].Time)
		}
	}
	again.Add(diag.Renderer, nil, "", "cast lost")
	if es := again.Events(); es[0].Count != 4 {
		t.Fatalf("repeat after reopen counted %d, want 4", es[0].Count)
	}
}

func TestEventsAreOnDiskBeforeClose(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 10)
	defer l.Close()
	l.Add(diag.Sync, []byte("p1"), "a", "refused")

	other := reopen(t, dir, 10)
	equal(t, views(other), []view{{diag.Sync, "p1", "a", "refused", 1}})
}

func TestFileSizeIsBoundedAndNewestEventsKept(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 1000)
	pad := strings.Repeat("x", 200)
	for i := range 10000 {
		l.Add(diag.Media, nil, "", fmt.Sprintf("%d %s", i, pad))
	}
	l.Close()
	if size := dirSize(t, dir); size > 1<<20 {
		t.Fatalf("log takes %d bytes on disk after 2 MB of events, want at most 1 MiB", size)
	}
	es := reopen(t, dir, 1000).Events()
	if len(es) != 1000 || !strings.HasPrefix(es[0].Message, "9999 ") || !strings.HasPrefix(es[999].Message, "9000 ") {
		t.Fatalf("after reopen %d events, newest %.10q, oldest %.10q", len(es), es[0].Message, es[len(es)-1].Message)
	}
}

func TestFrequentRepeatsDoNotPushOutHistory(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 1000)
	l.Add(diag.Connection, nil, "", "first")
	for range 100000 {
		l.Add(diag.Timeout, nil, "a", "no answer")
	}
	l.Close()
	equal(t, views(reopen(t, dir, 1000)), []view{
		{diag.Timeout, "", "a", "no answer", 100000},
		{diag.Connection, "", "", "first", 1},
	})
}

func TestTornLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 10)
	l.Add(diag.Sync, nil, "a", "before")
	l.Close()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(`{"time":17829`)
		f.Close()
	}
	l = reopen(t, dir, 10)
	l.Add(diag.Sync, nil, "a", "after")
	l.Close()
	equal(t, views(reopen(t, dir, 10)), []view{
		{diag.Sync, "", "a", "after", 1},
		{diag.Sync, "", "a", "before", 1},
	})
}

func TestUnwritableDirKeepsEventsInMemory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := reopen(t, filepath.Join(file, "diag"), 10)
	for range 100 {
		l.Add(diag.Sync, nil, "a", "refused")
	}
	l.Add(diag.Renderer, nil, "", "cast lost")
	es := l.Events()
	if len(es) != 3 || es[0].Message != "cast lost" || es[1].Message != "refused" || es[1].Count != 100 {
		t.Fatalf("events %+v, want cast lost, refused x100 and one note that the log is not saved", es)
	}
	if !strings.Contains(es[2].Message, "not a directory") {
		t.Fatalf("note %q does not say why the log is not saved", es[2].Message)
	}
}

func TestInvitesAreNotLogged(t *testing.T) {
	dir := t.TempDir()
	inv := invite.Invite{
		PileID: []byte("0123456789abcdef"),
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Addrs:  []netip.AddrPort{netip.MustParseAddrPort("192.168.1.5:41000")},
	}.String()
	l := reopen(t, dir, 10)
	l.Add(diag.App, nil, "", "join failed for "+inv+": timeout")
	l.Add(diag.App, nil, "", "scanned "+strings.ToLower(inv))
	l.Close()
	body := strings.TrimPrefix(inv, "PILEUP:")
	for _, e := range l.Events() {
		if strings.Contains(strings.ToUpper(e.Message), body[:16]) {
			t.Fatalf("event %q contains the invite", e.Message)
		}
	}
	if es := l.Events(); !strings.HasPrefix(es[1].Message, "join failed for ") || !strings.HasSuffix(es[1].Message, ": timeout") {
		t.Fatalf("the rest of the message is lost: %q", es[1].Message)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(strings.ToUpper(string(b)), body[:16]) {
			t.Fatalf("%s contains the invite", e.Name())
		}
	}
}
