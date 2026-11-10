package device_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/glumpo/pileup/core/device"
)

func key(t *testing.T, ks device.Keystore) ed25519.PrivateKey {
	t.Helper()
	k, err := device.Key(ks)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestKeyIsCreatedOnceAndReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device.key")
	first := key(t, device.File(path))
	again := key(t, device.File(path))
	if !first.Equal(again) {
		t.Fatal("reloaded key differs from the created one")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestFreshStoreGivesNewAuthor(t *testing.T) {
	a := key(t, device.File(filepath.Join(t.TempDir(), "device.key")))
	b := key(t, device.File(filepath.Join(t.TempDir(), "device.key")))
	if bytes.Equal(a.Public().(ed25519.PublicKey), b.Public().(ed25519.PublicKey)) {
		t.Fatal("two fresh stores gave the same key")
	}
}

type memory struct {
	seed  []byte
	saves int
}

func (m *memory) Load() ([]byte, error) { return m.seed, nil }

func (m *memory) Save(seed []byte) error {
	m.seed = append([]byte{}, seed...)
	m.saves++
	return nil
}

func TestKeyIsSavedOnlyOnFirstUse(t *testing.T) {
	m := &memory{}
	first := key(t, m)
	again := key(t, m)
	if !first.Equal(again) || m.saves != 1 {
		t.Fatalf("saves = %d, same key = %v", m.saves, first.Equal(again))
	}
}

type failing struct{ err error }

func (f failing) Load() ([]byte, error) { return nil, f.err }
func (f failing) Save([]byte) error     { return f.err }

func TestKeystoreErrorsAreReturned(t *testing.T) {
	want := errors.New("keychain locked")
	if _, err := device.Key(failing{want}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestCorruptKeyIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := device.Key(device.File(path)); !errors.Is(err, device.ErrSeed) {
		t.Fatalf("err = %v, want ErrSeed", err)
	}
}

func TestWriteFileReplacesContentWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "piles.json")
	for _, data := range []string{`[{"id":"a"}]`, `[]`} {
		if err := device.WriteFile(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != data {
			t.Fatalf("read back %q, want %q", got, data)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "piles.json" {
		t.Fatalf("directory holds %v, want only piles.json", entries)
	}
}

func TestWriteFileIntoMissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "piles.json")
	if err := device.WriteFile(path, []byte("[]")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}
