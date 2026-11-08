package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func (s *Store) refPath(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(s.dir, "refs", hex.EncodeToString(sum[:]))
}

func (s *Store) SetRef(name string, h Hash) error {
	f, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(h[:])
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), s.refPath(name))
}

func (s *Store) Ref(name string) (Hash, bool, error) {
	b, err := os.ReadFile(s.refPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return Hash{}, false, nil
	}
	var h Hash
	copy(h[:], b)
	return h, err == nil, err
}

func (s *Store) RemoveRef(name string) error {
	err := os.Remove(s.refPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
