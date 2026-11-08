package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/blob"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/peer"
)

const (
	T1 = 1
	T2 = 2
	T3 = 3
)

const (
	Immutable     = "max-age=31536000, immutable"
	fetchers      = 5
	originalChunk = 1 << 20
)

var (
	ErrUnavailable = errors.New("node: asset owner is unavailable")
	errStatus      = errors.New("node: owner refused")
	errNoOwner     = errors.New("node: owner is not among known peers")
)

type RenditionMaker interface {
	Rendition(localID string, tier int) ([]byte, error)
	OriginalSize(localID string) (int64, error)
	ReadOriginal(localID string, offset, length int64) ([]byte, error)
}

func ETag(h blob.Hash) string {
	return `"` + h.String() + `"`
}

func (n *Node) Blob(ctx context.Context, pileID, assetID, revision []byte, tier int) (*os.File, blob.Hash, error) {
	p, err := n.find(pileID)
	if err != nil {
		return nil, blob.Hash{}, err
	}
	if tier != T1 && tier != T2 {
		return nil, blob.Hash{}, ErrUnknownAsset
	}
	return n.open(ctx, p, assetID, revision, tier)
}

func (n *Node) open(ctx context.Context, p *pile, assetID, revision []byte, tier int) (*os.File, blob.Hash, error) {
	h, err := n.blob(ctx, p, assetID, revision, tier)
	if err != nil {
		return nil, blob.Hash{}, err
	}
	f, err := n.blobs.Open(h)
	return f, h, err
}

func ref(owner op.Author, assetID, revision []byte, tier int) string {
	return fmt.Sprintf("%x/%x/%x/%d", owner[:], assetID, revision, tier)
}

func (p *pile) asset(assetID, revision []byte) (state.Asset, error) {
	a, ok, err := p.store.Asset(assetID)
	if err == nil && (!ok || !bytes.Equal(a.Meta.Revision, revision)) {
		err = ErrUnknownAsset
	}
	return a, err
}

func (n *Node) blob(ctx context.Context, p *pile, assetID, revision []byte, tier int) (blob.Hash, error) {
	a, err := p.asset(assetID, revision)
	if err != nil {
		return blob.Hash{}, err
	}
	key := ref(a.Owner, assetID, revision, tier)
	if h, ok, err := n.stored(key); ok || err != nil {
		return h, err
	}
	var h blob.Hash
	if a.Owner == n.Author() {
		n.rendering.Lock()
		defer n.rendering.Unlock()
		if h, ok, err := n.stored(key); ok || err != nil {
			return h, err
		}
		h, err = n.render(p, assetID, tier)
		n.report(diag.Media, p.ID, "", fmt.Sprintf("render %x T%d", assetID, tier), err)
	} else {
		h, err = n.fetch(ctx, p, a.Owner, assetID, revision, tier)
		n.report(diag.Media, p.ID, hex.EncodeToString(a.Owner[:]), fmt.Sprintf("fetch %x T%d", assetID, tier), err)
	}
	if err == nil {
		err = n.blobs.SetRef(key, h)
	}
	return h, err
}

func (n *Node) stored(key string) (blob.Hash, bool, error) {
	h, ok, err := n.blobs.Ref(key)
	if ok && err == nil {
		ok, err = n.blobs.Has(h)
	}
	return h, ok, err
}

func (n *Node) refOf(pileID, assetID, revision []byte, tier int) (string, error) {
	p, err := n.find(pileID)
	if err != nil {
		return "", err
	}
	a, err := p.asset(assetID, revision)
	if err != nil {
		return "", err
	}
	return ref(a.Owner, assetID, revision, tier), nil
}

func (n *Node) Cached(pileID, assetID, revision []byte, tier int) (int64, bool) {
	key, err := n.refOf(pileID, assetID, revision, tier)
	if err != nil {
		return 0, false
	}
	h, ok, err := n.blobs.Ref(key)
	if !ok || err != nil {
		return 0, false
	}
	f, err := n.blobs.Open(h)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

func (n *Node) Evict(pileID, assetID, revision []byte, tier int) error {
	key, err := n.refOf(pileID, assetID, revision, tier)
	if err != nil {
		return err
	}
	h, ok, err := n.blobs.Ref(key)
	if !ok || err != nil {
		return err
	}
	return errors.Join(n.blobs.RemoveRef(key), n.blobs.Remove(h))
}

func (n *Node) localID(p *pile, assetID []byte) (string, error) {
	n.mu.Lock()
	local, ok := p.local[hex.EncodeToString(assetID)]
	n.mu.Unlock()
	if !ok {
		return "", ErrUnknownAsset
	}
	if n.cfg.Renditions == nil {
		return "", ErrUnavailable
	}
	return local, nil
}

func (n *Node) render(p *pile, assetID []byte, tier int) (blob.Hash, error) {
	local, err := n.localID(p, assetID)
	if err != nil {
		return blob.Hash{}, err
	}
	b, err := n.cfg.Renditions.Rendition(local, tier)
	if err != nil {
		return blob.Hash{}, err
	}
	return n.blobs.Put(bytes.NewReader(b))
}

func (n *Node) fetch(ctx context.Context, p *pile, owner op.Author, assetID, revision []byte, tier int) (blob.Hash, error) {
	u := fmt.Sprintf("http://%x/blob/%x/%x/%d", owner[:], assetID, revision, tier)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return blob.Hash{}, err
	}
	resp, err := p.owners.Do(req)
	if err != nil {
		return blob.Hash{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return n.blobs.Put(resp.Body)
	case http.StatusNotFound:
		return blob.Hash{}, ErrUnknownAsset
	}
	return blob.Hash{}, fmt.Errorf("%w: %w: %s", ErrUnavailable, errStatus, resp.Status)
}

func (n *Node) owners(p *pile) *http.Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		return n.dialOwner(ctx, p, addr)
	}}
	context.AfterFunc(n.ctx, tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: timeout}
}

func (n *Node) dialOwner(ctx context.Context, p *pile, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	want, err := hex.DecodeString(host)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	peers := slices.Clone(p.Peers)
	n.mu.Unlock()
	errs := []error{errNoOwner}
	for _, a := range peers {
		dctx, cancel := context.WithTimeout(ctx, dialTimeout)
		c, err := peer.Dial(dctx, a, n.key, p.ID, p.Secret)
		cancel()
		if err == nil && bytes.Equal(c.Peer(), want) {
			return c, nil
		}
		if err == nil {
			c.Close()
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func (n *Node) serveBlob(p *pile, w http.ResponseWriter, r *http.Request) {
	id, ierr := hex.DecodeString(r.PathValue("asset"))
	rev, rerr := hex.DecodeString(r.PathValue("revision"))
	tier, terr := strconv.Atoi(r.PathValue("tier"))
	a, err := p.asset(id, rev)
	if errors.Join(ierr, rerr, terr) != nil || tier < T1 || tier > T3 || err == nil && a.Owner != n.Author() {
		err = ErrUnknownAsset
	}
	var content io.ReadSeeker
	if err == nil && tier == T3 {
		if content, err = n.original(p, id); err == nil {
			w.Header().Set("ETag", `"`+hex.EncodeToString(rev)+`"`)
			w.Header().Set("Content-Type", a.Meta.MIME)
		}
		n.report(diag.Media, p.ID, "", fmt.Sprintf("original %x", id), err)
	} else if err == nil {
		var f *os.File
		var h blob.Hash
		if f, h, err = n.open(r.Context(), p, id, rev, tier); err == nil {
			defer f.Close()
			content = f
			w.Header().Set("ETag", ETag(h))
		}
	}
	switch {
	case errors.Is(err, ErrUnknownAsset):
		http.NotFound(w, r)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		w.Header().Set("Cache-Control", Immutable)
		http.ServeContent(w, r, "", time.Time{}, content)
	}
}

func (n *Node) ServeOriginal(w http.ResponseWriter, r *http.Request, pileID, assetID, revision []byte) {
	p, err := n.find(pileID)
	var a state.Asset
	if err == nil {
		a, err = p.asset(assetID, revision)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.Owner != n.Author() {
		n.relayOriginal(p, a, w, r)
		return
	}
	content, err := n.original(p, assetID)
	n.report(diag.Media, p.ID, "", fmt.Sprintf("original %x", assetID), err)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("ETag", `"`+hex.EncodeToString(revision)+`"`)
	w.Header().Set("Content-Type", a.Meta.MIME)
	w.Header().Set("Cache-Control", Immutable)
	http.ServeContent(w, r, "", time.Time{}, content)
}

func (n *Node) relayOriginal(p *pile, a state.Asset, w http.ResponseWriter, r *http.Request) {
	u := fmt.Sprintf("http://%x/blob/%x/%x/%d", a.Owner[:], a.Meta.AssetID, a.Meta.Revision, T3)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, h := range []string{"Range", "If-Range", "If-None-Match"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := p.stream.Do(req)
	n.report(diag.Media, p.ID, hex.EncodeToString(a.Owner[:]), fmt.Sprintf("stream %x", a.Meta.AssetID), err)
	if err != nil {
		http.Error(w, ErrUnavailable.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Accept-Ranges", "Cache-Control", "Content-Length", "Content-Range", "Content-Type", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (n *Node) original(p *pile, assetID []byte) (io.ReadSeeker, error) {
	local, err := n.localID(p, assetID)
	if err != nil {
		return nil, err
	}
	size, err := n.cfg.Renditions.OriginalSize(local)
	if err != nil {
		return nil, err
	}
	return &original{maker: n.cfg.Renditions, id: local, size: size}, nil
}

type original struct {
	maker     RenditionMaker
	id        string
	size, pos int64
	buf       []byte
}

func (o *original) Read(b []byte) (int, error) {
	if len(o.buf) == 0 {
		if o.pos >= o.size {
			return 0, io.EOF
		}
		chunk, err := o.maker.ReadOriginal(o.id, o.pos, min(originalChunk, o.size-o.pos))
		if err != nil {
			return 0, err
		}
		if len(chunk) == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		o.buf = chunk
	}
	k := copy(b, o.buf)
	o.buf = o.buf[k:]
	o.pos += int64(k)
	return k, nil
}

func (o *original) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += o.pos
	case io.SeekEnd:
		offset += o.size
	}
	o.pos, o.buf = offset, nil
	return offset, nil
}

func (n *Node) thumbnails(p *pile) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			changed := p.replica.Changed()
			var again <-chan time.Time
			if !n.fetchThumbnails(p) {
				again = time.After(retry)
			}
			select {
			case <-n.ctx.Done():
				return
			case <-changed:
			case <-again:
			}
		}
	}()
}

func (n *Node) fetchThumbnails(p *pile) bool {
	v, err := p.store.View()
	if err != nil {
		return false
	}
	var mu sync.Mutex
	done, offline := true, map[op.Author]bool{}
	todo := make(chan state.Asset)
	var wg sync.WaitGroup
	for range fetchers {
		wg.Go(func() {
			for a := range todo {
				mu.Lock()
				skip := offline[a.Owner]
				mu.Unlock()
				if skip {
					continue
				}
				_, err := n.blob(n.ctx, p, a.Meta.AssetID, a.Meta.Revision, T1)
				mu.Lock()
				if err != nil {
					done = false
				}
				if errors.Is(err, ErrUnavailable) && !errors.Is(err, errStatus) {
					offline[a.Owner] = true
				}
				mu.Unlock()
			}
		})
	}
	for _, a := range v.Assets {
		if a.Owner != n.Author() {
			todo <- a
		}
	}
	close(todo)
	wg.Wait()
	return done
}
