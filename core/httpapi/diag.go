package httpapi

import (
	"encoding/hex"
	"net/http"

	"github.com/glumpo/pileup/core/node"
)

type event struct {
	Time    int64  `json:"time"`
	Kind    string `json:"kind"`
	Pile    string `json:"pile,omitempty"`
	Peer    string `json:"peer,omitempty"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func diagnostics(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("GET /diag", func(w http.ResponseWriter, r *http.Request) {
		out := []event{}
		for _, e := range n.Diag().Events() {
			out = append(out, event{
				Time:    e.Time.UnixMilli(),
				Kind:    string(e.Kind),
				Pile:    hex.EncodeToString(e.Pile),
				Peer:    e.Peer,
				Message: e.Message,
				Count:   e.Count,
			})
		}
		reply(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /diag/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="pileup-diag.txt"`)
		n.Diag().Export(w)
	})
}
