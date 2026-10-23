package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/weiping/magpie-bridge/internal/fsutil"
	"github.com/weiping/magpie-bridge/internal/magpie"
)

// The magpie page plugin is a window plugin: it runs in magpie's sandbox
// and cannot reach mbridge (connect-src 'none'). The one channel between
// them is the plugin's own options object in magpie's store: the page
// reads it in-process (free), mbridge reads and writes it through
// magpie's CLI (about 0.2 s a call, and writing does not restart
// magpie's plugin host — measured, see the spec's §7).
//
// The two sides share one object and each writes only its own keys:
// the page owns "period", "sort" and "cmd", mbridge owns "mbridge".
const (
	// pagePluginSpec is the magpie plugin mbridge talks to.
	pagePluginSpec = "@weiping/mbridge"
	// pagePollInterval is how often mbridge looks at the options: for a
	// command from the page, and for a snapshot that needs publishing.
	pagePollInterval = 5 * time.Second
	// pagePublishInterval is the shortest time between two publishes.
	// Publishing costs a CLI call; the content changes far slower than
	// that, so mbridge only writes when something really changed.
	pagePublishInterval = 15 * time.Second
)

// pageBridge is mbridge's side of the magpie page: it publishes the
// status payload into the plugin's options and runs the commands the page
// leaves there.
type pageBridge struct {
	client *magpie.Client
	spec   string
	// payload is what to publish; nil means mbridge is not serving a
	// router (there is nothing to tell).
	payload func(kind string) map[string]any
	// restart performs the page's restart command.
	restart func(context.Context) error
	// now is the clock, for the tests.
	now func() time.Time

	seq          int64
	lastPublish  time.Time
	lastSnapshot string
	lastCmdSeq   int64
	quiet        bool
}

// newPageBridge is the bridge for the magpie page.
func newPageBridge(client *magpie.Client, payload func(string) map[string]any, restart func(context.Context) error) *pageBridge {
	return &pageBridge{client: client, spec: pagePluginSpec, payload: payload, restart: restart, now: time.Now}
}

// run watches the plugin's options until ctx is done.
func (b *pageBridge) run(ctx context.Context) {
	t := time.NewTicker(pagePollInterval)
	defer t.Stop()
	b.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.tick(ctx)
		}
	}
}

// tick is one round: read the options, run a command the page left, and
// publish the snapshot when it changed.
func (b *pageBridge) tick(ctx context.Context) {
	opts, err := b.client.PluginOptions(ctx, b.spec)
	if err != nil {
		// the page is not installed: that is not an error, only nothing
		// to say. Stay quiet about it: the user may never install it.
		if !b.quiet && !magpie.ErrNoPlugin(err) {
			log.Println("mbridge: the magpie page's options:", err)
		}
		b.quiet = true
		return
	}
	b.quiet = false
	if cmd := magpie.ReadPluginCommand(opts); cmd != nil && cmd.Seq > b.lastCmdSeq {
		b.lastCmdSeq = cmd.Seq
		b.runCommand(ctx, cmd)
	}
	b.publish(ctx, opts)
}

// runCommand does what the page asked.
func (b *pageBridge) runCommand(ctx context.Context, cmd *magpie.PluginCommand) {
	switch cmd.ID {
	case magpie.CommandRefresh:
		// publish now, whatever the interval says
		b.lastPublish = time.Time{}
	case magpie.CommandRestart:
		if b.restart == nil {
			return
		}
		// remember it before restarting: the new process reads the file
		// and does not run the same command a second time
		if err := pageCommandRemember(cmd.Seq); err != nil {
			log.Println("mbridge: remembering the page's command:", err)
		}
		log.Println("mbridge: the magpie page asked for a restart")
		if err := b.restart(ctx); err != nil {
			log.Println("mbridge: restart:", err)
		}
	}
}

// publish writes the snapshot into the plugin's options, when it changed
// and the interval allows it.
func (b *pageBridge) publish(ctx context.Context, opts map[string]any) {
	if b.payload == nil {
		return
	}
	windowKind := "7d"
	if p, ok := opts["period"].(string); ok && (p == "1d" || p == "7d" || p == "30d") {
		windowKind = p
	}
	payload := b.payload(windowKind)
	if payload == nil {
		return
	}
	now := b.now()
	cur := magpie.PluginSnapshot(opts)
	curSeq := magpie.PluginSnapshotSeq(cur)
	// the file does not hold what we published: the page replaced the
	// object with its own keys, or mbridge restarted. Put it back, even
	// if the interval says wait.
	lost := b.seq == 0 || cur == nil || curSeq < b.seq
	fresh := snapshotFingerprint(payload)
	if !lost {
		// only publish when the content changed: the clock moves every
		// tick, the interesting part does not
		if fresh == b.lastSnapshot {
			return
		}
		// and not more often than the interval: a busy router changes the
		// decisions on every turn, and each publish is a CLI call
		if now.Sub(b.lastPublish) < pagePublishInterval {
			return
		}
	}
	// the sequence number tells the page how fresh the snapshot is, and
	// tells mbridge that the file still holds what it wrote
	b.seq++
	payload["seq"] = b.seq
	payload["at"] = now.UTC().Format(time.RFC3339Nano)
	// the page's own keys stay: only mbridge's key is replaced
	next := magpie.MergePluginOptions(opts, map[string]any{
		magpie.PluginOptionKey: payload,
	}, magpie.PluginOptionKey)
	if err := b.client.PluginSetOptions(ctx, b.spec, next); err != nil {
		// the sequence number is now ahead of what is in the file: the
		// next tick publishes again
		log.Println("mbridge: publishing to the magpie page:", err)
		return
	}
	b.lastSnapshot = fresh
	b.lastPublish = now
}

// snapshotFingerprint is what "the snapshot changed" means: the parts
// that say something, without the clock.
func snapshotFingerprint(payload map[string]any) string {
	keep := map[string]any{}
	for _, k := range []string{"version", "pid", "listen", "service", "config_error", "live", "live_total", "decisions", "window"} {
		if v, ok := payload[k]; ok {
			keep[k] = v
		}
	}
	b, err := json.Marshal(keep)
	if err != nil {
		return ""
	}
	return string(b)
}

// pageCommandFile is where mbridge remembers the last command it ran, so
// a restart does not run the same command twice.
func pageCommandFile() string {
	return filepath.Join(fsutil.ConfigDir(), "page-command.json")
}

// pageCommandDone is the last command mbridge ran.
func pageCommandDone() int64 {
	b, err := os.ReadFile(pageCommandFile())
	if err != nil {
		return 0
	}
	var rec struct {
		Seq int64 `json:"seq"`
	}
	if json.Unmarshal(b, &rec) != nil {
		return 0
	}
	return rec.Seq
}

// pageCommandRemember writes the last command mbridge ran.
func pageCommandRemember(seq int64) error {
	b, err := json.Marshal(map[string]int64{"seq": seq})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(pageCommandFile(), b)
}
