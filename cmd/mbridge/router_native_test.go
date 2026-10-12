package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/weiping/magpie-bridge/internal/router"
)

// nativeHome is a fake magpie with the four groups in place and a
// router.json that names a classifier magpie can ask. mb-balanced and
// mb-perf share kimi-k3; mb-fast shares nothing.
func nativeHome(t *testing.T) *fakeMag {
	t.Helper()
	f := routerHome(t)
	if err := routerInit([]string{"--preset", "cn", "--groups-only"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.groups["mb-fast"] = []string{"deepseek/deepseek-flash:high", "deepseek/deepseek-v4-flash"}
	f.groups["mb-balanced"] = []string{"deepseek/deepseek-v4-pro", "moonshot-cn/kimi-k2.7-code", "moonshot-cn/kimi-k3"}
	f.groups["mb-perf"] = []string{"moonshot-cn/kimi-k3", "deepseek/deepseek-v4-pro:high", "moonshot-cn/kimi-k2.6"}
	f.mu.Unlock()
	editRouterJSON(t, func(m map[string]any) { m["classifier"] = "typesafe/jev-latest" })
	return f
}

// editRouterJSON changes router.json the way a user would, by hand.
func editRouterJSON(t *testing.T, edit func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(routerJSONPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ = json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(routerJSONPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func initNative(t *testing.T, extra ...string) string {
	t.Helper()
	out, err := captureStdout(t, func() error {
		return routerInit(append([]string{"--preset", "cn", "--groups-only", "--native"}, extra...))
	})
	if err != nil {
		t.Fatalf("init --native: %v\n%s", err, out)
	}
	return out
}

// rulesOf is a group's rules as the fake magpie holds them.
func rulesOf(f *fakeMag, id string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rules[id]
}

func countCalls(f *fakeMag, prefix ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := strings.Join(append([]string{"magpie"}, prefix...), " ") + " "
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c, " ")+" ", want) {
			n++
		}
	}
	return n
}

func TestRouterInitLeavesNativeAloneByDefault(t *testing.T) {
	f := nativeHome(t)
	if err := routerInit([]string{"--preset", "cn", "--groups-only"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := groupMembers(f, "mb-native"); ok {
		t.Fatal("mb-native made without --native")
	}
	if f.calledPrefix("group", "rule") || f.calledPrefix("group", "set") {
		t.Fatalf("native calls without --native: %v", f.calls)
	}
}

func TestRouterInitNativeBuildsTheGroups(t *testing.T) {
	f := nativeHome(t)
	before := map[string][]string{}
	for _, id := range []string{"mb-fast", "mb-balanced", "mb-perf", "mbridge"} {
		before[id], _ = groupMembers(f, id)
	}

	out := initNative(t)

	// balanced's copy tells the member it shares with perf apart
	nb, _ := groupMembers(f, "mb-native-balanced")
	if got := strings.Join(nb, ","); got != "deepseek/deepseek-v4-pro,moonshot-cn/kimi-k2.7-code,moonshot-cn/kimi-k3:medium" {
		t.Fatalf("mb-native-balanced: %s", got)
	}
	// perf first, so its members stay as they are
	if m, _ := groupMembers(f, "mb-native"); strings.Join(m, ",") != "group/mb-perf,group/mb-native-balanced,group/mb-fast" {
		t.Fatalf("mb-native: %v", m)
	}
	if f.stays["mb-native"] != "turn" {
		t.Fatalf("stays %q", f.stays["mb-native"])
	}
	if f.classifier["mb-native"] != "typesafe/jev-latest" {
		t.Fatalf("classifier %q", f.classifier["mb-native"])
	}
	cfg, err := router.Load(routerJSONPath(), "")
	if err != nil {
		t.Fatal(err)
	}
	crit := func(tier router.Tier) string { return "intent=" + cfg.Tiers[tier].Criteria }
	want := [][]string{
		{"use=group/mb-fast", "classifier=typesafe/jev-latest", crit(router.TierFast)},
		{"use=group/mb-native-balanced", crit(router.TierBalanced)},
		{"use=group/mb-perf", crit(router.TierPerformance)},
		{"use=group/mb-native-balanced", "tokens=1"},
	}
	got := rulesOf(f, "mb-native")
	if len(got) != len(want) {
		t.Fatalf("rules: %v", got)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Fatalf("rule %d = %q, want %q", i+1, got[i], want[i])
		}
	}
	// the groups mbridge routes with are not touched
	for id, m := range before {
		if now, _ := groupMembers(f, id); !slices.Equal(now, m) {
			t.Fatalf("%s changed: %v → %v", id, m, now)
		}
	}
	if !strings.Contains(out, "codex -m group/mb-native") || !strings.Contains(out, "kimi-k3:medium") {
		t.Fatalf("output says neither how to use it nor what it changed:\n%s", out)
	}
}

// The standards the rules ask the classifier about are the ones in
// router.json, edited or not, and the fallback follows default_tier.
func TestRouterInitNativeFollowsRouterJSON(t *testing.T) {
	for tier, group := range map[string]string{
		"fast": "group/mb-fast", "balanced": "group/mb-native-balanced", "performance": "group/mb-perf",
	} {
		t.Run(tier, func(t *testing.T) {
			f := nativeHome(t)
			editRouterJSON(t, func(m map[string]any) {
				m["default_tier"] = tier
				m["tiers"].(map[string]any)["fast"].(map[string]any)["criteria"] = "only quick questions"
			})
			initNative(t)
			rules := rulesOf(f, "mb-native")
			if len(rules) != 4 || rules[0][2] != "intent=only quick questions" {
				t.Fatalf("rules: %v", rules)
			}
			if !slices.Equal(rules[3], []string{"use=" + group, "tokens=1"}) {
				t.Fatalf("fallback for default_tier %s: %v", tier, rules[3])
			}
		})
	}
}

// Rules need a classifier magpie can ask; init says so and finishes the
// rest, rather than failing or making groups that route nothing.
func TestRouterInitNativeNeedsARealClassifier(t *testing.T) {
	f := nativeHome(t)
	editRouterJSON(t, func(m map[string]any) { m["classifier"] = "local" })
	out := initNative(t)
	if _, ok := groupMembers(f, "mb-native"); ok {
		t.Fatal("mb-native made without a classifier")
	}
	if _, ok := groupMembers(f, "mb-native-balanced"); ok {
		t.Fatal("mb-native-balanced made without a classifier")
	}
	if !strings.Contains(out, "classifier") || !strings.Contains(out, "--native") {
		t.Fatalf("init did not say what to do:\n%s", out)
	}
}

func TestRouterInitNativeKeepsWhatExists(t *testing.T) {
	f := nativeHome(t)
	initNative(t)
	// the user tunes both groups
	f.mu.Lock()
	f.groups["mb-native-balanced"] = []string{"deepseek/deepseek-v4-pro"}
	f.mu.Unlock()
	out := initNative(t)
	if m, _ := groupMembers(f, "mb-native-balanced"); strings.Join(m, ",") != "deepseek/deepseek-v4-pro" {
		t.Fatalf("second init replaced the user's mb-native-balanced: %v", m)
	}
	if n := countCalls(f, "group", "add", "mb-native"); n != 1 {
		t.Fatalf("mb-native added %d times", n)
	}
	if n := len(rulesOf(f, "mb-native")); n != 4 {
		t.Fatalf("rules after a second init: %d", n)
	}
	if !strings.Contains(out, "kept") {
		t.Fatalf("init did not say what it kept:\n%s", out)
	}
	// nothing was made, so nothing is said to have been changed; and --force
	// is not how to make them again
	if strings.Contains(out, "(was ") || strings.Contains(out, "--force makes") || !strings.Contains(out, "magpie group rm") {
		t.Fatalf("a run that kept both groups talked as if it had built them:\n%s", out)
	}
}

// A first run that stopped after the group (magpie refused a rule, say)
// leaves a group with no rules; the next run adds them to it.
func TestRouterInitNativeAddsRulesToAGroupWithNone(t *testing.T) {
	f := nativeHome(t)
	initNative(t)
	f.mu.Lock()
	delete(f.rules, "mb-native")
	delete(f.classifier, "mb-native")
	f.mu.Unlock()
	initNative(t)
	if n := countCalls(f, "group", "add", "mb-native"); n != 1 {
		t.Fatalf("mb-native added %d times", n)
	}
	if n := len(rulesOf(f, "mb-native")); n != 4 {
		t.Fatalf("rules: %d", n)
	}
}

// --force writes router.json's defaults back, and the classifier with them
// ("local" again): the native groups made before stay as they are, and init
// says the rules need a classifier again.
func TestRouterInitNativeAfterForceWaitsForTheClassifier(t *testing.T) {
	f := nativeHome(t)
	initNative(t)
	out := initNative(t, "--force")
	if !strings.Contains(out, "classifier") {
		t.Fatalf("init did not say the classifier is gone:\n%s", out)
	}
	if n := len(rulesOf(f, "mb-native")); n != 4 {
		t.Fatalf("rules after --force: %d", n)
	}
	if n := countCalls(f, "group", "add", "mb-native"); n != 1 {
		t.Fatalf("mb-native added %d times", n)
	}
}

// Taking the native groups out (magpie group rm) and running init --native
// again makes them from the tier groups and router.json as they are now.
func TestRouterInitNativeRebuildsAfterTheGroupsAreRemoved(t *testing.T) {
	f := nativeHome(t)
	initNative(t)
	f.mu.Lock()
	for _, id := range []string{"mb-native", "mb-native-balanced"} {
		delete(f.groups, id)
		delete(f.rules, id)
		delete(f.classifier, id)
		f.order = slices.DeleteFunc(f.order, func(o string) bool { return o == id })
	}
	f.groups["mb-balanced"] = []string{"moonshot-cn/kimi-k2.7-code"}
	f.mu.Unlock()
	editRouterJSON(t, func(m map[string]any) {
		m["tiers"].(map[string]any)["performance"].(map[string]any)["criteria"] = "the hard ones"
	})
	initNative(t)
	if m, _ := groupMembers(f, "mb-native-balanced"); strings.Join(m, ",") != "moonshot-cn/kimi-k2.7-code" {
		t.Fatalf("mb-native-balanced: %v", m)
	}
	rules := rulesOf(f, "mb-native")
	if len(rules) != 4 || rules[2][1] != "intent=the hard ones" {
		t.Fatalf("rules: %v", rules)
	}
}

// mb-fast is shared with the groups before it in mb-native; it is not
// changed (mbridge uses it too), but init says which members drop out.
func TestRouterInitNativeWarnsOfWhatFastShares(t *testing.T) {
	f := nativeHome(t)
	f.mu.Lock()
	f.groups["mb-fast"] = []string{"deepseek/deepseek-flash:high", "moonshot-cn/kimi-k2.6"} // also in mb-perf
	f.mu.Unlock()
	out := initNative(t)
	if m, _ := groupMembers(f, "mb-fast"); m[1] != "moonshot-cn/kimi-k2.6" {
		t.Fatalf("mb-fast changed: %v", m)
	}
	if !strings.Contains(out, "mb-fast") || !strings.Contains(out, "moonshot-cn/kimi-k2.6") {
		t.Fatalf("no warning about the shared member:\n%s", out)
	}
}

func TestNativeBalancedMembers(t *testing.T) {
	cases := []struct {
		name           string
		balanced, perf []string
		want           []string
	}{
		{"nothing shared", []string{"a/x", "b/y"}, []string{"c/z"}, []string{"a/x", "b/y"}},
		{"a shared member gets a suffix", []string{"a/x", "b/y"}, []string{"b/y", "c/z"}, []string{"a/x", "b/y:medium"}},
		{"same suffix on both counts as shared", []string{"b/y:high"}, []string{"b/y:high"}, []string{"b/y:medium"}},
		{"another suffix is another member", []string{"b/y:medium"}, []string{"b/y:high"}, []string{"b/y:medium"}},
		{"medium taken in perf: next one", []string{"b/y"}, []string{"b/y", "b/y:medium"}, []string{"b/y:low"}},
		{"fast mode kept", []string{"b/y:fast"}, []string{"b/y:fast"}, []string{"b/y:medium:fast"}},
		{"an id that ends like a suffix is not one", []string{"or/m:free"}, []string{"or/m:free"}, []string{"or/m:free:medium"}},
		{"two shared members", []string{"a/x", "b/y"}, []string{"b/y", "a/x"}, []string{"a/x:medium", "b/y:medium"}},
		{"a nested group is not a model", []string{"group/g"}, []string{"group/g"}, []string{"group/g"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nativeBalancedMembers(c.balanced, c.perf); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
