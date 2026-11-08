package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
)

var ErrSeed = errors.New("device: stored key has wrong size")

type Keystore interface {
	Load() (seed []byte, err error)
	Save(seed []byte) error
}

func Key(ks Keystore) (ed25519.PrivateKey, error) {
	seed, err := ks.Load()
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		seed = make([]byte, ed25519.SeedSize)
		rand.Read(seed)
		if err := ks.Save(seed); err != nil {
			return nil, err
		}
	}
	if len(seed) != ed25519.SeedSize {
		return nil, ErrSeed
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

type File string

func (f File) Load() ([]byte, error) {
	b, err := os.ReadFile(string(f))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (f File) Save(seed []byte) error {
	return WriteFile(string(f), seed)
}

func WriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	t, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = t.Write(data)
	if err == nil {
		err = t.Sync()
	}
	if cerr := t.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	return err
}
