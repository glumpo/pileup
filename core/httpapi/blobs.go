package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/glumpo/pileup/core/node"
)

func blobs(mux *http.ServeMux, n *node.Node) {
	mux.HandleFunc("GET /piles/{id}/blob/{asset}/{revision}/{tier}", func(w http.ResponseWriter, r *http.Request) {
		id, err := hex.DecodeString(r.PathValue("id"))
		if err != nil {
			fail(w, node.ErrUnknownPile)
			return
		}
		asset, aerr := hex.DecodeString(r.PathValue("asset"))
		rev, rerr := hex.DecodeString(r.PathValue("revision"))
		tier, terr := strconv.Atoi(r.PathValue("tier"))
		if errors.Join(aerr, rerr, terr) != nil {
			reply(w, http.StatusNotFound, problem("unknown_asset"))
			return
		}
		f, h, err := n.Blob(r.Context(), id, asset, rev, tier)
		switch {
		case errors.Is(err, node.ErrUnknownAsset):
			reply(w, http.StatusNotFound, problem("unknown_asset"))
		case errors.Is(err, node.ErrUnavailable):
			reply(w, http.StatusBadGateway, problem("source_unavailable"))
		case err != nil:
			fail(w, err)
		default:
			defer f.Close()
			w.Header().Set("ETag", node.ETag(h))
			w.Header().Set("Cache-Control", node.Immutable)
			http.ServeContent(w, r, "", time.Time{}, f)
		}
	})
}
