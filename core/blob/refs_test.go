package blob_test

import (
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/blob"
)

func ref(t *testing.T, s *blob.Store, name string) (blob.Hash, bool) {
	t.Helper()
	h, ok, err := s.Ref(name)
	if err != nil {
		t.Fatal(err)
	}
	return h, ok
}

func setRef(t *testing.T, s *blob.Store, name string, h blob.Hash) {
	t.Helper()
	if err := s.SetRef(name, h); err != nil {
		t.Fatal(err)
	}
}

func TestRefPointsNameToBlob(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	thumb, display := put(t, s, []byte("T1")), put(t, s, []byte("T2"))
	names := []string{"asset/rev/1", "asset/rev/2", "../../escape", strings.Repeat("длинное имя ", 100)}
	if _, ok := ref(t, s, names[0]); ok {
		t.Fatal("ref that was never set is present")
	}
	setRef(t, s, names[0], thumb)
	setRef(t, s, names[1], display)
	setRef(t, s, names[2], thumb)
	setRef(t, s, names[3], display)
	setRef(t, s, names[3], thumb)

	s = open(t, dir)
	for i, want := range []blob.Hash{thumb, display, thumb, thumb} {
		if h, ok := ref(t, s, names[i]); !ok || h != want {
			t.Errorf("ref %.20q = %s, %v; want %s", names[i], h, ok, want)
		}
	}
	if _, ok := ref(t, s, "asset/rev/3"); ok {
		t.Fatal("ref that was never set is present after reopen")
	}
}
