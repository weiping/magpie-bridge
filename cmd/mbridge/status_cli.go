package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/weiping/magpie-bridge/internal/magpie"
	"github.com/weiping/magpie-bridge/internal/router"
)

// statusDecision is one line of the latest decisions.
type statusDecision struct {
	Session string `json:"session"`
	Tier    string `json:"tier"`
	Reason  string `json:"reason"`
}

// statusReport is what `mbridge status` concludes, in a shape a program
// can read (SP14 §3.2). Every field is one thing the text version prints;
// the text version adds no conclusion of its own.
type statusReport struct {
	ConfigPath     string           `json:"config_path"`
	ConfigError    string           `json:"config_error,omitempty"`
	ReviewMode     string           `json:"review_mode,omitempty"`
	MagpieURL      string           `json:"magpie_url,omitempty"`
	MagpieOK       bool             `json:"magpie_ok"`
	MagpieVersion  string           `json:"magpie_version,omitempty"`
	MagpieError    string           `json:"magpie_error,omitempty"`
	GroupsError    string           `json:"groups_error,omitempty"`
	MissingGroups  []string         `json:"missing_groups,omitempty"`
	UnlistedGroups []string         `json:"unlisted_groups,omitempty"`
	Running        bool             `json:"running"`
	BridgeState    string           `json:"bridge_state"` // running | down | invalid | unreadable
	Listen         string           `json:"listen,omitempty"`
	BridgeError    string           `json:"bridge_error,omitempty"`
	Decisions      []statusDecision `json:"decisions"`
}

// statusCmd is `mbridge status` (and `mbridge router status`): router.json,
// magpie, the tier groups in it, mbridge and its latest decisions. It
// reports what is wrong; it does not fail for it. --json prints the same
// conclusions as one JSON object and nothing else.
func statusCmd(args []string) error {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		return fmt.Errorf("status takes no arguments")
	}
	rep := collectStatus(context.Background())
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printStatus(rep)
	return nil
}

// collectStatus gathers what status says: router.json, magpie, the tier
// groups, and mbridge itself.
func collectStatus(ctx context.Context) statusReport {
	rep := statusReport{ConfigPath: routerJSONPath(), Decisions: []statusDecision{}}
	cfg, err := router.Load(routerJSONPath(), "")
	if err != nil {
		rep.ConfigError = err.Error()
		return rep
	}
	rep.MagpieURL = cfg.MagpieURL
	rep.Listen = cfg.Listen
	rep.ReviewMode = string(cfg.Review.Mode)

	mc := newMagpie(cfg.MagpieURL)
	if err := mc.Health(ctx); err != nil {
		rep.MagpieError = err.Error()
	} else {
		rep.MagpieOK = true
		rep.MagpieVersion, _ = mc.Version(ctx)
		groups, gerr := mc.Groups(ctx)
		if gerr != nil {
			rep.GroupsError = gerr.Error()
		}
		// a group magpie has but doesn't serve (every member skipped) is as
		// broken as a missing one: /v1/models leaves it out
		listed := map[string]bool{}
		models, lerr := mc.ModelList(ctx)
		for _, m := range models {
			listed[m.ID] = true
		}
		for _, g := range statusGroups(cfg) {
			switch {
			case gerr == nil && !slices.Contains(groups, g):
				rep.MissingGroups = append(rep.MissingGroups, g)
			case lerr == nil && !listed[magpie.GroupPrefix+g]:
				rep.UnlistedGroups = append(rep.UnlistedGroups, g)
			}
		}
	}

	c := &http.Client{Timeout: 2 * time.Second}
	res, err := c.Get("http://" + cfg.Listen + "/v1/bridge/router")
	if err != nil {
		// mbridge isn't there at all
		rep.BridgeState = "down"
		rep.BridgeError = err.Error()
		return rep
	}
	defer res.Body.Close()
	var body struct {
		Valid     bool             `json:"valid"`
		Error     string           `json:"error"`
		Decisions []statusDecision `json:"decisions"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		rep.BridgeState = "unreadable"
		rep.BridgeError = err.Error()
		return rep
	}
	if !body.Valid {
		rep.BridgeState = "invalid"
		rep.BridgeError = body.Error
		return rep
	}
	rep.BridgeState = "running"
	rep.Running = true
	rep.Decisions = body.Decisions
	return rep
}

// statusGroups are the groups mbridge needs magpie to serve.
func statusGroups(cfg router.Config) []string {
	want := []string{cfg.RouterGroup}
	for _, tier := range []router.Tier{router.TierFast, router.TierBalanced, router.TierPerformance} {
		want = append(want, cfg.Tiers[tier].Group)
	}
	return want
}

// printStatus says what collectStatus found, in the words it always used.
func printStatus(rep statusReport) {
	if rep.ConfigError != "" {
		fmt.Println(amber.Render("✗"), "router.json:", rep.ConfigError, muted.Render("· mbridge router init writes one"))
		return
	}
	fmt.Println(green.Render("✓"), "router.json", muted.Render(rep.ConfigPath), muted.Render("· review "+rep.ReviewMode))
	if rep.MagpieOK {
		fmt.Println(green.Render("✓"), "magpie at", rep.MagpieURL, muted.Render(rep.MagpieVersion))
	} else {
		fmt.Println(amber.Render("✗"), "magpie isn't answering at", rep.MagpieURL+":", rep.MagpieError, muted.Render("· start it: magpie serve, or its app"))
	}
	if rep.GroupsError != "" {
		fmt.Println(amber.Render("✗"), "magpie groups:", rep.GroupsError)
	}
	for _, g := range rep.MissingGroups {
		fmt.Println(amber.Render("✗"), "group/"+g, "missing in magpie", muted.Render("· mbridge router init --groups-only --force"))
	}
	for _, g := range rep.UnlistedGroups {
		explainUnlisted(context.Background(), newMagpie(rep.MagpieURL), "", g)
	}
	switch rep.BridgeState {
	case "running":
		fmt.Println(green.Render("✓"), "mbridge on", rep.Listen)
		if len(rep.Decisions) == 0 {
			fmt.Println(muted.Render("  no decisions yet"))
		}
		for _, d := range rep.Decisions {
			fmt.Printf("  %s → %s (%s)\n", d.Session, d.Tier, d.Reason)
		}
	case "invalid":
		fmt.Println(amber.Render("✗"), "mbridge on", rep.Listen, "runs without a router:", rep.BridgeError)
	case "unreadable":
		fmt.Println(amber.Render("✗"), "mbridge on", rep.Listen, "answered something else:", rep.BridgeError)
	default:
		fmt.Println(amber.Render("✗"), "mbridge isn't running on "+rep.Listen, muted.Render("· mbridge service install, or mbridge serve"))
	}
}

// versionCmd is `mbridge version`: mbridge's and the magpie it finds.
func versionCmd(args []string) error {
	fmt.Println("mbridge", version)
	if v, err := magpieFor().Version(context.Background()); err == nil && v != "" {
		fmt.Println(v)
	} else {
		fmt.Println(muted.Render("magpie: not found (" + fmt.Sprint(err) + ")"))
	}
	return nil
}
