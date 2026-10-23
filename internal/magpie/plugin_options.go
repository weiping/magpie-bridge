package magpie

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// PluginOptions reads one plugin's options: the JSON object magpie keeps
// for it (its own `magpie.options` in package.json are the defaults;
// these are the ones the user or another program set).
//
// It goes through magpie's own CLI, which is the only way in: the file
// behind it is magpie's private store, and reading it directly would be
// reading a format nobody promised to keep.
func (c *Client) PluginOptions(ctx context.Context, spec string) (map[string]any, error) {
	out, err := c.cli(ctx, "plugin", "options", spec)
	if err != nil {
		return nil, err
	}
	text := string(out)
	// "No options set." means the plugin has none yet: the answer is an
	// empty object, not an error. What follows it is the package's
	// suggestion, not the options.
	if strings.HasPrefix(strings.TrimSpace(text), "No options set") {
		return map[string]any{}, nil
	}
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			var opts map[string]any
			if err := json.Unmarshal([]byte(text[i:j+1]), &opts); err == nil {
				return opts, nil
			}
		}
	}
	return map[string]any{}, nil
}

// PluginSetOptions writes one plugin's options: magpie stores the object
// and the page reads it back with magpie.options.get().
//
// Writing does not restart magpie's plugin host (measured: the host's
// pid is the same before and after), so a program may publish a
// snapshot this way; it costs one CLI call, about 0.2 s.
func (c *Client) PluginSetOptions(ctx context.Context, spec string, opts map[string]any) error {
	b, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	_, err = c.cli(ctx, "plugin", "options", spec, string(b))
	return err
}

// PluginOptionKey is the key under which a plugin's options object
// carries another program's payload: the page's own keys (period, sort)
// and mbridge's (mbridge) live side by side, and each side writes only
// its own.
const PluginOptionKey = "mbridge"

// PluginCommandKey is the key under which the page asks mbridge to do
// something: {id, seq}. The sequence number makes the command one-shot:
// mbridge runs it once, however often the page repeats it.
const PluginCommandKey = "cmd"

// PluginCommand is one request from the page to mbridge.
type PluginCommand struct {
	ID  string `json:"id"`
	Seq int64  `json:"seq"`
}

// Plugin command ids.
const (
	// CommandRefresh asks mbridge to publish its snapshot now.
	CommandRefresh = "refresh"
	// CommandRestart asks mbridge to restart itself.
	CommandRestart = "restart"
)

// ReadPluginCommand reads the command a page left in a plugin's options.
// It returns nil when there is none.
func ReadPluginCommand(opts map[string]any) *PluginCommand {
	raw, ok := opts[PluginCommandKey].(map[string]any)
	if !ok {
		return nil
	}
	id, _ := raw["id"].(string)
	if id == "" {
		return nil
	}
	seq := int64(0)
	switch v := raw["seq"].(type) {
	case float64:
		seq = int64(v)
	case int64:
		seq = v
	case int:
		seq = int64(v)
	}
	return &PluginCommand{ID: id, Seq: seq}
}

// SetPluginCommand puts a command into a plugin's options, keeping the
// other keys: the page's and mbridge's share one object.
func SetPluginCommand(opts map[string]any, cmd PluginCommand) {
	if opts == nil {
		return
	}
	opts[PluginCommandKey] = map[string]any{"id": cmd.ID, "seq": cmd.Seq}
}

// PluginSnapshot returns the payload another program left under the
// mbridge key, or nil.
func PluginSnapshot(opts map[string]any) map[string]any {
	raw, ok := opts[PluginOptionKey].(map[string]any)
	if !ok {
		return nil
	}
	return raw
}

// SetPluginSnapshot puts a payload under the mbridge key, keeping the
// other keys.
func SetPluginSnapshot(opts map[string]any, snapshot map[string]any) {
	if opts == nil {
		return
	}
	opts[PluginOptionKey] = snapshot
}

// PluginSnapshotSeq is the sequence number of a published snapshot, or 0.
func PluginSnapshotSeq(snapshot map[string]any) int64 {
	if snapshot == nil {
		return 0
	}
	switch v := snapshot["seq"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

// PluginSnapshotAt is when the snapshot was published, or the zero time.
func PluginSnapshotAt(snapshot map[string]any) (t time.Time, ok bool) {
	if snapshot == nil {
		return time.Time{}, false
	}
	s, _ := snapshot["at"].(string)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, false
		}
	}
	return t, true
}

// MergePluginOptions is what a writer does before it writes: it keeps the
// keys the other side owns and replaces only its own. The page writes
// period, sort and cmd; mbridge writes mbridge.
func MergePluginOptions(current, mine map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for k, v := range current {
		out[k] = v
	}
	for _, k := range keys {
		if v, ok := mine[k]; ok {
			out[k] = v
		}
	}
	return out
}

// ErrNoPlugin is what magpie says when the plugin is not installed.
func ErrNoPlugin(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no plugin")
}
