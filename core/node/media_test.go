package node_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/store"
)

type library struct {
	mu    sync.Mutex
	items []any
	scans int
}

func (l *library) Media(from, to int64) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scans++
	return json.Marshal(l.items)
}

func (l *library) scanned() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scans
}

func (l *library) set(items ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = items
}

func shelf(items ...any) *library {
	l := &library{}
	l.set(items...)
	return l
}

func ms(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

func at(s string) *int64 {
	v := ms(s)
	return &v
}

func photo(localID, when string) node.MediaItem {
	return node.MediaItem{
		LocalID:      localID,
		Kind:         "photo",
		PlatformTime: at(when),
		Width:        4032,
		Height:       3024,
		SizeBytes:    2_500_000,
		MIME:         "image/heic",
		OriginalName: "IMG_" + localID + ".HEIC",
		Modified:     ms(when),
	}
}

func video(localID, when string, duration uint64) node.MediaItem {
	v := photo(localID, when)
	v.Kind = "video"
	v.Width, v.Height = 1920, 1080
	v.DurationMs = &duration
	v.SizeBytes = 80_000_000
	v.MIME = "video/mp4"
	v.Codec = "hvc1"
	v.OriginalName = "VID_" + localID + ".MP4"
	return v
}

func openWith(t *testing.T, dir, platform string, lib node.MediaProvider) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: platform,
		Addrs:    loopback,
		Media:    lib,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func choose(t *testing.T, n *node.Node, id []byte, from, to string) {
	t.Helper()
	if err := n.Select(context.Background(), id, ms(from), ms(to)); err != nil {
		t.Fatal(err)
	}
}

func decide(t *testing.T, n *node.Node, id, asset []byte, selected bool) {
	t.Helper()
	if err := n.Decide(context.Background(), id, asset, selected); err != nil {
		t.Fatal(err)
	}
}

func selection(t *testing.T, n *node.Node, id []byte) node.Selection {
	t.Helper()
	s, err := n.Selection(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assetIDs(t *testing.T, n *node.Node, id []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, c := range selection(t, n, id).Items {
		out[c.LocalID] = c.Meta.AssetID
	}
	return out
}

func assetsOn(t *testing.T, n *node.Node, id []byte) map[string]state.Asset {
	t.Helper()
	as, err := n.Assets(id)
	if err != nil {
		t.Fatal(err)
	}
	return as
}

func keys(m map[string]state.Asset) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func idSet(ids ...[]byte) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	sort.Strings(out)
	return out
}

func waitAssets(t *testing.T, id []byte, want []string, nodes ...*node.Node) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		same := true
		for _, n := range nodes {
			if !slices.Equal(keys(assetsOn(t, n, id)), want) {
				same = false
			}
		}
		if same {
			return
		}
		if time.Now().After(deadline) {
			for i, n := range nodes {
				t.Logf("node %d: %d assets", i, len(assetsOn(t, n, id)))
			}
			t.Fatalf("assets did not converge to %d", len(want))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func pair(t *testing.T, lib node.MediaProvider) (*node.Node, *node.Node, node.Pile) {
	t.Helper()
	a := openWith(t, t.TempDir(), "ios", lib)
	b := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	return a, b, p
}

func logOf(t *testing.T, dir string, p node.Pile, secret []byte) [][]byte {
	t.Helper()
	s, err := store.Open(filepath.Join(dir, "piles", hex.EncodeToString(p.ID)+".db"), p.ID, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.VV()
	if err != nil {
		t.Fatal(err)
	}
	var raws [][]byte
	for a := range v {
		ops, err := s.Ops(a, 1)
		if err != nil {
			t.Fatal(err)
		}
		raws = append(raws, ops...)
	}
	return raws
}

func changesBy(t *testing.T, raws [][]byte, a op.Author) []op.Change {
	t.Helper()
	var out []op.Change
	for _, raw := range raws {
		o, err := op.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if o.Author == a && (o.Type == op.TypeAssetAdd || o.Type == op.TypeAssetRemove) {
			out = append(out, o.Change)
		}
	}
	return out
}

func secretOf(t *testing.T, n *node.Node, p node.Pile) []byte {
	t.Helper()
	i, err := invite.Parse(inviteTo(t, n, p))
	if err != nil {
		t.Fatal(err)
	}
	return i.Secret
}

func TestSelectedRangePublishesItsPhotosAndVideosToMembers(t *testing.T) {
	exif := node.MediaItem{
		LocalID:      "exif",
		Kind:         "photo",
		ExifDateTime: "2026:07:05 14:00:00",
		ExifOffset:   "+04:00",
		Width:        3000,
		Height:       2000,
		SizeBytes:    3_000_000,
		MIME:         "image/jpeg",
		OriginalName: "DSC_0001.JPG",
	}
	live := photo("live", "2026-07-06T09:00:00Z")
	live.Kind = "live_photo"
	audio := photo("audio", "2026-07-06T10:00:00Z")
	audio.Kind = "audio"
	lib := shelf(
		photo("before", "2026-06-30T23:59:59.999Z"),
		photo("from", "2026-07-01T00:00:00Z"),
		video("video", "2026-07-03T12:00:00Z", 15_000),
		exif,
		live,
		audio,
		photo("to", "2026-07-10T00:00:00Z"),
	)
	a, b, p := pair(t, lib)
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")

	ids := assetIDs(t, a, p.ID)
	want := idSet(ids["from"], ids["video"], ids["exif"], ids["live"])
	if len(ids) != 4 || len(want) != 4 {
		t.Fatalf("selection lists %v", ids)
	}
	waitAssets(t, p.ID, want, a, b)

	got := assetsOn(t, b, p.ID)
	for _, as := range got {
		if as.Owner != a.Author() {
			t.Fatalf("owner %x, want %x", as.Owner, a.Author())
		}
	}
	v := got[string(ids["video"])].Meta
	if v.Kind != op.KindVideo || v.TakenAt != ms("2026-07-03T12:00:00Z") || v.TakenAtSource != op.SourcePlatform ||
		v.Width != 1920 || v.Height != 1080 || v.DurationMs == nil || *v.DurationMs != 15_000 ||
		v.SizeBytes != 80_000_000 || v.MIME != "video/mp4" || v.Codec != "hvc1" || v.OriginalName != "VID_video.MP4" ||
		len(v.Revision) == 0 || len(v.Thumbhash) != 0 {
		t.Fatalf("video meta %+v", v)
	}
	e := got[string(ids["exif"])].Meta
	if e.Kind != op.KindPhoto || e.TakenAt != ms("2026-07-05T10:00:00Z") || e.TakenAtSource != op.SourceExifOffset ||
		e.LocalOffsetSec == nil || *e.LocalOffsetSec != 4*3600 || e.RawTime.ExifDateTime != "2026:07:05 14:00:00" ||
		e.OriginalName != "DSC_0001.JPG" || e.MIME != "image/jpeg" || e.Width != 3000 || e.Height != 2000 {
		t.Fatalf("exif photo meta %+v", e)
	}
	if k := got[string(ids["live"])].Meta.Kind; k != op.KindLivePhoto {
		t.Fatalf("live photo kind %d", k)
	}
}

func TestScreenshotsAndHiddenAlbumsAreExcludedByDefault(t *testing.T) {
	shot := photo("shot", "2026-07-02T10:00:00Z")
	shot.Screenshot = true
	hidden := photo("hidden", "2026-07-02T11:00:00Z")
	hidden.Hidden = true
	a, b, p := pair(t, shelf(photo("plain", "2026-07-02T12:00:00Z"), shot, hidden))
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")

	ids := assetIDs(t, a, p.ID)
	waitAssets(t, p.ID, idSet(ids["plain"]), a, b)
	selected := map[string]bool{}
	for _, c := range selection(t, a, p.ID).Items {
		selected[c.LocalID] = c.Selected
	}
	if want := map[string]bool{"plain": true, "shot": false, "hidden": false}; !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected %v, want %v", selected, want)
	}

	decide(t, a, p.ID, ids["shot"], true)
	waitAssets(t, p.ID, idSet(ids["plain"], ids["shot"]), a, b)
}

func TestExcludedFileDisappearsForEveryone(t *testing.T) {
	lib := shelf(photo("1", "2026-07-02T10:00:00Z"), photo("2", "2026-07-02T11:00:00Z"), photo("3", "2026-07-02T12:00:00Z"))
	a := openWith(t, t.TempDir(), "ios", lib)
	b := open(t, t.TempDir(), "android")
	c := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)
	join(t, b, inv, "Боря")
	join(t, c, inv, "Вера")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	waitAssets(t, p.ID, idSet(ids["1"], ids["2"], ids["3"]), a, b, c)

	decide(t, a, p.ID, ids["2"], false)
	waitAssets(t, p.ID, idSet(ids["1"], ids["3"]), a, b, c)

	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	if got := keys(assetsOn(t, a, p.ID)); !slices.Equal(got, idSet(ids["1"], ids["3"])) {
		t.Fatal("selecting the range again brought back the excluded file")
	}

	decide(t, a, p.ID, ids["2"], true)
	waitAssets(t, p.ID, idSet(ids["1"], ids["2"], ids["3"]), a, b, c)
}

func TestTogglingListedFilesDoesNotScanTheLibraryAgain(t *testing.T) {
	lib := shelf(photo("1", "2026-07-02T10:00:00Z"), photo("2", "2026-07-02T11:00:00Z"), photo("3", "2026-07-02T12:00:00Z"))
	a := openWith(t, t.TempDir(), "ios", lib)
	p := create(t, a, "Грузия", "Аня")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	listed := lib.scanned()
	for i := range 5 {
		decide(t, a, p.ID, ids["1"], i%2 == 1)
		decide(t, a, p.ID, ids["2"], false)
	}
	if got := lib.scanned() - listed; got != 0 {
		t.Fatalf("10 toggles after the list scanned the library %d times, want 0", got)
	}
	if got := keys(assetsOn(t, a, p.ID)); !slices.Equal(got, idSet(ids["3"])) {
		t.Fatalf("published %d files after the toggles, want only 3", len(got))
	}

	lib.set(photo("1", "2026-07-02T10:00:00Z"), photo("2", "2026-07-02T11:00:00Z"), photo("3", "2026-07-02T12:00:00Z"), photo("4", "2026-07-02T13:00:00Z"))
	fresh := assetIDs(t, a, p.ID)
	decide(t, a, p.ID, fresh["4"], true)
	if got := keys(assetsOn(t, a, p.ID)); !slices.Equal(got, idSet(ids["3"], fresh["4"])) {
		t.Fatal("a file that appeared in a new list cannot be toggled")
	}
	choose(t, a, p.ID, "2026-07-02T11:30:00Z", "2026-07-10T00:00:00Z")
	decide(t, a, p.ID, fresh["3"], false)
	if got := keys(assetsOn(t, a, p.ID)); !slices.Equal(got, idSet(fresh["4"])) {
		t.Fatal("toggle after a new range used the files of the old range")
	}
}

func TestManualDecisionsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	lib := shelf(photo("1", "2026-07-02T10:00:00Z"), photo("2", "2026-07-02T11:00:00Z"))
	a := openWith(t, dir, "ios", lib)
	p := create(t, a, "Грузия", "Аня")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	decide(t, a, p.ID, ids["2"], false)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a = openWith(t, dir, "ios", lib)
	s := selection(t, a, p.ID)
	if s.From != ms("2026-07-01T00:00:00Z") || s.To != ms("2026-07-10T00:00:00Z") || len(s.Items) != 2 {
		t.Fatalf("selection after restart %+v", s)
	}
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-20T00:00:00Z")
	if got := keys(assetsOn(t, a, p.ID)); !slices.Equal(got, idSet(ids["1"])) {
		t.Fatal("excluded file was published after restart")
	}
}

func TestChangingRangePublishesOnlyTheDifference(t *testing.T) {
	day := func(d string) node.MediaItem { return photo(d, "2026-07-0"+d+"T12:00:00Z") }
	lib := shelf(day("1"), day("2"), day("3"), day("4"))
	dirA, dirB := t.TempDir(), t.TempDir()
	a := openWith(t, dirA, "ios", lib)
	b := open(t, dirB, "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	secret := secretOf(t, a, p)

	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-03T00:00:00Z")
	first := assetIDs(t, a, p.ID)
	choose(t, a, p.ID, "2026-07-02T00:00:00Z", "2026-07-05T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	choose(t, a, p.ID, "2026-07-02T00:00:00Z", "2026-07-05T00:00:00Z")
	edited := day("3")
	edited.Modified++
	lib.set(day("1"), day("2"), edited, day("4"))
	choose(t, a, p.ID, "2026-07-02T00:00:00Z", "2026-07-05T00:00:00Z")
	waitAssets(t, p.ID, idSet(ids["2"], ids["3"], ids["4"]), a, b)
	revision := assetsOn(t, a, p.ID)[string(ids["3"])].Meta.Revision
	deadline := time.Now().Add(10 * time.Second)
	for !bytes.Equal(assetsOn(t, b, p.ID)[string(ids["3"])].Meta.Revision, revision) {
		if time.Now().After(deadline) {
			t.Fatal("edited file did not reach the member")
		}
		time.Sleep(10 * time.Millisecond)
	}
	author := a.Author()
	if err := errors.Join(a.Close(), b.Close()); err != nil {
		t.Fatal(err)
	}

	changes := changesBy(t, logOf(t, dirB, p, secret), author)
	if len(changes) != 4 {
		t.Fatalf("author published %d asset ops, want 4: %+v", len(changes), changes)
	}
	added := func(c op.Change) []string {
		add, ok := c.(op.AssetAdd)
		if !ok {
			return nil
		}
		var out [][]byte
		for _, m := range add.Assets {
			out = append(out, m.AssetID)
		}
		return idSet(out...)
	}
	removed := func(c op.Change) []string {
		rm, ok := c.(op.AssetRemove)
		if !ok {
			return nil
		}
		return idSet(rm.AssetIDs...)
	}
	if got := added(changes[0]); !slices.Equal(got, idSet(first["1"], first["2"])) {
		t.Fatalf("first range published %+v", changes[0])
	}
	second := []op.Change{changes[1], changes[2]}
	if _, ok := second[0].(op.AssetAdd); ok {
		second[0], second[1] = second[1], second[0]
	}
	if !slices.Equal(removed(second[0]), idSet(first["1"])) || !slices.Equal(added(second[1]), idSet(ids["3"], ids["4"])) {
		t.Fatalf("changed range published %+v", second)
	}
	last, ok := changes[3].(op.AssetAdd)
	if !ok || !slices.Equal(added(last), idSet(ids["3"])) || !bytes.Equal(last.Assets[0].Revision, revision) {
		t.Fatalf("edited file published %+v", changes[3])
	}
	for _, m := range second[1].(op.AssetAdd).Assets {
		if bytes.Equal(m.AssetID, ids["3"]) && bytes.Equal(m.Revision, revision) {
			t.Fatal("edit did not change the revision")
		}
	}
}

type located struct {
	node.MediaItem
	Latitude  float64            `json:"latitude"`
	Longitude float64            `json:"longitude"`
	Location  map[string]float64 `json:"location"`
	Album     string             `json:"album"`
	Path      string             `json:"path"`
}

func TestDataOutsideAssetMetaStaysOnDevice(t *testing.T) {
	const (
		localID = "content://media/external/images/media/4242"
		path    = "/storage/emulated/0/DCIM/Camera/PXL_20260702.jpg"
		album   = "Секретный альбом"
		lat     = 41.715137
		lon     = 44.827096
	)
	item := located{
		MediaItem: photo(localID, "2026-07-02T10:00:00Z"),
		Latitude:  lat,
		Longitude: lon,
		Location:  map[string]float64{"lat": lat, "lon": lon, "alt": 491.5},
		Album:     album,
		Path:      path,
	}
	item.OriginalName = "PXL_20260702.jpg"
	dirA, dirB := t.TempDir(), t.TempDir()
	a := openWith(t, dirA, "android", shelf(item))
	b := open(t, dirB, "ios")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	secret := secretOf(t, a, p)
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	waitAssets(t, p.ID, idSet(ids[localID]), a, b)
	author := a.Author()
	if err := errors.Join(a.Close(), b.Close()); err != nil {
		t.Fatal(err)
	}

	raws := logOf(t, dirB, p, secret)
	secrets := map[string][]byte{"local id": []byte(localID), "path": []byte(path), "album": []byte(album)}
	for name, v := range map[string]float64{"latitude": lat, "longitude": lon} {
		secrets[name+" float64"] = binary.BigEndian.AppendUint64(nil, math.Float64bits(v))
		secrets[name+" float32"] = binary.BigEndian.AppendUint32(nil, math.Float32bits(float32(v)))
		secrets[name+" text"] = []byte(fmt.Sprintf("%.3f", v))
		secrets[name+" e7"] = binary.BigEndian.AppendUint32(nil, uint32(int32(v*1e7)))
	}
	for _, raw := range raws {
		for name, s := range secrets {
			if bytes.Contains(raw, s) {
				t.Errorf("%s leaked into an op", name)
			}
		}
	}

	adds := 0
	for _, raw := range raws {
		o, err := op.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if o.Author != author || o.Type != op.TypeAssetAdd {
			continue
		}
		adds++
		var payload map[uint64][]map[uint64]cbor.RawMessage
		if err := cbor.Unmarshal(o.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 1 || len(payload[1]) != 1 {
			t.Fatalf("payload %v", payload)
		}
		for k := range payload[1][0] {
			if k < 1 || k > 15 {
				t.Errorf("asset meta carries field %d outside AssetMeta", k)
			}
		}
	}
	if adds != 1 {
		t.Fatalf("%d AssetAdd ops, want 1", adds)
	}
}

type cloudOnly struct {
	node.MediaItem
	Local     bool `json:"local"`
	CloudOnly bool `json:"cloud_only"`
}

func TestFileWithoutLocalOriginalIsSelectable(t *testing.T) {
	item := cloudOnly{MediaItem: photo("icloud", "2026-07-02T10:00:00Z"), CloudOnly: true}
	a, b, p := pair(t, shelf(item))
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	ids := assetIDs(t, a, p.ID)
	waitAssets(t, p.ID, idSet(ids["icloud"]), a, b)
	m := assetsOn(t, b, p.ID)[string(ids["icloud"])].Meta
	if m.SizeBytes != item.SizeBytes || m.Width != item.Width || m.OriginalName != item.OriginalName {
		t.Fatalf("meta %+v", m)
	}
}

func TestSameLocalIDOnTwoDevicesGivesTwoAssets(t *testing.T) {
	a := openWith(t, t.TempDir(), "android", shelf(photo("1", "2026-07-02T10:00:00Z")))
	b := openWith(t, t.TempDir(), "android", shelf(photo("1", "2026-07-02T11:00:00Z")))
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	choose(t, b, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	idA, idB := assetIDs(t, a, p.ID)["1"], assetIDs(t, b, p.ID)["1"]
	if bytes.Equal(idA, idB) {
		t.Fatal("two devices got the same asset id")
	}
	waitAssets(t, p.ID, idSet(idA, idB), a, b)
	as := assetsOn(t, a, p.ID)
	if as[string(idA)].Owner != a.Author() || as[string(idB)].Owner != b.Author() {
		t.Fatalf("owners %+v", as)
	}
}

func TestSelectionErrors(t *testing.T) {
	a := openWith(t, t.TempDir(), "ios", shelf(photo("1", "2026-07-02T10:00:00Z"), photo("2", "2026-08-02T10:00:00Z")))
	p := create(t, a, "Грузия", "Аня")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	outside := assetIDs(t, a, p.ID)
	choose(t, a, p.ID, "2026-08-01T00:00:00Z", "2026-08-10T00:00:00Z")
	ctx := context.Background()
	if err := a.Decide(ctx, p.ID, outside["1"], false); !errors.Is(err, node.ErrUnknownAsset) {
		t.Errorf("file outside the range: err = %v, want ErrUnknownAsset", err)
	}
	unknown := []byte("no such pile")
	if err := a.Select(ctx, unknown, 0, 1); !errors.Is(err, node.ErrUnknownPile) {
		t.Errorf("select: err = %v, want ErrUnknownPile", err)
	}
	if _, err := a.Selection(unknown); !errors.Is(err, node.ErrUnknownPile) {
		t.Errorf("selection: err = %v, want ErrUnknownPile", err)
	}

	noLibrary := open(t, t.TempDir(), "desktop")
	q := create(t, noLibrary, "Грузия", "Аня")
	if err := noLibrary.Select(ctx, q.ID, 0, 1); !errors.Is(err, node.ErrNoMedia) {
		t.Errorf("node without a library: err = %v, want ErrNoMedia", err)
	}
}
