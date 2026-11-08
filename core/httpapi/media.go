package httpapi

import (
	"encoding/hex"
	"net/http"

	"github.com/glumpo/pileup/core/node"
)

type asset struct {
	ID           string `json:"id"`
	LocalID      string `json:"local_id"`
	Kind         string `json:"kind"`
	TakenAt      int64  `json:"taken_at"`
	OriginalName string `json:"original_name"`
	Screenshot   bool   `json:"screenshot"`
	Hidden       bool   `json:"hidden"`
	Selected     bool   `json:"selected"`
}

type selection struct {
	From   int64   `json:"from"`
	To     int64   `json:"to"`
	Assets []asset `json:"assets"`
}

func media(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("GET /piles/{id}/selection", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathHex(w, r, "id", node.ErrUnknownPile)
		if !ok {
			return
		}
		s, err := n.Selection(id)
		if err != nil {
			fail(w, err)
			return
		}
		out := selection{From: s.From, To: s.To, Assets: make([]asset, 0, len(s.Items))}
		for _, c := range s.Items {
			out.Assets = append(out.Assets, asset{
				ID:           hex.EncodeToString(c.Meta.AssetID),
				LocalID:      c.LocalID,
				Kind:         c.Kind,
				TakenAt:      c.Meta.TakenAt,
				OriginalName: c.OriginalName,
				Screenshot:   c.Screenshot,
				Hidden:       c.Hidden,
				Selected:     c.Selected,
			})
		}
		reply(w, http.StatusOK, out)
	})
	mux.HandleFunc("PUT /piles/{id}/selection", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathHex(w, r, "id", node.ErrUnknownPile)
		if !ok {
			return
		}
		var req struct{ From, To *int64 }
		if !decode(w, r, &req) {
			return
		}
		if req.From == nil || req.To == nil {
			reply(w, http.StatusBadRequest, problem("bad_request"))
			return
		}
		if err := n.Select(r.Context(), id, *req.From, *req.To); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /piles/{id}/selection/{asset}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathHex(w, r, "id", node.ErrUnknownPile)
		if !ok {
			return
		}
		a, ok := pathHex(w, r, "asset", node.ErrUnknownAsset)
		if !ok {
			return
		}
		var req struct{ Selected *bool }
		if !decode(w, r, &req) {
			return
		}
		if req.Selected == nil {
			reply(w, http.StatusBadRequest, problem("bad_request"))
			return
		}
		if err := n.Decide(r.Context(), id, a, *req.Selected); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func pathHex(w http.ResponseWriter, r *http.Request, name string, notFound error) ([]byte, bool) {
	b, err := hex.DecodeString(r.PathValue(name))
	if err != nil {
		fail(w, notFound)
		return nil, false
	}
	return b, true
}
