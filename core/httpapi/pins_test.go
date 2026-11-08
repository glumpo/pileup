package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/show"
)

type pinProgress struct {
	Author   string  `json:"author"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	Total    int     `json:"total"`
	Done     int     `json:"done"`
	Failed   int     `json:"failed"`
	Reason   *string `json:"reason"`
}

type estimate struct {
	Files      int    `json:"files"`
	Missing    int    `json:"missing"`
	Bytes      int64  `json:"estimated_bytes"`
	Budget     int64  `json:"budget_bytes"`
	Free       *int64 `json:"free_bytes"`
	OverBudget bool   `json:"over_budget"`
}

func (c client) pins(method, path string, body any) []pinProgress {
	c.t.Helper()
	var out []pinProgress
	c.do(method, path, body, http.StatusOK, &out)
	return out
}

func (c client) estimate(path string) estimate {
	c.t.Helper()
	var out estimate
	c.do("GET", path, nil, http.StatusOK, &out)
	return out
}

func TestPinEndpointsRequireToken(t *testing.T) {
	c, _, p, _ := hostIn(t, "android")
	for _, r := range []struct{ method, path string }{
		{"GET", "/piles/" + p.ID + "/pins"}, {"POST", "/piles/" + p.ID + "/pins"},
		{"DELETE", "/piles/" + p.ID + "/pins"}, {"GET", "/piles/" + p.ID + "/pins/estimate"},
	} {
		c.call(r.method, r.path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}

func TestPinLoadAndUnpinThroughAPI(t *testing.T) {
	c, _, p, items := hostIn(t, "android")
	pins := "/piles/" + p.ID + "/pins"
	me := items[0].Author
	if got := c.pins("GET", pins, nil); got == nil || len(got) != 0 {
		t.Fatalf("pins of a new pile: %+v", got)
	}
	est := c.estimate(pins + "/estimate?member=" + me)
	if est.Files != 3 || est.Missing != 3 || est.Bytes <= 0 || est.Budget != show.DefaultBudget || est.Free == nil || *est.Free <= 0 || est.OverBudget {
		t.Fatalf("estimate before loading: %+v", est)
	}
	if all := c.estimate(pins + "/estimate"); all.Files != est.Files || all.Missing != est.Missing || all.Bytes != est.Bytes {
		t.Fatalf("estimate of everything %+v, of the only member %+v", all, est)
	}

	got := c.pins("POST", pins, map[string]string{"member": me})
	if len(got) != 1 || got[0].Author != me || got[0].Name != "Аня" || got[0].Platform != "android" || got[0].Total != 3 {
		t.Fatalf("progress after pinning: %+v", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for got := c.pins("GET", pins, nil); len(got) != 1 || got[0].Done != 3 || got[0].Failed != 0 || got[0].Reason != nil; got = c.pins("GET", pins, nil) {
		if time.Now().After(deadline) {
			t.Fatalf("progress %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if est := c.estimate(pins + "/estimate?member=" + me); est.Files != 3 || est.Missing != 0 || est.Bytes != 0 {
		t.Fatalf("estimate after loading: %+v", est)
	}

	if got := c.pins("DELETE", pins+"?member="+me, nil); len(got) != 0 {
		t.Fatalf("progress after unpinning: %+v", got)
	}
	if got := c.pins("POST", pins, map[string]string{}); len(got) != 1 || got[0].Total != 3 || got[0].Done != 3 {
		t.Fatalf("progress after pinning everything: %+v", got)
	}
	if got := c.pins("DELETE", pins, nil); len(got) != 0 {
		t.Fatalf("progress after unpinning everything: %+v", got)
	}
}

func TestPinErrors(t *testing.T) {
	c, _, p, _ := hostIn(t, "android")
	pins := "/piles/" + p.ID + "/pins"
	stranger := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	for _, e := range []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{"GET", "/piles/00/pins", nil, http.StatusNotFound, "unknown_pile"},
		{"GET", "/piles/zz/pins", nil, http.StatusNotFound, "unknown_pile"},
		{"POST", "/piles/00/pins", map[string]string{}, http.StatusNotFound, "unknown_pile"},
		{"DELETE", "/piles/00/pins", nil, http.StatusNotFound, "unknown_pile"},
		{"GET", "/piles/00/pins/estimate", nil, http.StatusNotFound, "unknown_pile"},
		{"POST", pins, map[string]string{"member": stranger}, http.StatusNotFound, "unknown_member"},
		{"POST", pins, map[string]string{"member": "zz"}, http.StatusNotFound, "unknown_member"},
		{"DELETE", pins + "?member=" + stranger, nil, http.StatusNotFound, "unknown_member"},
		{"GET", pins + "/estimate?member=00", nil, http.StatusNotFound, "unknown_member"},
		{"POST", pins, "not an object", http.StatusBadRequest, "bad_request"},
	} {
		c.fails(e.method, e.path, e.body, e.status, e.code)
	}
}
