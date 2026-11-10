package node_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/node"
	"modernc.org/sqlite"
)

const (
	selectorDir  = "PILEUP_SELECTOR_DIR"
	selectorPile = "PILEUP_SELECTOR_PILE"
	selectorMode = "PILEUP_SELECTOR_MODE"
	crashed      = 3
)

var (
	oldRange = [2]string{"2026-07-01T00:00:00Z", "2026-07-03T00:00:00Z"}
	newRange = [2]string{"2026-07-02T00:00:00Z", "2026-07-05T00:00:00Z"}
)

type phone struct {
	crashOn int64
}

func (p phone) Media(from, to int64) ([]byte, error) {
	if p.crashOn != 0 && from == p.crashOn {
		os.Exit(crashed)
	}
	var items []node.MediaItem
	for d := 1; d <= 4; d++ {
		items = append(items, photo(strconv.Itoa(d), fmt.Sprintf("2026-07-0%dT12:00:00Z", d)))
	}
	return json.Marshal(items)
}

func (phone) Rendition(string, int) ([]byte, error) {
	return nil, errors.New("no renditions on this phone")
}

func original(local string) []byte {
	return []byte("original of " + local)
}

func (phone) OriginalSize(local string) (int64, error) {
	return int64(len(original(local))), nil
}

func (phone) ReadOriginal(local string, offset, length int64) ([]byte, error) {
	b := original(local)
	return b[offset:min(offset+length, int64(len(b)))], nil
}

func openPhone(t *testing.T, dir string) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   "android",
		Addrs:      loopback,
		Media:      phone{},
		Renditions: phone{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

type picked struct {
	From, To int64
	Selected []string
	Served   []string
}

func pickedIn(r [2]string, files ...string) picked {
	return picked{From: ms(r[0]), To: ms(r[1]), Selected: files, Served: files}
}

func pickedOn(t *testing.T, n *node.Node, id []byte) picked {
	t.Helper()
	s := selection(t, n, id)
	got := picked{From: s.From, To: s.To}
	for _, c := range s.Items {
		if c.Selected {
			got.Selected = append(got.Selected, c.LocalID)
		}
	}
	for _, a := range assetsOn(t, n, id) {
		w := httptest.NewRecorder()
		n.ServeOriginal(w, httptest.NewRequest(http.MethodGet, "/", nil), id, a.Meta.AssetID, a.Meta.Revision)
		served := fmt.Sprintf("%x not served: %d", a.Meta.AssetID, w.Code)
		for d := 1; d <= 4; d++ {
			if w.Code == http.StatusOK && slices.Equal(w.Body.Bytes(), original(strconv.Itoa(d))) {
				served = strconv.Itoa(d)
			}
		}
		got.Served = append(got.Served, served)
	}
	slices.Sort(got.Selected)
	slices.Sort(got.Served)
	return got
}

func pileDB(t *testing.T, dir string, id []byte) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "piles", hex.EncodeToString(id)+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func failOnOp(t *testing.T, dir string, id []byte, nth int, action string) {
	t.Helper()
	db := pileDB(t, dir, id)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ops`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER injected BEFORE INSERT ON ops WHEN (SELECT count(*) FROM ops) >= %d BEGIN SELECT %s; END`,
		count+nth-1, action)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func repair(t *testing.T, dir string, id []byte) {
	t.Helper()
	db := pileDB(t, dir, id)
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS injected`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectingProcess(t *testing.T) {
	dir := os.Getenv(selectorDir)
	if dir == "" {
		t.Skip("helper process for TestInterruptedSelectionChangeIsAllOrNothing")
	}
	sqlite.MustRegisterScalarFunction("crash", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		os.Exit(crashed)
		return nil, nil
	})
	id, err := hex.DecodeString(os.Getenv(selectorPile))
	if err != nil {
		t.Fatal(err)
	}
	lib := phone{}
	if os.Getenv(selectorMode) == "media" {
		lib.crashOn = ms(newRange[0])
	}
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "android",
		Addrs:    loopback,
		Media:    lib,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Select(context.Background(), id, ms(newRange[0]), ms(newRange[1])); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func selectInChild(t *testing.T, dir string, id []byte, mode string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSelectingProcess$")
	cmd.Env = append(os.Environ(), selectorDir+"="+dir, selectorPile+"="+hex.EncodeToString(id), selectorMode+"="+mode)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 && code != crashed {
		t.Fatalf("selecting process failed with %d:\n%s", code, out)
	}
	return cmd.ProcessState.ExitCode()
}

func TestInterruptedSelectionChangeIsAllOrNothing(t *testing.T) {
	old := pickedIn(oldRange, "1", "2")
	changed := pickedIn(newRange, "2", "3", "4")
	for _, tc := range []struct {
		name string
		op   int
		mode string
		exit int
		want picked
	}{
		{name: "crash while preparing the change", mode: "media", exit: crashed, want: old},
		{name: "crash after the selection is written, before the first op", op: 1, exit: crashed, want: old},
		{name: "crash between the ops", op: 2, exit: crashed, want: old},
		{name: "crash right after the change returned", exit: 0, want: changed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			n := openPhone(t, dir)
			p := create(t, n, "Грузия", "Аня")
			choose(t, n, p.ID, oldRange[0], oldRange[1])
			if got := pickedOn(t, n, p.ID); !reflect.DeepEqual(got, old) {
				t.Fatalf("before the change: %+v, want %+v", got, old)
			}
			if err := n.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.op > 0 {
				failOnOp(t, dir, p.ID, tc.op, "crash()")
			}

			if exit := selectInChild(t, dir, p.ID, tc.mode); exit != tc.exit {
				t.Fatalf("selecting process exited with %d, want %d", exit, tc.exit)
			}
			repair(t, dir, p.ID)

			n = openPhone(t, dir)
			if got := pickedOn(t, n, p.ID); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("after restart: %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFailedSelectionChangeLeavesPreviousSelection(t *testing.T) {
	old := pickedIn(oldRange, "1", "2")
	dir := t.TempDir()
	n := openPhone(t, dir)
	p := create(t, n, "Грузия", "Аня")
	choose(t, n, p.ID, oldRange[0], oldRange[1])

	failOnOp(t, dir, p.ID, 2, "RAISE(ABORT, 'injected write error')")
	if err := n.Select(context.Background(), p.ID, ms(newRange[0]), ms(newRange[1])); err == nil {
		t.Fatal("change succeeded despite the write error")
	}
	repair(t, dir, p.ID)
	if got := pickedOn(t, n, p.ID); !reflect.DeepEqual(got, old) {
		t.Fatalf("after the failed change: %+v, want %+v", got, old)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = openPhone(t, dir)
	if got := pickedOn(t, n, p.ID); !reflect.DeepEqual(got, old) {
		t.Fatalf("after restart: %+v, want %+v", got, old)
	}

	choose(t, n, p.ID, newRange[0], newRange[1])
	if got, want := pickedOn(t, n, p.ID), pickedIn(newRange, "2", "3", "4"); !reflect.DeepEqual(got, want) {
		t.Fatalf("after retrying the change: %+v, want %+v", got, want)
	}
}

func TestSelectionOfPreviousVersionIsKept(t *testing.T) {
	dir := t.TempDir()
	n := openPhone(t, dir)
	p := create(t, n, "Грузия", "Аня")
	choose(t, n, p.ID, oldRange[0], oldRange[1])
	ids := assetIDs(t, n, p.ID)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "piles", hex.EncodeToString(p.ID)+".selection.json")
	b, err := json.Marshal(map[string]any{
		"from":   ms(oldRange[0]),
		"to":     ms("2026-07-04T00:00:00Z"),
		"manual": map[string]bool{hex.EncodeToString(ids["2"]): false, hex.EncodeToString(ids["3"]): true},
		"local":  map[string]string{hex.EncodeToString(ids["1"]): "1", hex.EncodeToString(ids["2"]): "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, b, 0o600); err != nil {
		t.Fatal(err)
	}

	n = openPhone(t, dir)
	if got, want := pickedOn(t, n, p.ID), (picked{From: ms(oldRange[0]), To: ms("2026-07-04T00:00:00Z"), Selected: []string{"1", "2"}, Served: []string{"1", "2"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("selection of the previous version: %+v, want %+v", got, want)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous selection file is still there: %v", err)
	}
	choose(t, n, p.ID, oldRange[0], "2026-07-05T00:00:00Z")
	decide(t, n, p.ID, ids["1"], true)
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selection is still written to the previous file: %v", err)
	}
	if got, want := pickedOn(t, n, p.ID), pickedIn([2]string{oldRange[0], "2026-07-05T00:00:00Z"}, "1", "3", "4"); !reflect.DeepEqual(got, want) {
		t.Fatalf("manual decisions of the previous version were lost: %+v, want %+v", got, want)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = openPhone(t, dir)
	if got, want := pickedOn(t, n, p.ID), pickedIn([2]string{oldRange[0], "2026-07-05T00:00:00Z"}, "1", "3", "4"); !reflect.DeepEqual(got, want) {
		t.Fatalf("after restart: %+v, want %+v", got, want)
	}
}
