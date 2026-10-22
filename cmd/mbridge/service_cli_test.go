package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiping/magpie-bridge/internal/service"
)

// TestServiceRestartRunsTheSupervisor: `mbridge service restart` hands the
// job to the platform's supervisor, once, and says so.
func TestServiceRestartRunsTheSupervisor(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin":  "launchctl kickstart -k gui/501/io.github.weiping.magpie-bridge",
		"linux":   "systemctl --user restart mbridge.service",
		"windows": "schtasks /Run /TN mbridge",
	} {
		home := t.TempDir()
		var calls []string
		old := newService
		newService = func() *service.Manager {
			return &service.Manager{GOOS: goos, Home: home, ConfigDir: home, UID: 501,
				Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					calls = append(calls, name+" "+strings.Join(args, " "))
					return nil, nil
				}}
		}
		path, content := service.Unit(goos, home, home, "/q")
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, content, 0o644)

		out, err := captureStdout(t, func() error { return serviceCmd([]string{"restart"}) })
		newService = old
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if !strings.Contains(out, "restarts now") {
			t.Fatalf("%s: out %q", goos, out)
		}
		if !strings.Contains(strings.Join(calls, "|"), want) {
			t.Fatalf("%s: calls %v", goos, calls)
		}
	}
}

// TestServiceRestartWithoutAUnitDoesNothing: mbridge that was never
// installed has nothing to restart, and that is not an error.
func TestServiceRestartWithoutAUnitDoesNothing(t *testing.T) {
	home := t.TempDir()
	var calls []string
	old := newService
	newService = func() *service.Manager {
		return &service.Manager{GOOS: "darwin", Home: home, ConfigDir: home, UID: 501,
			Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls = append(calls, name)
				return nil, nil
			}}
	}
	t.Cleanup(func() { newService = old })

	out, err := captureStdout(t, func() error { return serviceCmd([]string{"restart"}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
	if !strings.Contains(out, "restarts now") {
		t.Fatalf("out %q", out)
	}
}

// TestServiceTakesOnlyOneSubcommand: the usage names the four it has.
func TestServiceTakesOnlyOneSubcommand(t *testing.T) {
	for _, args := range [][]string{{"install", "extra"}, {"nope"}, {}} {
		err := serviceCmd(args)
		if err == nil || !strings.Contains(err.Error(), "usage: mbridge service install|uninstall|status|restart") {
			t.Fatalf("%v: err %v", args, err)
		}
	}
}
