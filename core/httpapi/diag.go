package httpapi

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/glumpo/pileup/core/diag"
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

var reported = []diag.Kind{diag.Discovery, diag.Connection, diag.Sync, diag.Timeout, diag.Renderer, diag.Media, diag.App}

func diagnostics(mux *http.ServeMux, n *node.Node, token string) {
	mux.HandleFunc("GET /diag", func(w http.ResponseWriter, r *http.Request) {
		names := memberNames(n)
		out := []event{}
		for _, e := range n.Diag().Events() {
			out = append(out, event{
				Time:    e.Time.UnixMilli(),
				Kind:    string(e.Kind),
				Pile:    hex.EncodeToString(e.Pile),
				Peer:    named(names, e.Peer),
				Message: e.Message,
				Count:   e.Count,
			})
		}
		reply(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /diag", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Kind, Message string }
		if !decode(w, r, &req) {
			return
		}
		if !slices.Contains(reported, diag.Kind(req.Kind)) || req.Message == "" {
			reply(w, http.StatusBadRequest, problem("bad_request"))
			return
		}
		n.Diag().Add(diag.Kind(req.Kind), nil, "", strings.ReplaceAll(req.Message, token, "…"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /diag/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="pileup-diag.txt"`)
		names := memberNames(n)
		fmt.Fprint(w, header(n))
		events := n.Diag().Events()
		slices.Reverse(events)
		for _, e := range events {
			e.Peer = named(names, e.Peer)
			fmt.Fprintln(w, e)
		}
	})
}

func named(names map[string]string, peer string) string {
	if name, ok := names[peer]; ok {
		return name
	}
	return peer
}

func memberNames(n *node.Node) map[string]string {
	out := map[string]string{}
	ps, _ := n.Piles()
	for _, p := range ps {
		for a, m := range p.Members {
			key := hex.EncodeToString(a[:])
			out[key] = m.Name + " (" + key[:8] + ")"
		}
	}
	return out
}

func header(n *node.Node) string {
	version, platform := n.Build()
	me := n.Author()
	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "Pileup %s %s; device %x; %s (UTC%s)\n", version, platform, me[:4], now.UTC().Format(time.RFC3339), now.Format("-07:00"))
	ps, err := n.Piles()
	if err != nil {
		fmt.Fprintf(&b, "piles: %v\n", err)
	}
	for _, p := range ps {
		var members []string
		for _, m := range toJSON(n, p).Members {
			s := fmt.Sprintf("%s (%s) %s %s", m.Name, m.Key[:8], m.Platform, m.AppVersion)
			switch {
			case m.Online:
				s += " online"
			case m.LastSeen != nil:
				s += " offline since " + time.UnixMilli(*m.LastSeen).UTC().Format(time.RFC3339)
			default:
				s += " never seen"
			}
			if m.LastError != "" {
				s += ", " + m.LastError
			}
			members = append(members, s)
		}
		fmt.Fprintf(&b, "pile «%s» %x: %s\n", p.Name, p.ID[:min(4, len(p.ID))], strings.Join(members, "; "))
	}
	return b.String() + "\n"
}
