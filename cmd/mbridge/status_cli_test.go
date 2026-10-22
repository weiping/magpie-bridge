package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiping/magpie-bridge/internal/fsutil"
)

// writeStatusConfig writes a valid router.json that names listen as the
// address the fake mbridge answers on.
func writeStatusConfig(t *testing.T, listen string) {
	t.Helper()
	cfg := fsutil.ConfigDir()
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"listen":"` + listen + `","magpie_url":"http://127.0.0.1:1",` +
		`"tiers":{"fast":{"group":"mb-fast","claude_alias":"f"},` +
		`"balanced":{"group":"mb-balanced","claude_alias":"b"},` +
		`"performance":{"group":"mb-perf","claude_alias":"p"}}}`
	if err := os.WriteFile(filepath.Join(cfg, "router.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStatusJSONIsOneObject: --json prints one JSON object and nothing
// else, and every conclusion the text version prints is in it.
func TestStatusJSONIsOneObject(t *testing.T) {
	withFakeMagpie(t, "deepseek/deepseek-chat")
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"valid": true,
			"decisions": []map[string]string{
				{"session": "s1", "tier": "fast", "reason": "R6-adopt"},
			},
		})
	}))
	defer bridge.Close()
	writeStatusConfig(t, bridge.Listener.Addr().String())

	out, err := captureStdout(t, func() error { return statusCmd([]string{"--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var rep statusReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("out %q: %v", out, err)
	}
	if !rep.MagpieOK {
		t.Fatalf("magpie not ok: %+v", rep)
	}
	if !rep.Running {
		t.Fatalf("not running: %+v", rep)
	}
	if len(rep.Decisions) != 1 || rep.Decisions[0].Reason != "R6-adopt" {
		t.Fatalf("decisions %+v", rep.Decisions)
	}
	// the keys a program reads exist even when they are false
	for _, key := range []string{`"magpie_ok"`, `"running"`, `"decisions"`, `"config_path"`} {
		if !strings.Contains(out, key) {
			t.Fatalf("out lacks %s: %q", key, out)
		}
	}
}

// TestStatusJSONIsTheOnlyThingOnStdout: nothing else may share stdout
// with the JSON, or a program cannot parse it.
func TestStatusJSONIsTheOnlyThingOnStdout(t *testing.T) {
	withFakeMagpie(t)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"valid": true})
	}))
	defer bridge.Close()
	writeStatusConfig(t, bridge.Listener.Addr().String())

	out, err := captureStdout(t, func() error { return statusCmd([]string{"--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var any map[string]any
	if err := json.Unmarshal([]byte(out), &any); err != nil {
		t.Fatalf("out %q: %v", out, err)
	}
}

// TestStatusTextAndJSONAgree: the two versions conclude the same things.
func TestStatusTextAndJSONAgree(t *testing.T) {
	withFakeMagpie(t)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"valid": true})
	}))
	defer bridge.Close()
	writeStatusConfig(t, bridge.Listener.Addr().String())

	text, err := captureStdout(t, func() error { return statusCmd(nil) })
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return statusCmd([]string{"--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var rep statusReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Running && !strings.Contains(text, "mbridge on "+rep.Listen) {
		t.Fatalf("text %q misses the running mbridge", text)
	}
	if len(rep.MissingGroups) > 0 && !strings.Contains(text, "missing in magpie") {
		t.Fatalf("text %q misses the missing groups %v", text, rep.MissingGroups)
	}
}

// TestStatusTakesNoOtherArgument: only --json is understood.
func TestStatusTakesNoOtherArgument(t *testing.T) {
	err := statusCmd([]string{"--yaml"})
	if err == nil || !strings.Contains(err.Error(), "status takes no arguments") {
		t.Fatalf("err %v", err)
	}
}
