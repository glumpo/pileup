package node

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func (n *Node) Pins(id []byte) ([]string, error) {
	p, err := n.find(id)
	if err != nil {
		return nil, err
	}
	list, err := n.blobs.Pins(string(p.ID))
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = hex.EncodeToString([]byte(a))
	}
	return out, err
}

func (n *Node) SetPins(id []byte, assets []string) error {
	p, err := n.find(id)
	if err != nil {
		return err
	}
	return n.setPins(p.ID, assets)
}

func (n *Node) setPins(id []byte, assets []string) error {
	list := make([]string, len(assets))
	for i, a := range assets {
		b, err := hex.DecodeString(a)
		if err != nil {
			return err
		}
		list[i] = string(b)
	}
	return n.blobs.SetPins(string(id), list)
}

func (n *Node) importPins(id []byte) error {
	path := filepath.Join(n.cfg.Dir, "piles", hex.EncodeToString(id)+".pins.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var assets []string
	if err == nil {
		err = json.Unmarshal(b, &assets)
	}
	if err == nil {
		err = n.setPins(id, assets)
	}
	if err == nil {
		err = os.Remove(path)
	}
	return err
}

func (n *Node) Free() (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(n.cfg.Dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
