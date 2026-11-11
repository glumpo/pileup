package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/node"
)

var kindNames = map[op.Kind]string{op.KindPhoto: "photo", op.KindVideo: "video", op.KindLivePhoto: "live_photo"}

type item struct {
	ID          string  `json:"id"`
	Revision    string  `json:"revision"`
	Author      string  `json:"author"`
	AuthorName  string  `json:"author_name"`
	Kind        string  `json:"kind"`
	TakenAt     int64   `json:"taken_at"`
	LocalOffset *int32  `json:"local_offset,omitempty"`
	DurationMs  *uint64 `json:"duration_ms,omitempty"`
	Width       uint32  `json:"width"`
	Height      uint32  `json:"height"`
	Thumbhash   string  `json:"thumbhash"`
}

type timelineBody struct {
	changed <-chan struct{}
	etag    string
	body    []byte
}

func timeline(mux *http.ServeMux, n *node.Node) {
	var mu sync.Mutex
	bodies := map[string]timelineBody{}
	mux.HandleFunc("GET /piles/{id}/timeline", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathHex(w, r, "id", node.ErrUnknownPile)
		if !ok {
			return
		}
		changed, err := n.Changed(id)
		if err != nil {
			fail(w, r, err)
			return
		}
		mu.Lock()
		b := bodies[string(id)]
		mu.Unlock()
		if b.changed != changed {
			p, assets, err := n.Timeline(id)
			if err != nil {
				fail(w, r, err)
				return
			}
			b = timelineBody{changed: changed, body: timelineJSON(p, assets)}
			sum := sha256.Sum256(b.body)
			b.etag = `"` + hex.EncodeToString(sum[:16]) + `"`
			mu.Lock()
			bodies[string(id)] = b
			mu.Unlock()
		}
		w.Header().Set("ETag", b.etag)
		if r.Header.Get("If-None-Match") == b.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b.body)
	})
}

func timelineJSON(p node.Pile, assets []state.Asset) []byte {
	out := make([]item, 0, len(assets))
	for _, a := range assets {
		m := a.Meta
		out = append(out, item{
			ID:          hex.EncodeToString(m.AssetID),
			Revision:    hex.EncodeToString(m.Revision),
			Author:      hex.EncodeToString(a.Owner[:]),
			AuthorName:  p.Members[a.Owner].Name,
			Kind:        kindNames[m.Kind],
			TakenAt:     m.TakenAt,
			LocalOffset: m.LocalOffsetSec,
			DurationMs:  m.DurationMs,
			Width:       m.Width,
			Height:      m.Height,
			Thumbhash:   base64.StdEncoding.EncodeToString(m.Thumbhash),
		})
	}
	b, _ := json.Marshal(out)
	return append(b, '\n')
}
