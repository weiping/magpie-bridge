package router

import (
	"strings"
	"testing"
)

// served is what /v1/models lists for a magpie with DeepSeek and
// Kimi (China) configured (groups included, as magpie lists them).
var served = []string{"deepseek/deepseek-flash", "deepseek/deepseek-v4-pro", "moonshot-cn/kimi-k2.7-code", "group/mb-fast"}

// A "<p>/model" placeholder nothing configured serves is no member at all
// ("<p>" names no provider): it is reported, never written into a group,
// where it would sit as a member that can never answer ("no member ready").
func TestResolveDropsPlaceholdersNothingServes(t *testing.T) {
	resolved, unresolved := Presets()["frontier"].Resolve(served)
	for _, m := range resolved[TierPerformance] {
		if strings.HasPrefix(m, "<p>/") {
			t.Fatalf("performance kept the placeholder %q: %v", m, resolved[TierPerformance])
		}
	}
	if len(resolved[TierPerformance]) != 0 || !contains(unresolved, "<p>/gpt-6-astra:low") || !contains(unresolved, "<p>/claude-opus-5-5:high") {
		t.Fatalf("performance %v, unresolved %v", resolved[TierPerformance], unresolved)
	}
}

// A concrete member nothing serves is dropped too: magpie refuses a group
// with a model it does not know (`magpie knows no model "glm/glm-5.3-flash"`),
// and that stopped router init for anyone without that provider.
func TestResolveDropsConcreteMembersNothingServes(t *testing.T) {
	p := Preset{ID: "t", Tiers: map[Tier][]string{TierFast: {"glm/glm-5.3-flash:high", "deepseek/deepseek-flash:low"}}}
	resolved, unresolved := p.Resolve(served)
	if got := resolved[TierFast]; len(got) != 1 || got[0] != "deepseek/deepseek-flash:low" {
		t.Fatalf("fast: %v", got)
	}
	if len(unresolved) != 1 || unresolved[0] != "glm/glm-5.3-flash:high" {
		t.Fatalf("unresolved: %v", unresolved)
	}
}

// Every preset member is a model magpie listed: no glm/ (magpie's Zhipu
// provider is zhipu) and no retired id like kimi-k2.5.
func TestResolveCnServesEveryTierFromDeepSeekAndKimi(t *testing.T) {
	resolved, _ := Presets()["cn"].Resolve(append([]string{"moonshot-cn/kimi-k3"}, served...))
	want := map[Tier]string{TierFast: "deepseek/deepseek-flash", TierBalanced: "moonshot-cn/kimi-k2.7-code", TierPerformance: "moonshot-cn/kimi-k3:high"}
	for tier, first := range want {
		if got := resolved[tier]; len(got) == 0 || got[0] != first {
			t.Fatalf("%s: %v, want it led by %s", tier, got, first)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// A placeholder some provider serves resolves to that provider, its
// effort kept.
func TestResolveFillsServedPlaceholders(t *testing.T) {
	resolved, unresolved := Presets()["frontier"].Resolve(append([]string{"relay/gpt-6-astra"}, served...))
	if got := resolved[TierPerformance]; len(got) != 1 || got[0] != "relay/gpt-6-astra:low" {
		t.Fatalf("performance: %v", got)
	}
	if !contains(unresolved, "<p>/claude-opus-5-5:high") {
		t.Fatalf("unresolved: %v", unresolved)
	}
}

// A group magpie lists is never taken for a provider serving a model.
func TestResolveUsesServedModels(t *testing.T) {
	resolved, unresolved := Presets()["frontier"].Resolve([]string{"group/gpt-6-astra", "relay/gpt-6-astra"})
	if got := resolved[TierPerformance]; len(got) != 1 || got[0] != "relay/gpt-6-astra:low" {
		t.Fatalf("performance: %v (unresolved %v)", got, unresolved)
	}
}
