package httpapi_test

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type item struct {
	ID          string  `json:"id"`
	Revision    string  `json:"revision"`
	Author      string  `json:"author"`
	AuthorName  string  `json:"author_name"`
	Kind        string  `json:"kind"`
	TakenAt     int64   `json:"taken_at"`
	LocalOffset *int32  `json:"local_offset"`
	DurationMs  *uint64 `json:"duration_ms"`
	Width       uint32  `json:"width"`
	Height      uint32  `json:"height"`
	Thumbhash   *string `json:"thumbhash"`
}

const (
	minute = 60_000
	ten    = july1 + 10*60*minute
)

const anya library = `[
	{"local_id": "a1", "kind": "photo", "platform_time": 1782900000000, "width": 4032, "height": 3024,
	 "size_bytes": 1, "mime": "image/heic", "original_name": "a1.HEIC", "modified": 1},
	{"local_id": "a2", "kind": "video", "platform_time": 1782900120000, "width": 1920, "height": 1080,
	 "duration_ms": 15000, "size_bytes": 1, "mime": "video/mp4", "original_name": "a2.MP4", "modified": 1},
	{"local_id": "a3", "kind": "photo", "exif_date_time": "2026:07:01 14:05:00", "exif_offset": "+04:00",
	 "width": 3000, "height": 2000, "size_bytes": 1, "mime": "image/jpeg", "original_name": "a3.JPG", "modified": 1}
]`

const borya library = `[
	{"local_id": "b1", "kind": "video", "platform_time": 1782900060000, "width": 1080, "height": 1920,
	 "duration_ms": 4000, "size_bytes": 1, "mime": "video/mp4", "original_name": "b1.MP4", "modified": 1},
	{"local_id": "b2", "kind": "photo", "platform_time": 1782900180000, "width": 4000, "height": 3000,
	 "size_bytes": 1, "mime": "image/jpeg", "original_name": "b2.JPG", "modified": 1},
	{"local_id": "b3", "kind": "live_photo", "platform_time": 1782900300000, "width": 4000, "height": 3000,
	 "size_bytes": 1, "mime": "image/heic", "original_name": "b3.HEIC", "modified": 1},
	{"local_id": "b4", "kind": "photo", "platform_time": 1782900420000, "width": 4000, "height": 3000,
	 "size_bytes": 1, "mime": "image/jpeg", "original_name": "b4.JPG", "modified": 1},
	{"local_id": "b5", "kind": "photo", "platform_time": 1782900420000, "width": 4000, "height": 3000,
	 "size_bytes": 1, "mime": "image/jpeg", "original_name": "b5.JPG", "modified": 1},
	{"local_id": "b6", "kind": "photo", "platform_time": 1782900420000, "width": 4000, "height": 3000,
	 "size_bytes": 1, "mime": "image/jpeg", "original_name": "b6.JPG", "modified": 1},
	{"local_id": "b7", "kind": "video", "platform_time": 1782900420000, "width": 4000, "height": 3000,
	 "duration_ms": 1000, "size_bytes": 1, "mime": "video/mp4", "original_name": "b7.MP4", "modified": 1}
]`

func (c client) timeline(p pile) []item {
	c.t.Helper()
	var out []item
	c.do("GET", "/piles/"+p.ID+"/timeline", nil, http.StatusOK, &out)
	return out
}

func (c client) localIDs(p pile) map[string]string {
	c.t.Helper()
	out := map[string]string{}
	for _, a := range c.selection(p).Assets {
		out[a.ID] = a.LocalID
	}
	return out
}

func TestTimelineEndpointRequiresToken(t *testing.T) {
	a := startWith(t, anya)
	p := a.create("Грузия", "Аня")
	a.call("GET", "/piles/"+p.ID+"/timeline", "wrong", nil, http.StatusUnauthorized, nil)
}

func TestTimelineOfUnknownPile(t *testing.T) {
	a := startWith(t, anya)
	for _, id := range []string{"00112233445566778899aabbccddeeff", "zz"} {
		a.fails("GET", "/piles/"+id+"/timeline", nil, http.StatusNotFound, "unknown_pile")
	}
}

func TestBothMembersSeeOneTimelineInTheSameOrder(t *testing.T) {
	a, b := startWith(t, anya), startWith(t, borya)
	p := a.create("Грузия", "Аня")
	b.do("POST", "/piles/join", map[string]string{"invite": a.invite(p), "member": "Боря"}, http.StatusCreated, nil)
	for _, c := range []client{a, b} {
		c.do("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusNoContent, nil)
	}

	var ta, tb []item
	deadline := time.Now().Add(10 * time.Second)
	for {
		ta, tb = a.timeline(p), b.timeline(p)
		if len(ta) == 10 && reflect.DeepEqual(ta, tb) && ta[0].AuthorName != "" && tb[len(tb)-1].AuthorName != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timelines did not converge:\n%+v\n%+v", ta, tb)
		}
		time.Sleep(10 * time.Millisecond)
	}

	local := a.localIDs(p)
	for id, l := range b.localIDs(p) {
		local[id] = l
	}
	var anyaKey, boryaKey string
	byLocal := map[string]item{}
	order := make([]string, 0, len(ta))
	for _, it := range ta {
		l := local[it.ID]
		byLocal[l] = it
		order = append(order, l)
		switch l[0] {
		case 'a':
			anyaKey = it.Author
			if it.AuthorName != "Аня" {
				t.Fatalf("%s author %q", l, it.AuthorName)
			}
		case 'b':
			boryaKey = it.Author
			if it.AuthorName != "Боря" {
				t.Fatalf("%s author %q", l, it.AuthorName)
			}
		}
	}

	want := []string{"a1", "b1", "a2", "b2"}
	if anyaKey < boryaKey {
		want = append(want, "a3", "b3")
	} else {
		want = append(want, "b3", "a3")
	}
	same := []string{"b4", "b5", "b6", "b7"}
	slices.SortFunc(same, func(x, y string) int { return strings.Compare(byLocal[x].ID, byLocal[y].ID) })
	want = append(want, same...)
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}

	if v := byLocal["a2"]; v.Kind != "video" || v.TakenAt != ten+2*minute || v.DurationMs == nil || *v.DurationMs != 15000 ||
		v.Width != 1920 || v.Height != 1080 || v.Revision == "" || v.LocalOffset != nil || v.Thumbhash == nil {
		t.Fatalf("video a2 %+v", v)
	}
	if e := byLocal["a3"]; e.Kind != "photo" || e.TakenAt != ten+5*minute || e.LocalOffset == nil || *e.LocalOffset != 4*3600 ||
		e.DurationMs != nil {
		t.Fatalf("exif photo a3 %+v", e)
	}
	if k := byLocal["b3"].Kind; k != "live_photo" {
		t.Fatalf("live photo kind %q", k)
	}
}
