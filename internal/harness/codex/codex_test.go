package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/weiping/magpie-bridge/internal/harness"
)

// The hooks keep per-session state in os.TempDir(), which reads TMPDIR on
// Unix and TMP/TEMP on Windows. Every test gets the package's own temp dir
// from TestMain, and a test that reads state another test may have written
// under the same session id takes a fresh one with isolateTemp.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mbridge-codex-test-")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		os.Setenv(k, dir)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func isolateTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
}

// fakeGateway records every path+body and answers /turn with scripted
// tiers, one per call.
type fakeGateway struct {
	turns []map[string]any
	posts []struct {
		path string
		body map[string]any
	}
}

func newFakeGateway(t *testing.T, replies ...map[string]any) (*fakeGateway, *harness.Client) {
	t.Helper()
	f := &fakeGateway{turns: replies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.posts = append(f.posts, struct {
			path string
			body map[string]any
		}{r.URL.Path, body})
		if r.URL.Path == "/v1/bridge/turn" {
			if len(f.turns) == 0 {
				w.WriteHeader(500)
				io.WriteString(w, `{"error":"no scripted reply"}`)
				return
			}
			reply := f.turns[0]
			f.turns = f.turns[1:]
			json.NewEncoder(w).Encode(reply)
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)
	return f, &harness.Client{Base: srv.URL, HTTP: srv.Client()}
}

func (f *fakeGateway) at(path string) []map[string]any {
	var out []map[string]any
	for _, p := range f.posts {
		if p.path == path {
			out = append(out, p.body)
		}
	}
	return out
}

func TestUserPromptRoutedModelCallsTurn(t *testing.T) {
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	stdin := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","turn_id":"t9","cwd":"/w","prompt":"what license?","model":"group/mbridge","permission_mode":"bypassPermissions"}`)
	out, err := UserPrompt(context.Background(), stdin, c)
	if err != nil || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	turns := f.at("/v1/bridge/turn")
	if len(turns) != 1 {
		t.Fatalf("turns: %d", len(turns))
	}
	body := turns[0]
	for k, want := range map[string]any{
		"harness":    "codex",
		"session":    "s1",
		"turn_id":    "t9",
		"cwd":        "/w",
		"prompt":     "what license?",
		"agent":      "main",
		"plan_mode":  false,
		"store_hint": true,
	} {
		if body[k] != want {
			t.Fatalf("%s: got %v, want %v", k, body[k], want)
		}
	}
}

func TestUserPromptPinnedModelReportsInsteadOfRouting(t *testing.T) {
	f, c := newFakeGateway(t)
	stdin := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","prompt":"hi","model":"gpt-6-sol"}`)
	if out, err := UserPrompt(context.Background(), stdin, c); out != nil || err != nil {
		t.Fatal("unexpected output")
	}
	if len(f.at("/v1/bridge/turn")) != 0 {
		t.Fatal("pinned model still routed")
	}
	fb := f.at("/v1/bridge/feedback")
	if len(fb) != 1 || fb[0]["kind"] != "manual_model_switch" || fb[0]["value"] != "gpt-6-sol" {
		t.Fatalf("feedback: %v", fb)
	}
}

func TestUserPromptCarriesForkedFromThread(t *testing.T) {
	f, c := newFakeGateway(t, map[string]any{"tier": "balanced", "group": "group/mb-balanced"})
	stdin := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s2","prompt":"go on","model":"group/mbridge","thread":{"forked_from_thread_id":"th-parent"}}`)
	UserPrompt(context.Background(), stdin, c)
	turns := f.at("/v1/bridge/turn")
	if len(turns) != 1 || turns[0]["parent_session"] != "th-parent" {
		t.Fatalf("parent: %v", turns)
	}
}

func TestUserPromptDefensiveSkips(t *testing.T) {
	f, c := newFakeGateway(t)
	for _, stdin := range []string{
		`not json at all`,
		`{"session_id":"","prompt":"hi"}`,
		`{"session_id":"s","prompt":""}`,
	} {
		if out, err := UserPrompt(context.Background(), []byte(stdin), c); out != nil || err != nil {
			t.Fatalf("%s: %v %v", stdin, out, err)
		}
	}
	if len(f.posts) != 0 {
		t.Fatalf("posts: %v", f.posts)
	}
}

func TestUserPromptTurnFailureStaysSilent(t *testing.T) {
	_, c := newFakeGateway(t) // no scripted replies → 500
	stdin := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s","prompt":"hi","model":"group/mbridge"}`)
	if out, err := UserPrompt(context.Background(), stdin, c); out != nil || err != nil {
		t.Fatal("failure was not silent")
	}
}

func TestPreAgentPinsModelAndKeepsParams(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"}, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s1", "t1", "group/mbridge")
	stdin := []byte(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"spawn_agent","tool_input":{"message":"search the repo","agent_type":"Explore"}}`)
	out, err := PreAgent(context.Background(), stdin, c)
	if err != nil || out == nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	var decoded struct {
		HookSpecificOutput struct {
			HookEventName      string         `json:"hookEventName"`
			PermissionDecision string         `json:"permissionDecision"`
			UpdatedInput       map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output not json: %v", err)
	}
	h := decoded.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "allow" {
		t.Fatalf("decision: %+v", h)
	}
	if h.UpdatedInput["message"] != "search the repo" || h.UpdatedInput["agent_type"] != "Explore" {
		t.Fatalf("params not kept: %v", h.UpdatedInput)
	}
	if h.UpdatedInput["model"] != "group/mb-fast" {
		t.Fatalf("model: %v", h.UpdatedInput["model"])
	}
	turns := f.at("/v1/bridge/turn")
	turns = turns[len(turns)-1:] // the spawn's, after the prompt's
	if len(turns) != 1 || turns[0]["agent"] != "Explore" || turns[0]["prompt"] != "search the repo" || turns[0]["store_hint"] != false {
		t.Fatalf("turn: %v", turns)
	}
}

func TestPreAgentDefaultsAgentType(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "balanced", "group": "group/mb-balanced"}, map[string]any{"tier": "balanced", "group": "group/mb-balanced"})
	promptOn(t, c, "s1", "t1", "group/mbridge")
	stdin := []byte(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"spawn_agent","tool_input":{"message":"do things"}}`)
	if _, err := PreAgent(context.Background(), stdin, c); err != nil {
		t.Fatal(err)
	}
	turns := f.at("/v1/bridge/turn")
	if last := turns[len(turns)-1]; last["agent"] != "default" {
		t.Fatalf("agent: %v", last["agent"])
	}
}

func TestPreAgentSkipsPresetModelAndFailures(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t)                  // failing gateway; the bare call still posts
	promptOn(t, c, "s", "t1", "group/mbridge") // its /turn fails: still the routing group
	f.posts = nil
	preset := []byte(`{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"spawn_agent","tool_input":{"message":"x","model":"gpt-6"}}`)
	if out, err := PreAgent(context.Background(), preset, c); out != nil || err != nil {
		t.Fatal("preset model was touched")
	}
	bare := []byte(`{"hook_event_name":"PreToolUse","session_id":"s","tool_name":"spawn_agent","tool_input":{"message":"x"}}`)
	if out, err := PreAgent(context.Background(), bare, c); out != nil || err != nil {
		t.Fatal("failure was not silent")
	}
	if len(f.posts) != 1 { // only the bare one reached the gateway
		t.Fatalf("posts: %v", f.posts)
	}
}

func TestPostBashFindsPRLinkAnywhere(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s1", "t1", "group/mbridge")
	stdin := []byte(`{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"Bash","whatever":{"nested":{"output":"Opened https://github.com/weiping/magpie-bridge/pull/12 today"}}}`)
	if out, err := PostBash(context.Background(), stdin, c); out != nil || err != nil {
		t.Fatal("unexpected output")
	}
	fb := f.at("/v1/bridge/feedback")
	if len(fb) != 1 || fb[0]["kind"] != "pr_created" || fb[0]["value"] != "https://github.com/weiping/magpie-bridge/pull/12" {
		t.Fatalf("feedback: %v", fb)
	}

	f2, c2 := newFakeGateway(t)
	PostBash(context.Background(), []byte(`{"session_id":"s1","output":"no links here"}`), c2)
	if len(f2.posts) != 0 {
		t.Fatalf("posts: %v", f2.posts)
	}
}

// ---- SP7: the Stop hook's end-of-turn review ----

// TestStopPostsReview: UserPrompt leaves this turn's prompt behind, Stop
// pairs it with the final answer and posts the review.
func TestStopPostsReview(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	prompt := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","turn_id":"t9","prompt":"read ./no-such-file.md and summarise","model":"group/mbridge"}`)
	if _, err := UserPrompt(context.Background(), prompt, c); err != nil {
		t.Fatal(err)
	}
	stop := []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t9","last_assistant_message":"I could not read it."}`)
	out, err := Stop(context.Background(), stop, c)
	if err != nil || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	rev := f.at("/v1/bridge/review")
	if len(rev) != 1 {
		t.Fatalf("reviews: %d", len(rev))
	}
	for k, want := range map[string]any{
		"session": "s1",
		"harness": "codex",
		"turn_id": "t9",
		"prompt":  "read ./no-such-file.md and summarise",
		"answer":  "I could not read it.",
	} {
		if rev[0][k] != want {
			t.Fatalf("%s: got %v, want %v", k, rev[0][k], want)
		}
	}
}

// TestStopWithoutPromptStateIsSilent: no state file (gateway mode, a
// pinned turn), no answer, or a different turn — nothing is posted.
func TestStopWithoutPromptStateIsSilent(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	for _, stdin := range []string{
		`{"hook_event_name":"Stop","session_id":"nobody","turn_id":"t1","last_assistant_message":"ok"}`,
		`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t1"}`,
		`not json at all`,
	} {
		if out, err := Stop(context.Background(), []byte(stdin), c); out != nil || err != nil {
			t.Fatalf("stdin %s: out=%v err=%v", stdin, out, err)
		}
	}
	// a state file from another turn does not pair with this answer
	if _, err := UserPrompt(context.Background(), []byte(`{"session_id":"s1","turn_id":"t1","prompt":"old","model":"group/mbridge"}`), c); err != nil {
		t.Fatal(err)
	}
	Stop(context.Background(), []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t2","last_assistant_message":"new answer"}`), c)
	if got := len(f.at("/v1/bridge/review")); got != 0 {
		t.Fatalf("stale state still posted %d reviews", got)
	}
}

// TestStopGatewayDownExitsQuietly: an unreachable gateway costs less than
// the 1s review budget and never errors.
func TestStopGatewayDownExitsQuietly(t *testing.T) {
	isolateTemp(t)
	c := &harness.Client{Base: "http://127.0.0.1:1", HTTP: http.DefaultClient}
	if _, err := UserPrompt(context.Background(), []byte(`{"session_id":"s1","turn_id":"t1","prompt":"hi","model":"group/mbridge"}`), c); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := Stop(context.Background(), []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t1","last_assistant_message":"ok"}`), c)
	if out != nil || err != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("Stop waited %v on a dead gateway", elapsed)
	}
}

// TestCodexHooksJSONHasStop: the shipped hooks file carries the Stop hook
// the CLI's `mbridge hook stop` serves.
func TestCodexHooksJSONHasStop(t *testing.T) {
	b, err := os.ReadFile("../../../clients/codex/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &hooks); err != nil {
		t.Fatalf("hooks.json: %v", err)
	}
	stops := hooks.Hooks["Stop"]
	if len(stops) != 1 || len(stops[0].Hooks) != 1 {
		t.Fatalf("Stop hooks: %+v", stops)
	}
	got := stops[0].Hooks[0]
	if got.Type != "command" || got.Command != "mbridge hook stop --harness codex" || got.Timeout != 2 {
		t.Fatalf("Stop hook: %+v", got)
	}
}

// SP10: Codex kills a hook at its "timeout" (seconds). The hooks that call
// /turn must outlive the longest turn budget mbridge can report (8000 ms),
// or a cold first turn is cut off before Jev answers.
func TestCodexTurnHooksOutliveTheTurnBudget(t *testing.T) {
	b, err := os.ReadFile("../../../clients/codex/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &hooks); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse"} {
		for _, g := range hooks.Hooks[event] {
			for _, h := range g.Hooks {
				if h.Timeout*1000 <= 8000 {
					t.Errorf("%s hook %q times out at %d s; /turn may take up to 8 s", event, h.Command, h.Timeout)
				}
			}
		}
	}
}

// SP11: Stop keeps the turn's final answer, and the next UserPromptSubmit
// sends it to /turn as previous_answer.
func TestNextTurnCarriesThePreviousAnswer(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t,
		map[string]any{"tier": "fast", "group": "group/mb-fast"},
		map[string]any{"tier": "performance", "group": "group/mb-perf"})
	turn := func(id, prompt string) {
		in := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","turn_id":"` + id + `","prompt":"` + prompt + `","model":"group/mbridge"}`)
		if _, err := UserPrompt(context.Background(), in, c); err != nil {
			t.Fatal(err)
		}
	}
	turn("t1", "CI 还要吗？")
	stop := []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t1","last_assistant_message":"提议：改三个工作流"}`)
	if _, err := Stop(context.Background(), stop, c); err != nil {
		t.Fatal(err)
	}
	turn("t2", "按这个思路修改")

	turns := f.at("/v1/bridge/turn")
	if len(turns) != 2 {
		t.Fatalf("turns: %d", len(turns))
	}
	if _, ok := turns[0]["previous_answer"]; ok {
		t.Fatalf("first turn sent a previous answer: %v", turns[0])
	}
	if turns[1]["previous_answer"] != "提议：改三个工作流" {
		t.Fatalf("second turn: %v", turns[1])
	}

	// another session never reads s1's answer
	in := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s2","turn_id":"u1","prompt":"hi","model":"group/mbridge"}`)
	f.turns = append(f.turns, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	if _, err := UserPrompt(context.Background(), in, c); err != nil {
		t.Fatal(err)
	}
	if last := f.at("/v1/bridge/turn"); last[len(last)-1]["previous_answer"] != nil {
		t.Fatalf("s2 got s1's answer: %v", last[len(last)-1])
	}
}

// A long answer is cut to 4000 characters before it is sent.
func TestPreviousAnswerSentIsBounded(t *testing.T) {
	long := ""
	for len([]rune(long)) < 9000 {
		long += "改动说明。"
	}
	if got := []rune(clientCut(long)); len(got) > 4010 {
		t.Fatalf("sent %d runes", len(got))
	}
}

// An answer is good for one prompt. A turn that ends without a Stop
// (interrupted) or with an empty answer leaves none for the turn after it:
// the answer before is not what the user now points at.
func TestPreviousAnswerIsOnlyForTheNextPrompt(t *testing.T) {
	isolateTemp(t)
	fast := map[string]any{"tier": "fast", "group": "group/mb-fast"}
	f, c := newFakeGateway(t, fast, fast, fast, fast)
	turn := func(id, prompt string) {
		in := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s1","turn_id":"` + id + `","prompt":"` + prompt + `","model":"group/mbridge"}`)
		if _, err := UserPrompt(context.Background(), in, c); err != nil {
			t.Fatal(err)
		}
	}
	stop := func(id, answer string) {
		in := []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"` + id + `","last_assistant_message":"` + answer + `"}`)
		if _, err := Stop(context.Background(), in, c); err != nil {
			t.Fatal(err)
		}
	}
	turn("t1", "重构方案？")
	stop("t1", "方案 A：大重构")
	turn("t2", "先看下 B") // interrupted: no Stop
	turn("t3", "按这个思路做")
	stop("t3", "") // ended with no answer
	turn("t4", "继续")

	turns := f.at("/v1/bridge/turn")
	if len(turns) != 4 {
		t.Fatalf("turns: %d", len(turns))
	}
	if turns[1]["previous_answer"] != "方案 A：大重构" {
		t.Fatalf("t2: %v", turns[1])
	}
	for _, i := range []int{2, 3} {
		if v, ok := turns[i]["previous_answer"]; ok {
			t.Fatalf("t%d got a stale answer %v", i+1, v)
		}
	}
}

// A file that names another session is never sent, even at this session's
// path.
func TestAnswerStateOfAnotherSessionIsIgnored(t *testing.T) {
	isolateTemp(t)
	if err := os.WriteFile(answerStatePath("s1"), []byte(`{"session":"s9","answer":"别人的"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readAnswerState("s1"); ok {
		t.Fatalf("read %q", got)
	}
}

// ---- SP13: only a turn on group/mbridge is routed ----

func promptOn(t *testing.T, c *harness.Client, session, turn, model string) {
	t.Helper()
	in := []byte(`{"hook_event_name":"UserPromptSubmit","session_id":"` + session + `","turn_id":"` + turn + `","prompt":"hi","model":"` + model + `"}`)
	if out, err := UserPrompt(context.Background(), in, c); out != nil || err != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

const bareSpawn = `{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"spawn_agent","tool_input":{"message":"search the repo"}}`
const prPost = `{"hook_event_name":"PostToolUse","session_id":"s1","tool_name":"Bash","output":"https://github.com/weiping/magpie-bridge/pull/12"}`

// magpie names the group "group/mbridge[1m]" for a 1M window.
func TestUserPromptRoutesTheSuffixedGroup(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s1", "t1", "group/mbridge[1m]")
	if len(f.at("/v1/bridge/turn")) != 1 || len(f.at("/v1/bridge/feedback")) != 0 {
		t.Fatalf("posts: %v", f.posts)
	}
}

// On any other model the spawn, the PR link and the review all stay quiet.
func TestOtherModelLeavesSpawnPRLinkAndReviewAlone(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s1", "t1", "group/auto-claude-sonnet-5-5")
	if out, err := PreAgent(context.Background(), []byte(bareSpawn), c); out != nil || err != nil {
		t.Fatalf("spawn: out=%v err=%v", out, err)
	}
	if out, err := PostBash(context.Background(), []byte(prPost), c); out != nil || err != nil {
		t.Fatalf("pr: out=%v err=%v", out, err)
	}
	stop := []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t1","last_assistant_message":"done"}`)
	if out, err := Stop(context.Background(), stop, c); out != nil || err != nil {
		t.Fatalf("stop: out=%v err=%v", out, err)
	}
	if len(f.at("/v1/bridge/turn")) != 0 || len(f.at("/v1/bridge/review")) != 0 {
		t.Fatalf("mbridge was asked on another model: %v", f.posts)
	}
	for _, fb := range f.at("/v1/bridge/feedback") {
		if fb["kind"] == "pr_created" {
			t.Fatalf("PR link reported on another model: %v", fb)
		}
	}
}

// A routed turn keeps doing all three.
func TestMbridgeModelStillRoutesSpawnAndReportsPRLink(t *testing.T) {
	isolateTemp(t)
	fast := map[string]any{"tier": "fast", "group": "group/mb-fast"}
	f, c := newFakeGateway(t, fast, fast)
	promptOn(t, c, "s1", "t1", "group/mbridge")
	out, err := PreAgent(context.Background(), []byte(bareSpawn), c)
	if err != nil || out == nil {
		t.Fatalf("spawn not routed: out=%v err=%v", out, err)
	}
	PostBash(context.Background(), []byte(prPost), c)
	if got := f.at("/v1/bridge/feedback"); len(got) != 1 || got[0]["kind"] != "pr_created" {
		t.Fatalf("feedback: %v", got)
	}
}

// No UserPromptSubmit seen for this session (or another session's): off.
func TestSpawnAndPRLinkWithoutAModeAreOff(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "other", "t1", "group/mbridge")
	if out, _ := PreAgent(context.Background(), []byte(bareSpawn), c); out != nil {
		t.Fatalf("spawn routed with no mode for its session: %s", out)
	}
	PostBash(context.Background(), []byte(prPost), c)
	if len(f.at("/v1/bridge/feedback")) != 0 {
		t.Fatalf("feedback: %v", f.at("/v1/bridge/feedback"))
	}
}

// A turn that is off must not pair its answer with the last routed turn's prompt.
func TestOffTurnDropsThePreviousPromptState(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s1", "t1", "group/mbridge")
	promptOn(t, c, "s1", "t2", "group/auto-claude-sonnet-5-5")
	stop := []byte(`{"hook_event_name":"Stop","session_id":"s1","turn_id":"t2","last_assistant_message":"done"}`)
	Stop(context.Background(), stop, c)
	if got := len(f.at("/v1/bridge/review")); got != 0 {
		t.Fatalf("an off turn posted %d reviews", got)
	}
}

// A session that never used mbridge is not a manual switch; leaving mbridge
// is, once; staying off is not, again.
func TestManualSwitchIsReportedOnlyWhenLeavingMbridge(t *testing.T) {
	isolateTemp(t)
	f, c := newFakeGateway(t, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	switches := func() int {
		n := 0
		for _, fb := range f.at("/v1/bridge/feedback") {
			if fb["kind"] == "manual_model_switch" {
				n++
			}
		}
		return n
	}
	promptOn(t, c, "s1", "t1", "group/auto-claude-sonnet-5-5")
	promptOn(t, c, "s1", "t2", "group/auto-claude-sonnet-5-5")
	if switches() != 0 {
		t.Fatalf("a session that started off reported %d switches", switches())
	}

	promptOn(t, c, "s2", "t1", "group/mbridge")
	promptOn(t, c, "s2", "t2", "gpt-6-sol")
	promptOn(t, c, "s2", "t3", "gpt-6-sol")
	if switches() != 1 {
		t.Fatalf("leaving mbridge reported %d switches, want 1", switches())
	}
	if fb := f.at("/v1/bridge/feedback"); fb[0]["value"] != "gpt-6-sol" {
		t.Fatalf("value: %v", fb[0])
	}

	// back on, then off again: a second switch
	f.turns = append(f.turns, map[string]any{"tier": "fast", "group": "group/mb-fast"})
	promptOn(t, c, "s2", "t4", "group/mbridge")
	promptOn(t, c, "s2", "t5", "gpt-6-sol")
	if switches() != 2 {
		t.Fatalf("switches: %d, want 2", switches())
	}
}
