package httpapi

import (
	"net/http"

	"github.com/glumpo/pileup/core/node"
)

func reconnect(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("POST /reconnect", func(w http.ResponseWriter, r *http.Request) {
		n.Reconnect("requested by the app")
		w.WriteHeader(http.StatusNoContent)
	})
}
