package node_test

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glumpo/pileup/core/node"
)

func TestPinsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	n := open(t, dir, "android")
	p := create(t, n, "Грузия", "Аня")
	other := create(t, n, "Армения", "Аня")
	if pins, err := n.Pins(p.ID); err != nil || len(pins) != 0 {
		t.Fatalf("pins of a new pile: %v, %v", pins, err)
	}
	if err := n.SetPins(p.ID, []string{"bb", "aa"}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = open(t, dir, "android")
	if pins, err := n.Pins(p.ID); err != nil || !slices.Equal(pins, []string{"aa", "bb"}) {
		t.Fatalf("pins after restart: %v, %v", pins, err)
	}
	if pins, err := n.Pins(other.ID); err != nil || len(pins) != 0 {
		t.Fatalf("pins of another pile: %v, %v", pins, err)
	}
	if err := n.SetPins(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	if pins, err := n.Pins(p.ID); err != nil || len(pins) != 0 {
		t.Fatalf("pins after clearing: %v, %v", pins, err)
	}
	if _, err := n.Pins([]byte("nope")); !errors.Is(err, node.ErrUnknownPile) {
		t.Fatalf("pins of an unknown pile: %v", err)
	}
	if err := n.SetPins([]byte("nope"), nil); !errors.Is(err, node.ErrUnknownPile) {
		t.Fatalf("set pins of an unknown pile: %v", err)
	}
}

func TestPinsOfPreviousVersionAreKept(t *testing.T) {
	dir := t.TempDir()
	n := open(t, dir, "android")
	p := create(t, n, "Грузия", "Аня")
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "piles", hex.EncodeToString(p.ID)+".pins.json")
	if err := os.WriteFile(old, []byte(`["cc","aa"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	n = open(t, dir, "android")
	if pins, err := n.Pins(p.ID); err != nil || !slices.Equal(pins, []string{"aa", "cc"}) {
		t.Fatalf("pins of the previous version: %v, %v", pins, err)
	}
	if err := n.SetPins(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = open(t, dir, "android")
	if pins, err := n.Pins(p.ID); err != nil || len(pins) != 0 {
		t.Fatalf("cleared pins came back after restart: %v, %v", pins, err)
	}
}

func TestFreeSpaceIsReported(t *testing.T) {
	n := open(t, t.TempDir(), "android")
	if free, err := n.Free(); err != nil || free <= 0 {
		t.Fatalf("free space %d, %v", free, err)
	}
}
