package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"

	"github.com/glumpo/pileup/core/crdt/op"
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

func timeline(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("GET /piles/{id}/timeline", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathHex(w, r, "id", node.ErrUnknownPile)
		if !ok {
			return
		}
		p, assets, err := n.Timeline(id)
		if err != nil {
			fail(w, r, err)
			return
		}
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
		reply(w, http.StatusOK, out)
	})
}
