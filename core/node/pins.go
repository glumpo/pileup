package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/glumpo/pileup/core/device"
)

func (n *Node) Pins(id []byte) ([]string, error) {
	p, err := n.find(id)
	if err != nil {
		return nil, err
	}
	var out []string
	b, err := os.ReadFile(n.pinsPath(p.ID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}

func (n *Node) SetPins(id []byte, assets []string) error {
	p, err := n.find(id)
	if err != nil {
		return err
	}
	b, err := json.Marshal(slices.Sorted(slices.Values(assets)))
	if err != nil {
		return err
	}
	return device.WriteFile(n.pinsPath(p.ID), b)
}

func (n *Node) pinsPath(id []byte) string {
	return filepath.Join(n.cfg.Dir, "piles", hex.EncodeToString(id)+".pins.json")
}

func (n *Node) Free() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(n.cfg.Dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
