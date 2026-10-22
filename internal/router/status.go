package router

import (
	"context"
	"os"
	"time"
)

// ServiceStatus is what the platform's service manager says about
// mbridge (SP14 §A).
type ServiceStatus struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Detail    string `json:"detail"`
}

// PageThresholds are the policy's cut-offs as they are in force.
type PageThresholds struct {
	TierMin             float64 `json:"tier_min"`
	DissatisfiedMin     float64 `json:"dissatisfied_min"`
	ReviewMin           float64 `json:"review_min"`
	ReviewConfidenceMin float64 `json:"review_confidence_min"`
	CompactMin          float64 `json:"compact_min"`
}

// PageReview is the end-of-turn review's configuration.
type PageReview struct {
	Mode           string `json:"mode"`
	TimeoutMs      int    `json:"timeout_ms"`
	MaxAnswerChars int    `json:"max_answer_chars"`
}

// PageExperiment is the experiment's configuration without its salt: the
// salt is how sessions are split between the arms, and it does not leave
// the process (SP14 §B).
type PageExperiment struct {
	Enabled       bool `json:"enabled"`
	RouterPercent int  `json:"router_percent"`
	ControlTier   Tier `json:"control_tier"`
}

// PageTier is one tier's configuration.
type PageTier struct {
	Group       string `json:"group"`
	ClaudeAlias string `json:"claude_alias"`
	Criteria    string `json:"criteria"`
}

// PageConfig is router.json as it is in force: the values with the
// defaults applied, not the file's text (SP14 §B).
type PageConfig struct {
	RouterGroup       string            `json:"router_group"`
	DefaultTier       Tier              `json:"default_tier"`
	Classifier        string            `json:"classifier"`
	ClassifyTimeoutMs int               `json:"classify_timeout_ms"`
	EscalateTurns     int               `json:"escalate_turns"`
	CacheTTLSeconds   int               `json:"cache_ttl_seconds"`
	Thresholds        PageThresholds    `json:"thresholds"`
	Review            PageReview        `json:"review"`
	Experiment        PageExperiment    `json:"experiment"`
	FixedAgents       map[string]Tier   `json:"fixed_agents"`
	Tiers             map[Tier]PageTier `json:"tiers"`
}

// PageView is the configuration as it is in force. The defaults are
// applied here too, so a config that never went through Load still shows
// what would be used.
func (c Config) PageView() PageConfig {
	c.defaults()
	pc := PageConfig{
		RouterGroup:       c.RouterGroup,
		DefaultTier:       c.DefaultTier,
		Classifier:        c.Classifier,
		ClassifyTimeoutMs: c.ClassifyTimeoutMs,
		EscalateTurns:     c.EscalateTurns,
		CacheTTLSeconds:   c.CacheTTLSeconds,
		Thresholds: PageThresholds{
			TierMin:             c.Thresholds.TierMin,
			DissatisfiedMin:     c.Thresholds.DissatisfiedMin,
			ReviewMin:           c.Thresholds.ReviewMin,
			ReviewConfidenceMin: c.Thresholds.ReviewConfidenceMin,
			CompactMin:          c.Thresholds.CompactMin,
		},
		Review: PageReview{
			Mode:           c.Review.Mode,
			TimeoutMs:      c.Review.TimeoutMs,
			MaxAnswerChars: c.Review.MaxAnswerChars,
		},
		Experiment: PageExperiment{
			Enabled:       c.Experiment.Enabled,
			RouterPercent: c.Experiment.RouterPercent,
			ControlTier:   c.Experiment.ControlTier,
		},
		FixedAgents: map[string]Tier{},
		Tiers:       map[Tier]PageTier{},
	}
	for agent, tier := range c.FixedAgents {
		pc.FixedAgents[agent] = tier
	}
	for tier, tc := range c.Tiers {
		pc.Tiers[tier] = PageTier{Group: tc.Group, ClaudeAlias: tc.ClaudeAlias, Criteria: tc.Criteria}
	}
	return pc
}

// StatusView is what mbridge knows about itself and its surroundings
// right now (SP14 §A).
type StatusView struct {
	Version       string        `json:"version"`
	PID           int           `json:"pid"`
	UptimeS       int64         `json:"uptime_s"`
	Listen        string        `json:"listen"`
	ListenPending bool          `json:"listen_pending"`
	MagpieVersion string        `json:"magpie_version"`
	MagpieOK      bool          `json:"magpie_ok"`
	Service       ServiceStatus `json:"service"`
	ConfigError   string        `json:"config_error"`
	Config        PageConfig    `json:"config"`
	Live          []LiveCard    `json:"live"`
}

// StatusView gathers the process's own state. The parts that need the
// outside world (magpie, the service manager) are functions on Deps, so
// a test can answer them without either.
func (d *Deps) StatusView() StatusView {
	v := StatusView{
		Version: d.Version,
		PID:     os.Getpid(),
		Listen:  d.Listen,
		Config:  d.Config.PageView(),
	}
	if !d.StartedAt.IsZero() {
		v.UptimeS = int64(time.Since(d.StartedAt).Seconds())
	}
	// the config names another address: it takes a restart to change
	if d.Listen != "" && d.Config.Listen != "" && d.Listen != d.Config.Listen {
		v.ListenPending = true
	}
	if d.MagpieInfo != nil {
		v.MagpieVersion, v.MagpieOK = d.MagpieInfo(context.Background())
	}
	if d.Service != nil {
		v.Service = d.Service(context.Background())
	}
	if err := ConfigError(); err != nil {
		v.ConfigError = err.Error()
	}
	v.Live = d.LiveCards(LiveWindow)
	return v
}
