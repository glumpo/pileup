package blob_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/glumpo/pileup/core/blob"
)

func open(t *testing.T, dir string) *blob.Store {
	t.Helper()
	s, err := blob.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, s *blob.Store, content []byte) blob.Hash {
	t.Helper()
	h, err := s.Put(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func has(t *testing.T, s *blob.Store, h blob.Hash) bool {
	t.Helper()
	ok, err := s.Has(h)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func read(t *testing.T, s *blob.Store, h blob.Hash) []byte {
	t.Helper()
	f, err := s.Open(h)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func files(t *testing.T, dir string) (count int, size int64) {
	t.Helper()
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		count++
		size += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count, size
}

func TestBlobIsAddressedByContentHash(t *testing.T) {
	s := open(t, t.TempDir())
	content := []byte("превью 256px")
	h := put(t, s, content)
	if h != sha256.Sum256(content) {
		t.Fatalf("hash %s, want SHA-256 of content", h)
	}
	if !has(t, s, h) {
		t.Fatal("stored blob is missing")
	}
	if got := read(t, s, h); !bytes.Equal(got, content) {
		t.Fatalf("read %q, want %q", got, content)
	}

	other := blob.Hash(sha256.Sum256([]byte("other")))
	if has(t, s, other) {
		t.Fatal("blob that was never stored is present")
	}
	if _, err := s.Open(other); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open missing blob: err = %v, want fs.ErrNotExist", err)
	}
}

func TestSameContentIsStoredOnce(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	content := bytes.Repeat([]byte("x"), 1000)
	first := put(t, s, content)
	if second := put(t, s, content); second != first {
		t.Fatalf("same content got hashes %s and %s", first, second)
	}
	if n, size := files(t, dir); n != 1 || size != int64(len(content)) {
		t.Fatalf("%d files of %d bytes on disk, want one of %d", n, size, len(content))
	}
	put(t, s, []byte("y"))
	if n, _ := files(t, dir); n != 2 {
		t.Fatalf("%d files on disk after different content, want 2", n)
	}
}

func TestBlobIsInvisibleUntilFullyWritten(t *testing.T) {
	s := open(t, t.TempDir())
	content := bytes.Repeat([]byte("0123456789"), 100000)
	h := blob.Hash(sha256.Sum256(content))
	r, w := io.Pipe()
	done := make(chan error)
	go func() {
		_, err := s.Put(r)
		done <- err
	}()

	half := content[:len(content)/2]
	if _, err := w.Write(half); err != nil {
		t.Fatal(err)
	}
	if has(t, s, h) || has(t, s, sha256.Sum256(half)) {
		t.Fatal("partially written blob is visible")
	}
	if _, err := s.Open(h); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open partially written blob: err = %v, want fs.ErrNotExist", err)
	}

	if _, err := w.Write(content[len(half):]); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := read(t, s, h); !bytes.Equal(got, content) {
		t.Fatal("blob content differs after write")
	}
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("source lost") }

func TestFailedPutLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	part := []byte("начало файла")
	if _, err := s.Put(io.MultiReader(bytes.NewReader(part), failing{})); err == nil {
		t.Fatal("put from failing reader succeeded")
	}
	if has(t, s, sha256.Sum256(part)) {
		t.Fatal("partial content stored as blob")
	}
	if n, _ := files(t, dir); n != 0 {
		t.Fatalf("%d files left on disk after failed put", n)
	}
}

func TestBlobsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	content := []byte("оригинал")
	h := put(t, open(t, dir), content)

	s := open(t, dir)
	if !has(t, s, h) {
		t.Fatal("blob missing after reopen")
	}
	if got := read(t, s, h); !bytes.Equal(got, content) {
		t.Fatalf("read %q after reopen, want %q", got, content)
	}
}
