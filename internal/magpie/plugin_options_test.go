package magpie

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeCLI answers the plugin options commands.
func fakeCLI(t *testing.T, read string, written *[]string) *Client {
	t.Helper()
	c := New("")
	c.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "plugin" && args[1] == "options" {
			if len(args) == 3 {
				return []byte(read), nil
			}
			if written != nil {
				*written = append(*written, args[3])
			}
			return []byte("✓ options set"), nil
		}
		return []byte(""), nil
	}
	return c
}

func TestPluginOptionsReadsTheObject(t *testing.T) {
	c := fakeCLI(t, `{"period":"7d","mbridge":{"seq":12}}`, nil)
	opts, err := c.PluginOptions(context.Background(), "@weiping/mbridge-page")
	if err != nil {
		t.Fatal(err)
	}
	if opts["period"] != "7d" {
		t.Fatalf("opts %+v", opts)
	}
	if magpie, _ := opts["mbridge"].(map[string]any); magpie == nil {
		t.Fatalf("opts %+v", opts)
	}
}

func TestPluginOptionsWithoutAnyIsEmpty(t *testing.T) {
	c := fakeCLI(t, "No options set. Its package suggests:\n{\n  \"period\": \"7d\"\n}\n", nil)
	opts, err := c.PluginOptions(context.Background(), "@weiping/mbridge-page")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 0 {
		t.Fatalf("opts %+v", opts)
	}
}

func TestPluginSetOptionsWritesTheWholeObject(t *testing.T) {
	var written []string
	c := fakeCLI(t, "{}", &written)
	err := c.PluginSetOptions(context.Background(), "@weiping/mbridge-page",
		map[string]any{"period": "today", "mbridge": map[string]any{"seq": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("written %v", written)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(written[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got["period"] != "today" {
		t.Fatalf("written %s", written[0])
	}
	if m, _ := got["mbridge"].(map[string]any); m == nil {
		t.Fatalf("written %s", written[0])
	}
}

func TestTheCommandIsReadFromTheOptions(t *testing.T) {
	opts := map[string]any{"cmd": map[string]any{"id": "restart", "seq": 7}}
	cmd := ReadPluginCommand(opts)
	if cmd == nil || cmd.ID != "restart" || cmd.Seq != 7 {
		t.Fatalf("cmd %+v", cmd)
	}
	if ReadPluginCommand(map[string]any{}) != nil {
		t.Fatal("a command out of nowhere")
	}
	if ReadPluginCommand(map[string]any{"cmd": map[string]any{"id": "", "seq": 1}}) != nil {
		t.Fatal("a command with no id")
	}
}

func TestTheCommandSurvivesTheJSONRoundTrip(t *testing.T) {
	// the options come back from magpie as JSON, so a sequence number
	// arrives as a float
	opts := map[string]any{"cmd": map[string]any{"id": "refresh", "seq": float64(9)}}
	cmd := ReadPluginCommand(opts)
	if cmd == nil || cmd.Seq != 9 {
		t.Fatalf("cmd %+v", cmd)
	}
}

func TestEachSideKeepsTheOtherSidesKeys(t *testing.T) {
	// the page wrote period and cmd, mbridge adds its own key
	current := map[string]any{"period": "30d", "cmd": map[string]any{"id": "refresh", "seq": 2}}
	next := MergePluginOptions(current, map[string]any{PluginOptionKey: map[string]any{"seq": 5}}, PluginOptionKey)
	if next["period"] != "30d" {
		t.Fatalf("the page's key was lost: %+v", next)
	}
	if next["cmd"] == nil {
		t.Fatalf("the page's command was lost: %+v", next)
	}
	if magpie, _ := next[PluginOptionKey].(map[string]any); magpie == nil {
		t.Fatalf("mbridge's key is missing: %+v", next)
	}
}

func TestTheSnapshotSequenceAndTime(t *testing.T) {
	snap := map[string]any{"seq": float64(12), "at": "2026-10-22T23:20:00.5Z"}
	if PluginSnapshotSeq(snap) != 12 {
		t.Fatalf("seq %d", PluginSnapshotSeq(snap))
	}
	at, ok := PluginSnapshotAt(snap)
	if !ok || at.Minute() != 20 {
		t.Fatalf("at %v %v", at, ok)
	}
	if _, ok := PluginSnapshotAt(map[string]any{}); ok {
		t.Fatal("a snapshot with no time")
	}
}

func TestTheSnapshotGoesUnderTheMbridgeKey(t *testing.T) {
	opts := map[string]any{"period": "7d"}
	SetPluginSnapshot(opts, map[string]any{"seq": 3})
	if PluginSnapshot(opts) == nil {
		t.Fatalf("opts %+v", opts)
	}
	if opts["period"] != "7d" {
		t.Fatalf("the page's key was lost: %+v", opts)
	}
}

func TestAMissingPluginIsNotAnErrorToReport(t *testing.T) {
	c := New("")
	c.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`magpie: no plugin "@weiping/mbridge-page"`), &Error{Op: "plugin options", Msg: `no plugin "@weiping/mbridge-page"`}
	}
	_, err := c.PluginOptions(context.Background(), "@weiping/mbridge-page")
	if err == nil {
		t.Fatal("no error")
	}
	if !ErrNoPlugin(err) {
		t.Fatalf("err %v", err)
	}
	if ErrNoPlugin(nil) {
		t.Fatal("nil is not a missing plugin")
	}
}

func TestTheOptionsCommandIsTheOneMagpieTakes(t *testing.T) {
	// the command must be the one magpie's CLI documents
	c := New("")
	var seen []string
	c.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		seen = args
		return []byte("{}"), nil
	}
	c.PluginOptions(context.Background(), "@weiping/mbridge-page")
	if strings.Join(seen, " ") != "plugin options @weiping/mbridge-page" {
		t.Fatalf("seen %v", seen)
	}
}
