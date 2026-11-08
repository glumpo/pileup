package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Hash [sha256.Size]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

type Store struct {
	dir string
}

func Open(dir string) (*Store, error) {
	tmp := filepath.Join(dir, "tmp")
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(h Hash) string { return filepath.Join(s.dir, h.String()) }

func (s *Store) Put(r io.Reader) (Hash, error) {
	f, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "")
	if err != nil {
		return Hash{}, err
	}
	defer os.Remove(f.Name())
	sum := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, sum), r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Hash{}, err
	}
	h := Hash(sum.Sum(nil))
	if ok, err := s.Has(h); ok || err != nil {
		return h, err
	}
	return h, os.Rename(f.Name(), s.path(h))
}

func (s *Store) Open(h Hash) (*os.File, error) {
	return os.Open(s.path(h))
}

func (s *Store) Has(h Hash) (bool, error) {
	_, err := os.Stat(s.path(h))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
