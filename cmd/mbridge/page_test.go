package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weiping/magpie-bridge/internal/fsutil"
	"github.com/weiping/magpie-bridge/internal/magpie"
)

// fakeOptions is a magpie whose plugin options live in a map.
type fakeOptions struct {
	opts map[string]any
	// writes counts the publications
	writes int
	// readErr is what the read answers with
	readErr error
}

func (f *fakeOptions) client(t *testing.T) *magpie.Client {
	t.Helper()
	c := magpie.New("")
	c.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "plugin" && args[1] == "options" {
			if len(args) == 3 {
				if f.readErr != nil {
					return nil, f.readErr
				}
				b, _ := json.Marshal(f.opts)
				return b, nil
			}
			var next map[string]any
			if err := json.Unmarshal([]byte(args[3]), &next); err != nil {
				return nil, err
			}
			f.opts = next
			f.writes++
			return []byte("✓ options set"), nil
		}
		return []byte(""), nil
	}
	return c
}

func testBridge(t *testing.T, f *fakeOptions, payload map[string]any) (*pageBridge, *int) {
	t.Helper()
	restarts := 0
	b := newPageBridge(f.client(t), func(kind string) map[string]any {
		out := map[string]any{}
		for k, v := range payload {
			out[k] = v
		}
		return out
	}, func(context.Context) error {
		restarts++
		return nil
	})
	b.now = func() time.Time { return time.Now() }
	return b, &restarts
}

// TestTheBridgePublishesItsSnapshot: the page's options get mbridge's
// payload, and the page's own keys stay.
func TestTheBridgePublishesItsSnapshot(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{"period": "7d", "sort": "tokens"}}
	b, _ := testBridge(t, f, map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}})
	b.tick(context.Background())

	if f.writes != 1 {
		t.Fatalf("writes %d", f.writes)
	}
	if f.opts["period"] != "7d" || f.opts["sort"] != "tokens" {
		t.Fatalf("the page's keys were lost: %+v", f.opts)
	}
	snap := magpie.PluginSnapshot(f.opts)
	if snap == nil || snap["version"] != "0.3.5" {
		t.Fatalf("snapshot %+v", snap)
	}
	if magpie.PluginSnapshotSeq(snap) == 0 {
		t.Fatalf("no sequence number: %+v", snap)
	}
	if _, ok := magpie.PluginSnapshotAt(snap); !ok {
		t.Fatalf("no time: %+v", snap)
	}
}

// TestTheBridgeDoesNotPublishTheSameThingOverAndOver: the clock moves,
// the content does not: no new write.
func TestTheBridgeDoesNotPublishTheSameThingOverAndOver(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{}}
	b, _ := testBridge(t, f, map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}})
	now := time.Now()
	b.now = func() time.Time { return now }
	b.tick(context.Background())
	if f.writes != 1 {
		t.Fatalf("writes %d", f.writes)
	}
	now = now.Add(time.Hour)
	b.tick(context.Background())
	if f.writes != 1 {
		t.Fatalf("the same snapshot was published again: %d writes", f.writes)
	}
}

// TestTheBridgePublishesAgainWhenSomethingChanged: new decisions, new
// write — once the interval has passed.
func TestTheBridgePublishesAgainWhenSomethingChanged(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{}}
	payload := map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}}
	b, _ := testBridge(t, f, payload)
	now := time.Now()
	b.now = func() time.Time { return now }
	b.tick(context.Background())
	payload["decisions"] = []any{map[string]any{"reason": "R6-adopt"}}
	b.tick(context.Background())
	if f.writes != 1 {
		// the change came too soon after the last publish: the next tick
		// will carry it
		t.Fatalf("writes %d", f.writes)
	}
	now = now.Add(pagePublishInterval + time.Second)
	b.tick(context.Background())
	if f.writes != 2 {
		t.Fatalf("the change was not published: %d writes", f.writes)
	}
}

// TestTheBridgeRunsThePagesRestartCommandOnce: the page asks, mbridge
// restarts once, and the same command is not run again.
func TestTheBridgeRunsThePagesRestartCommandOnce(t *testing.T) {
	t.Setenv("MBRIDGE_CONFIG_DIR", t.TempDir())
	f := &fakeOptions{opts: map[string]any{"cmd": map[string]any{"id": "restart", "seq": float64(5)}}}
	b, restarts := testBridge(t, f, map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}})
	b.tick(context.Background())
	if *restarts != 1 {
		t.Fatalf("restarts %d", *restarts)
	}
	// the page repeats the command (it does not know the restart worked):
	// it must not restart again
	b.tick(context.Background())
	if *restarts != 1 {
		t.Fatalf("restarts %d", *restarts)
	}
	// and the command is remembered, so a new process does not run it
	if got := pageCommandDone(); got != 5 {
		t.Fatalf("remembered %d", got)
	}
}

// TestTheRefreshCommandPublishesNow: the page asks for fresh numbers and
// gets them, whatever the interval says.
func TestTheRefreshCommandPublishesNow(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{"cmd": map[string]any{"id": "refresh", "seq": float64(2)}}}
	b, _ := testBridge(t, f, map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}})
	now := time.Now()
	b.now = func() time.Time { return now }
	b.tick(context.Background())
	if f.writes != 1 {
		t.Fatalf("writes %d", f.writes)
	}
}

// TestTheBridgePublishesAgainWhenThePageOverwroteIt: the page writes its
// own keys over the whole object (magpie's options.set replaces it), and
// mbridge's snapshot is gone: mbridge puts it back.
func TestTheBridgePublishesAgainWhenThePageOverwroteIt(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{}}
	b, _ := testBridge(t, f, map[string]any{"version": "0.3.5", "pid": 42, "live": []any{}})
	b.tick(context.Background())
	if f.writes != 1 {
		t.Fatalf("writes %d", f.writes)
	}
	// the page replaces the object with its own keys: mbridge's snapshot
	// is gone
	f.opts = map[string]any{"period": "today"}
	now := time.Now().Add(pagePublishInterval + time.Second)
	b.now = func() time.Time { return now }
	b.tick(context.Background())
	if f.writes != 2 {
		t.Fatalf("mbridge did not put its snapshot back: %d writes", f.writes)
	}
	if magpie.PluginSnapshot(f.opts) == nil {
		t.Fatalf("no snapshot: %+v", f.opts)
	}
}

// TestTheBridgeIsQuietWhenThePageIsNotInstalled: the user may never
// install it, and that is not something to log every five seconds.
func TestTheBridgeIsQuietWhenThePageIsNotInstalled(t *testing.T) {
	f := &fakeOptions{readErr: &magpie.Error{Op: "plugin options", Msg: `no plugin "@weiping/mbridge"`}}
	b, _ := testBridge(t, f, map[string]any{"version": "0.3.5"})
	b.tick(context.Background())
	if f.writes != 0 {
		t.Fatalf("writes %d", f.writes)
	}
	if !b.quiet {
		t.Fatal("it should be quiet about it")
	}
}

// TestTheCommandFileRoundTrip: the sequence number survives a restart.
func TestTheCommandFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MBRIDGE_CONFIG_DIR", dir)
	if err := pageCommandRemember(11); err != nil {
		t.Fatal(err)
	}
	if got := pageCommandDone(); got != 11 {
		t.Fatalf("got %d", got)
	}
	if _, err := os.Stat(filepath.Join(fsutil.ConfigDir(), "page-command.json")); err != nil {
		t.Fatal(err)
	}
}

// TestTheBridgePublishesTheWindowThePageSelected: if the page sets
// period to 1d or 30d, mbridge publishes that window.
func TestTheBridgePublishesTheWindowThePageSelected(t *testing.T) {
	f := &fakeOptions{opts: map[string]any{"period": "30d"}}
	var askedWindow string
	b := newPageBridge(f.client(t), func(kind string) map[string]any {
		askedWindow = kind
		return map[string]any{"version": "0.3.5", "window": map[string]any{"kind": kind}}
	}, nil)
	b.tick(context.Background())
	if askedWindow != "30d" {
		t.Fatalf("asked window %q, want 30d", askedWindow)
	}
}
