package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/weiping/magpie-bridge/internal/magpie"
	"github.com/weiping/magpie-bridge/internal/router"
)

// SP12: `router init --native` builds the groups that route by magpie's own
// intent rules, with no mbridge in the path.
const (
	nativeGroup         = "mb-native"
	nativeBalancedGroup = "mb-native-balanced"
)

// nativeEfforts are the reasoning levels tried, in order, to tell a member
// that two tier groups share apart from its twin.
var nativeEfforts = []string{"medium", "low", "high", "xhigh"}

// effortWords are the suffixes magpie reads as a member's reasoning. Any
// other ":something" is part of the model's id (:free, :7b).
var effortWords = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// splitMember takes a group member "provider/model[:effort][:fast]" apart.
func splitMember(m string) (base, effort string, fast bool) {
	base, fast = strings.CutSuffix(m, ":fast")
	if i := strings.LastIndex(base, ":"); i >= 0 && effortWords[base[i+1:]] {
		base, effort = base[:i], base[i+1:]
	}
	return base, effort, fast
}

func joinMember(base, effort string, fast bool) string {
	if effort != "" {
		base += ":" + effort
	}
	if fast {
		base += ":fast"
	}
	return base
}

// nativeBalancedMembers is balanced's members with each one that perf has
// too (same id, suffix and all) given a reasoning suffix of its own. mb-native
// nests the tier groups, and a model in two of them stays only in the first
// one met: without this, the rule that sends to mb-perf would find its first
// members already taken by the group before it.
func nativeBalancedMembers(balanced, perf []string) []string {
	taken := map[string]bool{}
	for _, m := range perf {
		taken[m] = true
	}
	out := make([]string, 0, len(balanced))
	for _, m := range balanced {
		if !taken[m] || strings.HasPrefix(m, magpie.GroupPrefix) {
			out = append(out, m)
			continue
		}
		base, _, fast := splitMember(m)
		pick := m // no free suffix: it stays, and shares as before
		for _, e := range nativeEfforts {
			c := joinMember(base, e, fast)
			if !taken[c] && !slices.Contains(balanced, c) && !slices.Contains(out, c) {
				pick = c
				break
			}
		}
		out = append(out, pick)
	}
	return out
}

// routerInitNative makes mb-native-balanced and mb-native from the tier
// groups magpie has now and router.json's criteria. Like the rest of init
// it keeps what exists (have; empty under --force, when magpie replaces a
// group of the same name and drops its rules).
func routerInitNative(ctx context.Context, mc *magpie.Client, have map[string]bool) error {
	cfg, err := router.Load(routerJSONPath(), "")
	if err != nil {
		fmt.Println(amber.Render("!"), "native: can't read", routerJSONPath()+":", err)
		return nil
	}
	if cfg.Classifier == "" || cfg.Classifier == "local" {
		fmt.Println(amber.Render("!"), "native: router.json's classifier is "+fmt.Sprintf("%q", cfg.Classifier)+", not a model magpie can ask; the rules need one (typesafe/jev-latest, say).",
			"Set it and run `mbridge router init --preset <name> --groups-only --native` again: what exists is kept")
		return nil
	}
	fastG, balG, perfG := cfg.Tiers[router.TierFast].Group, cfg.Tiers[router.TierBalanced].Group, cfg.Tiers[router.TierPerformance].Group
	read := map[string][]string{}
	for _, id := range []string{fastG, balG, perfG} {
		g, err := mc.Group(ctx, id)
		if err != nil {
			return fmt.Errorf("native: reading group %s: %v", id, err)
		}
		for _, m := range g.Members {
			read[id] = append(read[id], m.Model)
		}
	}
	nb := nativeBalancedMembers(read[balG], read[perfG])
	// what is said about the members is for groups made now, not kept ones
	if !have[nativeBalancedGroup] {
		for i, m := range read[balG] {
			if nb[i] != m {
				fmt.Println(muted.Render("  "+nb[i]), muted.Render("(was "+m+": "+perfG+" has it too, and a model in two groups stays in the first)"))
			}
		}
	}
	if !have[nativeBalancedGroup] || !have[nativeGroup] {
		claimed := append(slices.Clone(read[perfG]), nb...)
		for _, m := range read[fastG] {
			if slices.Contains(claimed, m) {
				fmt.Println(amber.Render("!"), fastG, "shares", m, "with "+perfG+" or "+nativeBalancedGroup+"; in "+nativeGroup, "it counts only in the group before",
					muted.Render("(give one of them a :<effort> suffix to tell them apart)"))
			}
		}
	}

	gp := magpie.GroupPrefix
	// mb-native-balanced uses pace (matches mb-balanced's routing; the
	// two groups share members). mb-native, the router group that
	// picks a tier for a turn, uses order.
	groups := []struct {
		id      string
		members []string
		routing string
		stays   string
	}{
		{nativeBalancedGroup, nb, "pace", ""},
		{nativeGroup, []string{gp + perfG, gp + nativeBalancedGroup, gp + fastG}, "order", "turn"},
	}
	added := map[string]bool{}
	for _, g := range groups {
		if have[g.id] {
			fmt.Println(muted.Render("  kept"), "group", g.id, muted.Render("(already in magpie; `magpie group rm "+g.id+"` and init again to make it anew)"))
			continue
		}
		if err := mc.GroupAdd(ctx, g.id, g.members, g.routing); err != nil {
			return err
		}
		if g.stays != "" {
			if err := mc.GroupSet(ctx, g.id, "stays="+g.stays); err != nil {
				return err
			}
		}
		added[g.id] = true
		fmt.Println(green.Render("✓"), "group", g.id, "→", strings.Join(g.members, ", "))
	}

	// a group kept without any rule (an earlier run stopped half way) gets them
	if !added[nativeGroup] {
		g, err := mc.Group(ctx, nativeGroup)
		if err != nil {
			return fmt.Errorf("native: reading group %s: %v", nativeGroup, err)
		}
		if g.Rules > 0 {
			return nil
		}
	}
	target := map[router.Tier]string{
		router.TierFast: gp + fastG, router.TierBalanced: gp + nativeBalancedGroup, router.TierPerformance: gp + perfG,
	}
	fallback, ok := target[cfg.DefaultTier]
	if !ok {
		fallback = target[router.TierBalanced]
	}
	// SP15: the classifier may name a group (mb-classifier). magpie's rule
	// classifier= takes group/<id>; a bare name is refused ("knows no
	// model"). Prefix bare group names before they go into the rule.
	classifierRef := cfg.Classifier
	if !strings.Contains(classifierRef, "/") {
		classifierRef = gp + classifierRef
	}
	rules := [][]string{
		{"use=" + target[router.TierFast], "classifier=" + classifierRef, "intent=" + cfg.Tiers[router.TierFast].Criteria},
		{"use=" + target[router.TierBalanced], "intent=" + cfg.Tiers[router.TierBalanced].Criteria},
		{"use=" + target[router.TierPerformance], "intent=" + cfg.Tiers[router.TierPerformance].Criteria},
		{"use=" + fallback, "tokens=1"}, // anything the classifier did not place: default_tier
	}
	for _, r := range rules {
		if err := mc.GroupRuleAdd(ctx, nativeGroup, r...); err != nil {
			return err
		}
	}
	fmt.Println(green.Render("✓"), "rules on", nativeGroup+":", "fast, balanced and performance by what the message asks for, the rest to", fallback, muted.Render("(classifier "+cfg.Classifier+")"))
	fmt.Println(muted.Render("  use it:"), "group/"+nativeGroup, muted.Render("· codex -m group/"+nativeGroup+" (no -p mbridge), magpie claude group/"+nativeGroup))
	return nil
}
