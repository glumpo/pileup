package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/media"
	"github.com/glumpo/pileup/core/store"
)

var (
	ErrNoMedia      = errors.New("node: no media library on this device")
	ErrUnknownAsset = errors.New("node: unknown asset")
	ErrMedia        = errors.New("node: media library failed")
)

type MediaProvider interface {
	Media(from, to int64) ([]byte, error)
}

type MediaItem struct {
	LocalID           string  `json:"local_id"`
	Kind              string  `json:"kind"`
	PlatformTime      *int64  `json:"platform_time"`
	ExifDateTime      string  `json:"exif_date_time"`
	ExifOffset        string  `json:"exif_offset"`
	VideoCreationTime *int64  `json:"video_creation_time"`
	FileTime          *int64  `json:"file_time"`
	Width             uint32  `json:"width"`
	Height            uint32  `json:"height"`
	DurationMs        *uint64 `json:"duration_ms"`
	SizeBytes         uint64  `json:"size_bytes"`
	MIME              string  `json:"mime"`
	Codec             string  `json:"codec"`
	OriginalName      string  `json:"original_name"`
	Modified          int64   `json:"modified"`
	Screenshot        bool    `json:"screenshot"`
	Hidden            bool    `json:"hidden"`
}

type Candidate struct {
	MediaItem
	Meta     op.AssetMeta
	Selected bool
}

type Selection struct {
	From, To int64
	Items    []Candidate
}

type legacySelection struct {
	From   int64             `json:"from"`
	To     int64             `json:"to"`
	Manual map[string]bool   `json:"manual"`
	Local  map[string]string `json:"local"`
}

var kinds = map[string]op.Kind{"photo": op.KindPhoto, "video": op.KindVideo, "live_photo": op.KindLivePhoto}

var metaEnc = mustMetaEnc()

func mustMetaEnc() cbor.EncMode {
	opts := cbor.CoreDetEncOptions()
	opts.NilContainers = cbor.NilContainerAsEmpty
	m, err := opts.EncMode()
	if err != nil {
		panic(err)
	}
	return m
}

func (n *Node) Select(ctx context.Context, id []byte, from, to int64) error {
	n.selecting.Lock()
	defer n.selecting.Unlock()
	p, c, err := n.choice(id)
	if err != nil {
		return err
	}
	c.From, c.To = from, to
	cs, err := n.candidates(c)
	if err != nil {
		return err
	}
	n.shown.put(id, c, cs)
	return n.commit(ctx, p, c, cs)
}

func (n *Node) Decide(ctx context.Context, id, asset []byte, selected bool) error {
	n.selecting.Lock()
	defer n.selecting.Unlock()
	p, c, err := n.choice(id)
	if err != nil {
		return err
	}
	cs, ok := n.shown.get(id, c)
	if !ok {
		if cs, err = n.candidates(c); err != nil {
			return err
		}
		n.shown.put(id, c, cs)
	}
	if !slices.ContainsFunc(cs, func(x Candidate) bool { return bytes.Equal(x.Meta.AssetID, asset) }) {
		return ErrUnknownAsset
	}
	c.Manual[string(asset)] = selected
	return n.commit(ctx, p, c, cs)
}

func (n *Node) Selection(id []byte) (Selection, error) {
	p, c, err := n.choice(id)
	if err != nil {
		return Selection{}, err
	}
	cs, err := n.candidates(c)
	if err != nil {
		return Selection{}, err
	}
	n.shown.put(id, c, cs)
	mine, err := n.published(p)
	if err != nil {
		return Selection{}, err
	}
	for i := range cs {
		_, cs[i].Selected = mine[string(cs[i].Meta.AssetID)]
	}
	return Selection{From: c.From, To: c.To, Items: cs}, nil
}

func (n *Node) Assets(id []byte) (map[string]state.Asset, error) {
	p, err := n.find(id)
	if err != nil {
		return nil, err
	}
	v, err := p.store.View()
	if err != nil {
		return nil, err
	}
	return v.Assets, nil
}

func (n *Node) choice(id []byte) (*pile, store.Selection, error) {
	p, err := n.find(id)
	if err != nil {
		return nil, store.Selection{}, err
	}
	c, err := p.store.Selection()
	return p, c, err
}

func (n *Node) importSelection(id []byte, s *store.Store) error {
	path := filepath.Join(n.cfg.Dir, "piles", hex.EncodeToString(id)+".selection.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var old legacySelection
	if err == nil {
		err = json.Unmarshal(b, &old)
	}
	if err != nil {
		return err
	}
	c := store.Selection{From: old.From, To: old.To, Manual: map[string]bool{}, Files: map[string]string{}}
	for k, v := range old.Manual {
		asset, err := hex.DecodeString(k)
		if err != nil {
			return err
		}
		c.Manual[string(asset)] = v
	}
	for k, v := range old.Local {
		asset, err := hex.DecodeString(k)
		if err != nil {
			return err
		}
		c.Files[string(asset)] = v
	}
	if err := s.Select(n.key, now(), c); err != nil {
		return err
	}
	return os.Remove(path)
}

func (n *Node) candidates(c store.Selection) ([]Candidate, error) {
	if n.cfg.Media == nil {
		return nil, ErrNoMedia
	}
	b, err := n.cfg.Media.Media(c.From, c.To)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMedia, err)
	}
	var items []MediaItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	raw := make([]op.RawTime, len(items))
	for i, it := range items {
		raw[i] = op.RawTime{
			PlatformTime:      it.PlatformTime,
			ExifDateTime:      it.ExifDateTime,
			ExifOffset:        it.ExifOffset,
			VideoCreationTime: it.VideoCreationTime,
			FileTime:          it.FileTime,
		}
	}
	times := media.TakenAt(raw)
	me := n.Author()
	var out []Candidate
	for i, it := range items {
		kind, ok := kinds[it.Kind]
		t := times[i]
		if !ok || t.TakenAt < c.From || t.TakenAt >= c.To {
			continue
		}
		out = append(out, Candidate{MediaItem: it, Meta: op.AssetMeta{
			AssetID:        digest(me[:], []byte(it.LocalID)),
			Revision:       digest(binary.BigEndian.AppendUint64(nil, uint64(it.Modified))),
			Kind:           kind,
			TakenAt:        t.TakenAt,
			TakenAtSource:  t.TakenAtSource,
			LocalOffsetSec: t.LocalOffsetSec,
			RawTime:        raw[i],
			Width:          it.Width,
			Height:         it.Height,
			DurationMs:     it.DurationMs,
			SizeBytes:      it.SizeBytes,
			MIME:           it.MIME,
			Codec:          it.Codec,
			OriginalName:   it.OriginalName,
		}})
	}
	return out, nil
}

const listedFor = 5 * time.Minute

type listing struct {
	mu       sync.Mutex
	pile     string
	from, to int64
	at       time.Time
	items    []Candidate
}

func (l *listing) put(id []byte, c store.Selection, cs []Candidate) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pile, l.from, l.to, l.at, l.items = string(id), c.From, c.To, time.Now(), slices.Clone(cs)
}

func (l *listing) get(id []byte, c store.Selection) ([]Candidate, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pile != string(id) || l.from != c.From || l.to != c.To || time.Since(l.at) >= listedFor {
		return nil, false
	}
	return slices.Clone(l.items), true
}

func wants(c store.Selection, x Candidate) bool {
	if v, ok := c.Manual[string(x.Meta.AssetID)]; ok {
		return v
	}
	return !x.Screenshot && !x.Hidden
}

func (n *Node) commit(ctx context.Context, p *pile, c store.Selection, cs []Candidate) error {
	c.Files = map[string]string{}
	for _, x := range cs {
		if wants(c, x) {
			c.Files[string(x.Meta.AssetID)] = x.LocalID
		}
	}
	have, err := n.published(p)
	if err != nil {
		return err
	}
	var add op.AssetAdd
	for _, x := range cs {
		if !wants(c, x) {
			continue
		}
		m, ok := have[string(x.Meta.AssetID)]
		delete(have, string(x.Meta.AssetID))
		if ok && bytes.Equal(m.Revision, x.Meta.Revision) {
			x.Meta.Thumbhash = m.Thumbhash
		}
		if !ok || !same(m, x.Meta) {
			add.Assets = append(add.Assets, x.Meta)
		}
	}
	var remove op.AssetRemove
	for id := range have {
		remove.AssetIDs = append(remove.AssetIDs, []byte(id))
	}
	var changes []op.Change
	if len(remove.AssetIDs) > 0 {
		changes = append(changes, remove)
	}
	if len(add.Assets) > 0 {
		changes = append(changes, add)
	}
	return p.replica.Select(ctx, now(), c, changes...)
}

func (n *Node) published(p *pile) (map[string]op.AssetMeta, error) {
	v, err := p.store.View()
	if err != nil {
		return nil, err
	}
	mine := map[string]op.AssetMeta{}
	for id, a := range v.Assets {
		if a.Owner == n.Author() {
			mine[id] = a.Meta
		}
	}
	return mine, nil
}

func same(a, b op.AssetMeta) bool {
	x, errX := metaEnc.Marshal(a)
	y, errY := metaEnc.Marshal(b)
	return errX == nil && errY == nil && bytes.Equal(x, y)
}

func digest(parts ...[]byte) []byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}
