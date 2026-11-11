package httpapi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func WithNode(token string, n *node.Node, h *show.Host) http.Handler {
	mux := base()
	mux.HandleFunc("GET /piles", func(w http.ResponseWriter, r *http.Request) {
		ps, err := n.Piles()
		if err != nil {
			fail(w, err)
			return
		}
		out := make([]pile, 0, len(ps))
		for _, p := range ps {
			out = append(out, toJSON(n, p))
		}
		reply(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /piles", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, Member string }
		if !decode(w, r, &req) {
			return
		}
		p, err := n.Create(req.Name, req.Member)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusCreated, toJSON(n, p))
	})
	mux.HandleFunc("GET /piles/{id}/invite", func(w http.ResponseWriter, r *http.Request) {
		id, err := hex.DecodeString(r.PathValue("id"))
		if err != nil {
			fail(w, node.ErrUnknownPile)
			return
		}
		s, err := n.Invite(id)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]string{"invite": s})
	})
	mux.HandleFunc("POST /piles/join", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Invite, Member string }
		if !decode(w, r, &req) {
			return
		}
		p, err := n.Join(r.Context(), req.Invite, req.Member)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusCreated, toJSON(n, p))
	})
	media(mux, n)
	blobs(mux, n)
	timeline(mux, n)
	screens(mux, n)
	diagnostics(mux, n, token)
	reconnect(mux, n)
	shows(mux, h)
	pins(mux, h)
	return authorized(token, mux)
}

type member struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	Online     bool   `json:"online"`
	LastSeen   *int64 `json:"last_seen"`
	LastError  string `json:"last_error,omitempty"`
}

type pile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []member `json:"members"`
	Broken  string   `json:"broken,omitempty"`
}

func toJSON(n *node.Node, p node.Pile) pile {
	out := pile{ID: hex.EncodeToString(p.ID), Name: p.Name, Members: []member{}, Broken: p.Broken}
	authors := make([]op.Author, 0, len(p.Members))
	for a := range p.Members {
		authors = append(authors, a)
	}
	slices.SortFunc(authors, func(x, y op.Author) int { return bytes.Compare(x[:], y[:]) })
	for _, a := range authors {
		m, pr := p.Members[a], n.Presence(p.ID, a)
		var seen *int64
		if !pr.LastSeen.IsZero() {
			ms := pr.LastSeen.UnixMilli()
			seen = &ms
		}
		out.Members = append(out.Members, member{Key: hex.EncodeToString(a[:]), Name: m.Name, Platform: m.Platform, AppVersion: m.AppVersion, Online: pr.Online, LastSeen: seen, LastError: pr.LastError})
	}
	return out
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		reply(w, http.StatusBadRequest, problem("bad_request"))
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invite.ErrFormat):
		reply(w, http.StatusBadRequest, problem("bad_invite"))
	case errors.Is(err, invite.ErrVersion):
		reply(w, http.StatusBadRequest, problem("unknown_invite_version"))
	case errors.Is(err, node.ErrUnknownPile):
		reply(w, http.StatusNotFound, problem("unknown_pile"))
	case errors.Is(err, node.ErrBrokenPile):
		reply(w, http.StatusServiceUnavailable, problem("pile_unavailable"))
	case errors.Is(err, node.ErrJoined):
		reply(w, http.StatusConflict, problem("already_joined"))
	case errors.Is(err, node.ErrUnreachable):
		reply(w, http.StatusBadGateway, problem("inviter_unreachable"))
	case errors.Is(err, node.ErrOffline):
		reply(w, http.StatusServiceUnavailable, problem("offline"))
	case errors.Is(err, node.ErrUnknownAsset):
		reply(w, http.StatusNotFound, problem("unknown_asset"))
	case errors.Is(err, node.ErrNoMedia):
		reply(w, http.StatusNotImplemented, problem("no_media"))
	default:
		reply(w, http.StatusInternalServerError, problem("internal"))
	}
}

func problem(code string) map[string]string {
	return map[string]string{"error": code}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
