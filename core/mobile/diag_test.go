package mobile_test

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/mobile"
)

type logged struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func events(t *testing.T) []logged {
	t.Helper()
	code, body := call(t, http.MethodGet, "/diag", mobile.Token(), nil)
	if code != http.StatusOK {
		t.Fatalf("GET /diag: status = %d", code)
	}
	var out []logged
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func export(t *testing.T) string {
	t.Helper()
	code, body := call(t, http.MethodGet, "/diag/export", mobile.Token(), nil)
	if code != http.StatusOK {
		t.Fatalf("GET /diag/export: status = %d", code)
	}
	return string(body)
}

func find(es []logged, kind, text string) []logged {
	var out []logged
	for _, e := range es {
		if e.Kind == kind && strings.Contains(e.Message, text) {
			out = append(out, e)
		}
	}
	return out
}

func TestReportReachesLogBeforeStartWhileRunningAndAfterStop(t *testing.T) {
	d := newDevice(t)
	mobile.Report("discovery", "nsd: discovery of _pileup._tcp failed, error 0")
	start(t, d)
	mobile.Report("media", "MediaStore query returned no cursor")
	if es := events(t); len(find(es, "discovery", "nsd: discovery")) != 1 || len(find(es, "media", "no cursor")) != 1 {
		t.Fatalf("events %+v, want the reports before Start and while running", es)
	}
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	mobile.Report("discovery", "nsd: stop discovery failed, error 0")
	start(t, d)
	es := events(t)
	if len(find(es, "discovery", "nsd: stop discovery")) != 1 || len(find(es, "media", "no cursor")) != 1 || len(find(es, "app", "1.4.0 android started")) != 1 || find(es, "app", "1.4.0 android started")[0].Count != 2 {
		t.Fatalf("events after restart %+v, want the report after Stop, the earlier events and the start counted twice", es)
	}
}

func TestReportHidesToken(t *testing.T) {
	start(t, newDevice(t))
	token := mobile.Token()
	mobile.Report("app", "request with Bearer "+token+" failed")
	if es := find(events(t), "app", "request with Bearer"); len(es) != 1 || strings.Contains(es[0].Message, token) {
		t.Fatalf("events %+v, want the report without the token", es)
	}
}

func TestGoCrashIsInLogAfterNextStart(t *testing.T) {
	if dir := os.Getenv("PILEUP_CRASH_DIR"); dir != "" {
		d := newDevice(t)
		if err := mobile.Start(dir, "android", "1.4.0", d.keys, d.media, d.media, d.network, d.lan); err != nil {
			t.Fatal(err)
		}
		go func() {
			var m map[string]int
			m["boom"]++
		}()
		select {}
	}
	d := newDevice(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestGoCrashIsInLogAfterNextStart$")
	cmd.Env = append(os.Environ(), "PILEUP_CRASH_DIR="+d.dir)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "assignment to entry in nil map") {
		t.Fatalf("child did not crash: %v\n%s", err, out)
	}

	start(t, d)
	if crashes := find(events(t), "crash", "assignment to entry in nil map"); len(crashes) != 1 || !strings.Contains(crashes[0].Message, "goroutine") {
		t.Fatalf("events %+v, want one crash with its trace", events(t))
	}
	if !strings.Contains(export(t), "assignment to entry in nil map") {
		t.Fatal("crash is not in the export")
	}

	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	start(t, d)
	if crashes := find(events(t), "crash", ""); len(crashes) != 1 || crashes[0].Count != 1 {
		t.Fatalf("crash events after a clean restart %+v, want the one crash only", crashes)
	}
}
