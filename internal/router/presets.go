package router

import (
	"strings"

	"github.com/weiping/magpie-bridge/internal/magpie"
)

// Preset is one §4.3 starter config: each tier's members in order, the
// first served one primary, the rest its failover. "<p>/" in a member
// means "whatever provider serves this model".
type Preset struct {
	ID    string
	Tiers map[Tier][]string // members in magpie group syntax
}

// presets holds §4.3's three presets (model ids as magpie's catalog names
// them on 2026-10-12, checked against the vendors' docs). A member nothing
// configured serves is left out, so a tier lists a few candidates and gets
// the ones the user has. A fast candidate costs less than the balanced
// tier's members: kimi-k2.7-code-highspeed ($1.90/$8) costs twice
// kimi-k2.7-code and so is no fast member.
var presets = map[string]Preset{
	"frontier": {ID: "frontier", Tiers: map[Tier][]string{
		TierFast:        {"<p>/deepseek-flash", "<p>/glm-5.3-flash:high", "<p>/gemini-3.8-flash:low", "<p>/gpt-6-luna:low"},
		TierBalanced:    {"<p>/gpt-6.1-sol:medium", "<p>/claude-sonnet-5-5:medium"},
		TierPerformance: {"<p>/gpt-6-astra:low", "<p>/claude-opus-5-5:high"},
	}},
	"anthropic": {ID: "anthropic", Tiers: map[Tier][]string{
		TierFast:        {"anthropic/claude-haiku-5-5", "openrouter/anthropic/claude-haiku-5.5"},
		TierBalanced:    {"anthropic/claude-sonnet-5-5:medium", "openrouter/anthropic/claude-sonnet-5.5:medium"},
		TierPerformance: {"anthropic/claude-opus-5-5:high", "openrouter/anthropic/claude-opus-5.5:high"},
	}},
	"cn": {ID: "cn", Tiers: map[Tier][]string{
		TierFast:        {"<p>/deepseek-flash", "<p>/glm-5.3-flash:high", "<p>/qwen3.8-flash:low"},
		TierBalanced:    {"<p>/glm-5.3:high", "<p>/kimi-k2.7-code", "<p>/deepseek-v4-pro:high"},
		TierPerformance: {"<p>/kimi-k3:high", "<p>/deepseek-v4-pro:max", "<p>/glm-5.3:max", "<p>/qwen3.8-max:xhigh"},
	}},
}

// Presets returns them by name.
func Presets() map[string]Preset { return presets }

// Resolve keeps the members served, the model ids magpie lists (GET
// /v1/models), turning "<p>/model[:effort]" into "provider/model[:effort]".
// A member nothing serves is reported unresolved and left out: magpie
// refuses a group naming a model it does not know, and "<p>" names no
// provider at all.
func (p Preset) Resolve(served []string) (resolved map[Tier][]string, unresolved []string) {
	resolved = map[Tier][]string{}
	for _, tier := range []Tier{TierFast, TierBalanced, TierPerformance} {
		seen := map[string]bool{}
		for _, member := range p.Tiers[tier] {
			m := member
			if strings.HasPrefix(m, "<p>/") {
				rest := m[4:] // model[:effort]
				base, _, _ := strings.Cut(rest, ":")
				id := providerFor(served, base)
				if id == "" {
					unresolved = append(unresolved, member)
					continue
				}
				m = id + "/" + rest
			} else if base, _, _ := strings.Cut(m, ":"); !servedID(served, base) {
				unresolved = append(unresolved, member)
				continue
			}
			if !seen[m] {
				seen[m] = true
				resolved[tier] = append(resolved[tier], m)
			}
		}
	}
	return resolved, unresolved
}

// servedID is whether served lists id ("provider/model").
func servedID(served []string, id string) bool {
	for _, s := range served {
		if s == id {
			return true
		}
	}
	return false
}

// providerFor is the first provider in served ("provider/model" ids)
// serving base; magpie's groups are not providers.
func providerFor(served []string, base string) string {
	for _, id := range served {
		prov, model, ok := strings.Cut(id, "/")
		if !ok || prov+"/" == magpie.GroupPrefix {
			continue
		}
		if model == base || strings.HasPrefix(model, base+"/") {
			return prov
		}
	}
	return ""
}
