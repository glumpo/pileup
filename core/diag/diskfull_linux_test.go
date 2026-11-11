package diag_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/diag"
)

func TestFullDiskKeepsEventsInMemory(t *testing.T) {
	dir := t.TempDir()
	l := diag.Open(dir, 10)
	l.Add(diag.Sync, nil, "a", "before")
	l.Close()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		os.Remove(path)
		if err := os.Symlink("/dev/full", path); err != nil {
			t.Fatal(err)
		}
	}

	l = reopen(t, dir, 10)
	for range 100 {
		l.Add(diag.Sync, nil, "a", "refused")
	}
	es := l.Events()
	if len(es) != 2 || es[0].Message != "refused" || es[0].Count != 100 {
		t.Fatalf("events %+v, want refused x100 and one note that the log is not saved", es)
	}
	if !strings.Contains(es[1].Message, "no space left on device") {
		t.Fatalf("note %q does not say why the log is not saved", es[1].Message)
	}
}
