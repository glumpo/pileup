package httpapi

import (
	"net/http"

	"github.com/glumpo/pileup/core/node"
)

type screen struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Address   string `json:"address"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func screens(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("GET /screens", func(w http.ResponseWriter, r *http.Request) {
		out := []screen{}
		for _, s := range n.Screens() {
			out = append(out, screen{ID: s.ID, Name: s.Name, Kind: s.Kind, Address: s.Addr.String(), Available: s.Available, Reason: s.Reason})
		}
		reply(w, http.StatusOK, out)
	})
}
