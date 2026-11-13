package httpapi

import (
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type showItem struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Author   string `json:"author"`
	Kind     string `json:"kind"`
	TakenAt  int64  `json:"taken_at"`
	Codec    string `json:"codec,omitempty"`
}

type rendererError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type hint struct {
	Author    string `json:"author"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	Reason    string `json:"reason"`
	LastError string `json:"last_error,omitempty"`
}

type showState struct {
	Pile             string         `json:"pile"`
	Screen           screen         `json:"screen"`
	Index            int            `json:"index"`
	Total            int            `json:"total"`
	Current          showItem       `json:"current"`
	Playing          bool           `json:"playing"`
	VideoUnavailable bool           `json:"video_unavailable"`
	VideoReason      string         `json:"video_reason,omitempty"`
	Buffering        bool           `json:"buffering"`
	Stalled          bool           `json:"stalled"`
	NeedBitrate      int64          `json:"need_bps,omitempty"`
	Bitrate          int64          `json:"rate_bps,omitempty"`
	RendererError    *rendererError `json:"renderer_error,omitempty"`
	Waiting          []showItem     `json:"waiting"`
	Hints            []hint         `json:"hints"`
	Slide            *slide         `json:"slide,omitempty"`
}

type slide struct {
	Image  string   `json:"image"`
	Video  string   `json:"video,omitempty"`
	Around []string `json:"around"`
}

func toHint(h show.Hint) hint {
	return hint{Author: hex.EncodeToString(h.Member[:]), Name: h.Name, Platform: h.Platform, Reason: h.Reason, LastError: h.LastError}
}

func toShowItem(a state.Asset) showItem {
	return showItem{
		ID:       hex.EncodeToString(a.Meta.AssetID),
		Revision: hex.EncodeToString(a.Meta.Revision),
		Author:   hex.EncodeToString(a.Owner[:]),
		Kind:     kindNames[a.Meta.Kind],
		TakenAt:  a.Meta.TakenAt,
		Codec:    a.Meta.Codec,
	}
}

func toShowState(st show.State) showState {
	s := st.Screen
	out := showState{
		Pile:             hex.EncodeToString(st.Pile),
		Screen:           screen{ID: s.ID, Name: s.Name, Kind: s.Kind, Address: s.Addr.String(), Available: s.Available, Reason: s.Reason},
		Index:            st.Index,
		Total:            st.Total,
		Current:          toShowItem(st.Current),
		Playing:          st.Playing,
		VideoUnavailable: st.VideoUnavailable,
		VideoReason:      st.VideoReason,
		Buffering:        st.Buffering,
		Stalled:          st.Stalled,
		NeedBitrate:      int64(st.NeedBitrate),
		Bitrate:          int64(st.Bitrate),
		Waiting:          []showItem{},
		Hints:            []hint{},
	}
	if st.Error != nil {
		out.RendererError = &rendererError{Code: st.Error.Code, Message: st.Error.Message}
	}
	for _, a := range st.Waiting {
		out.Waiting = append(out.Waiting, toShowItem(a))
	}
	for _, h := range st.Hints {
		out.Hints = append(out.Hints, toHint(h))
	}
	if sl := st.Slide; sl != nil {
		out.Slide = &slide{Image: sl.Image, Video: sl.Video, Around: append([]string{}, sl.Around...)}
	}
	return out
}

func shows(mux *http.ServeMux, h *show.Host) {
	mux.HandleFunc("POST /show", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Pile, Screen, Asset string }
		if !decode(w, r, &req) {
			return
		}
		pile, err := hex.DecodeString(req.Pile)
		if err != nil {
			fail(w, r, node.ErrUnknownPile)
			return
		}
		var asset []byte
		if req.Asset != "" {
			if asset, err = hex.DecodeString(req.Asset); err != nil {
				fail(w, r, node.ErrUnknownAsset)
				return
			}
		}
		st, err := h.Start(pile, req.Screen, asset)
		showReply(w, r, http.StatusCreated, st, err)
	})
	mux.HandleFunc("GET /show", func(w http.ResponseWriter, r *http.Request) {
		st, err := h.State()
		showReply(w, r, http.StatusOK, st, err)
	})
	mux.HandleFunc("DELETE /show", func(w http.ResponseWriter, r *http.Request) {
		h.Stop()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /show/interrupted", func(w http.ResponseWriter, r *http.Request) {
		in, err := h.Interrupted()
		if err != nil {
			showReply(w, r, http.StatusOK, show.State{}, err)
			return
		}
		s := in.Screen
		reply(w, http.StatusOK, map[string]any{
			"pile":   hex.EncodeToString(in.Pile),
			"screen": screen{ID: s.ID, Name: s.Name, Kind: s.Kind, Address: s.Addr.String(), Available: s.Available},
			"asset":  hex.EncodeToString(in.Asset),
		})
	})
	mux.HandleFunc("DELETE /show/interrupted", func(w http.ResponseWriter, r *http.Request) {
		h.Forget()
		w.WriteHeader(http.StatusNoContent)
	})
	for path, f := range map[string]func() (show.State, error){
		"/show/next":  h.Next,
		"/show/prev":  h.Prev,
		"/show/play":  h.Play,
		"/show/pause": h.Pause,
	} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			st, err := f()
			showReply(w, r, http.StatusOK, st, err)
		})
	}
	mux.HandleFunc("POST /show/player", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Asset, State, Message string }
		if !decode(w, r, &req) {
			return
		}
		asset, err := hex.DecodeString(req.Asset)
		if err != nil {
			fail(w, r, node.ErrUnknownAsset)
			return
		}
		st, err := h.Player(asset, req.State, req.Message)
		showReply(w, r, http.StatusOK, st, err)
	})
	mux.HandleFunc("POST /show/goto", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Asset string }
		if !decode(w, r, &req) {
			return
		}
		asset, err := hex.DecodeString(req.Asset)
		if err != nil {
			fail(w, r, node.ErrUnknownAsset)
			return
		}
		st, err := h.Goto(asset)
		showReply(w, r, http.StatusOK, st, err)
	})
}

func showReply(w http.ResponseWriter, r *http.Request, status int, st show.State, err error) {
	var unavailable *show.UnavailableError
	switch {
	case err == nil:
		reply(w, status, toShowState(st))
	case errors.As(err, &unavailable):
		reply(w, http.StatusConflict, map[string]string{"error": "screen_unavailable", "reason": unavailable.Reason})
	case errors.Is(err, show.ErrNoShow):
		reply(w, http.StatusNotFound, problem("no_show"))
	case errors.Is(err, show.ErrUnknownScreen):
		reply(w, http.StatusNotFound, problem("unknown_screen"))
	case errors.Is(err, show.ErrEmpty):
		reply(w, http.StatusConflict, problem("empty_pile"))
	case errors.Is(err, show.ErrNotVideo):
		reply(w, http.StatusConflict, problem("not_video"))
	case errors.Is(err, show.ErrUnreachable):
		reply(w, http.StatusBadGateway, problem("screen_unreachable"))
	case errors.Is(err, show.ErrPlayerState):
		reply(w, http.StatusBadRequest, problem("bad_request"))
	default:
		fail(w, r, err)
	}
}
