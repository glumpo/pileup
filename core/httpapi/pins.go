package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type pinProgress struct {
	Author   string `json:"author"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Failed   int    `json:"failed"`
	Reason   string `json:"reason,omitempty"`
}

type estimate struct {
	Files      int    `json:"files"`
	Missing    int    `json:"missing"`
	Bytes      int64  `json:"estimated_bytes"`
	Budget     int64  `json:"budget_bytes"`
	Free       *int64 `json:"free_bytes"`
	OverBudget bool   `json:"over_budget"`
}

func pins(mux *http.ServeMux, h *show.Host) {
	mux.HandleFunc("GET /piles/{id}/pins", func(w http.ResponseWriter, r *http.Request) {
		id, err := hex.DecodeString(r.PathValue("id"))
		if err != nil {
			fail(w, node.ErrUnknownPile)
			return
		}
		list, err := h.Pins(id)
		progressReply(w, list, err)
	})
	mux.HandleFunc("POST /piles/{id}/pins", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Member string }
		if !decode(w, r, &req) {
			return
		}
		pinned(w, r.PathValue("id"), req.Member, h.Pin)
	})
	mux.HandleFunc("DELETE /piles/{id}/pins", func(w http.ResponseWriter, r *http.Request) {
		pinned(w, r.PathValue("id"), r.URL.Query().Get("member"), h.Unpin)
	})
	mux.HandleFunc("GET /piles/{id}/pins/estimate", func(w http.ResponseWriter, r *http.Request) {
		id, member, err := pileMember(r.PathValue("id"), r.URL.Query().Get("member"))
		var e show.Estimate
		if err == nil {
			e, err = h.Estimate(id, member)
		}
		if err != nil {
			pinFail(w, err)
			return
		}
		reply(w, http.StatusOK, estimate{Files: e.Files, Missing: e.Missing, Bytes: e.Bytes, Budget: e.Budget, Free: e.Free, OverBudget: e.OverBudget})
	})
}

func pileMember(pile, member string) ([]byte, []byte, error) {
	id, err := hex.DecodeString(pile)
	if err != nil {
		return nil, nil, node.ErrUnknownPile
	}
	if member == "" {
		return id, nil, nil
	}
	m, err := hex.DecodeString(member)
	if err != nil {
		return nil, nil, show.ErrUnknownMember
	}
	return id, m, nil
}

func pinned(w http.ResponseWriter, pile, member string, f func(pileID, member []byte) ([]show.Progress, error)) {
	id, m, err := pileMember(pile, member)
	var list []show.Progress
	if err == nil {
		list, err = f(id, m)
	}
	progressReply(w, list, err)
}

func progressReply(w http.ResponseWriter, list []show.Progress, err error) {
	if err != nil {
		pinFail(w, err)
		return
	}
	out := []pinProgress{}
	for _, p := range list {
		out = append(out, pinProgress{
			Author:   hex.EncodeToString(p.Member[:]),
			Name:     p.Name,
			Platform: p.Platform,
			Total:    p.Total,
			Done:     p.Done,
			Failed:   p.Failed,
			Reason:   p.Reason,
		})
	}
	reply(w, http.StatusOK, out)
}

func pinFail(w http.ResponseWriter, err error) {
	if errors.Is(err, show.ErrUnknownMember) {
		reply(w, http.StatusNotFound, problem("unknown_member"))
		return
	}
	fail(w, err)
}
