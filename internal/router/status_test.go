package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTheStatusViewReportsTheConfigWithDefaults: the page shows what is
// in force, not the file's text (SP14 §B).
func TestTheStatusViewReportsTheConfigWithDefaults(t *testing.T) {
	d := &Deps{
		Config:    Config{Tiers: map[Tier]TierCfg{TierFast: {Group: "group/fast"}}},
		Version:   "0.3.2",
		StartedAt: time.Now().Add(-90 * time.Second),
		Listen:    "127.0.0.1:3426",
		Service: func(context.Context) ServiceStatus {
			return ServiceStatus{Installed: true, Running: true, Detail: "running"}
		},
		MagpieInfo: func(context.Context) (string, bool) { return "0.1.1318", true },
	}
	v := d.StatusView()
	if v.Version != "0.3.2" {
		t.Fatalf("version %q", v.Version)
	}
	if v.UptimeS < 89 || v.UptimeS > 95 {
		t.Fatalf("uptime %d", v.UptimeS)
	}
	if v.PID != os.Getpid() {
		t.Fatalf("pid %d", v.PID)
	}
	if !v.MagpieOK || v.MagpieVersion != "0.1.1318" {
		t.Fatalf("magpie %+v", v)
	}
	if !v.Service.Installed || !v.Service.Running {
		t.Fatalf("service %+v", v.Service)
	}
	// the defaults are in force, not zero
	if v.Config.DefaultTier != TierBalanced {
		t.Fatalf("default tier %q", v.Config.DefaultTier)
	}
	if v.Config.ClassifyTimeoutMs == 0 || v.Config.EscalateTurns == 0 {
		t.Fatalf("config %+v", v.Config)
	}
	if v.Config.Thresholds.TierMin == 0 {
		t.Fatalf("thresholds %+v", v.Config.Thresholds)
	}
}

// TestTheStatusViewKeepsTheSaltInside: the experiment's salt is how the
// sessions are split between the arms, and it does not leave the process.
func TestTheStatusViewKeepsTheSaltInside(t *testing.T) {
	d := &Deps{Config: Config{Experiment: ExperimentConfig{Enabled: true, RouterPercent: 50,
		ControlTier: TierPerformance, Salt: "deadbeef"}}}
	v := d.StatusView()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "deadbeef") {
		t.Fatalf("the salt is in the payload: %s", b)
	}
	if !v.Config.Experiment.Enabled || v.Config.Experiment.RouterPercent != 50 {
		t.Fatalf("experiment %+v", v.Config.Experiment)
	}
}

// TestTheStatusViewFlagsAListenThatNeedsARestart: the config names
// another address, and it only takes a restart to change.
func TestTheStatusViewFlagsAListenThatNeedsARestart(t *testing.T) {
	d := &Deps{Config: Config{Listen: "127.0.0.1:9999"}, Listen: "127.0.0.1:3426"}
	if !d.StatusView().ListenPending {
		t.Fatal("not flagged")
	}
	d = &Deps{Config: Config{Listen: "127.0.0.1:3426"}, Listen: "127.0.0.1:3426"}
	if d.StatusView().ListenPending {
		t.Fatal("flagged without a change")
	}
}

// TestTheRouterEndpointCarriesTheProcessAndTheConfig: the status page
// reads one endpoint, so it must have all of it (SP14 §3.2).
func TestTheRouterEndpointCarriesTheProcessAndTheConfig(t *testing.T) {
	d, _, _ := testDeps(t, &Verdict{Tier: TierBalanced})
	d.Version = "0.3.2"
	d.Listen = "127.0.0.1:3426"
	d.StartedAt = time.Now().Add(-time.Minute)
	srv := testServer(d)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/v1/bridge/router")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"valid", "version", "pid", "uptime_s", "listen", "listen_pending",
		"magpie_ok", "service", "config_error", "config", "decisions", "tiers"} {
		if _, ok := body[key]; !ok {
			t.Errorf("the endpoint lacks %q", key)
		}
	}
	if body["version"] != "0.3.2" {
		t.Fatalf("version %v", body["version"])
	}
	cfg, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("config %v", body["config"])
	}
	if cfg["default_tier"] != string(TierBalanced) {
		t.Fatalf("default tier %v", cfg["default_tier"])
	}
	if strings.Contains(mustJSON(t, body), "salt") {
		t.Fatal("the endpoint carries the salt")
	}
}

// mustJSON is the payload as text, for a substring check.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
