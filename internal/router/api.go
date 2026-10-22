package router

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/weiping/magpie-bridge/internal/wire"
)

// logf writes to mbridge.log (serve points the standard logger there).
var logf = log.Printf

// PromptHash is §6.4's normalization: injected blocks stripped (wire.StripInjected), whitespace
// trimmed, CRLF folded to LF, then SHA-256.
func PromptHash(prompt string) string {
	p := wire.StripInjected(prompt)
	p = strings.ReplaceAll(p, "\r\n", "\n")
	p = strings.TrimSpace(p)
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:])
}

// DecideInput is one routing decision, shared by /turn and the gateway
// hook (§6.5: both paths call Decide, which alone reads and writes the
// session state).
type DecideInput struct {
	Session       string // the harness session id
	Key           string // state key: the session id on the harness path, session|firstWords in gateway mode
	Harness       string // claude-code | codex | pi | gateway
	Agent         string // main | gateway | a subagent type
	Prompt        string
	TurnID        string
	PlanMode      bool
	ParentSession string // §5.8: the harness resolved it (mod store, forked_from_thread_id)
	FirstWords    string // gateway mode: for the lineage fallback
	HasToolStats  bool   // the harness reported the counts itself
	ToolCalls     int
	ToolFailures  int
	Criteria      map[Tier]string // the request cwd's project criteria, over the config's
	// PreviousAnswer is the assistant's reply before Prompt (SP11)
	PreviousAnswer string
}

// Decided is Decide's result, with what the response and event carry.
type Decided struct {
	Tier       Tier
	Reason     string
	Source     string // jev | llm | default
	Confidence float64
	Arm        string // router | control
	LatencyMs  int
	Shadow     bool // control arm: Tier is the control tier; the router's own choice went to the event log
}

// Deps is what the /v1/bridge handlers (and the gateway hook, Task 6)
// run on.
type Deps struct {
	Config   Config
	Sessions *Sessions
	Hints    *Hints
	Classify Classifier
	Log      func(Event) error // defaults to Append

	// SP14 §A: what the process knows about itself and its surroundings.
	// The service manager and magpie are functions, so a test can answer
	// them without either.
	Version    string
	StartedAt  time.Time
	Listen     string
	Service    func(context.Context) ServiceStatus
	MagpieInfo func(context.Context) (string, bool)

	mu        sync.Mutex
	decisions []Event // ring of the last 20 decisions, for /v1/bridge/router
	live      map[string]Event // key → the last decision that judged it (SP14 §C)
}

// Register mounts the §6.4 endpoints. Auth needs nothing extra: the
// gateway wraps the whole mux in its loopback/LAN-key guard.
func Register(mux *http.ServeMux, deps *Deps) {
	if deps.Sessions == nil {
		deps.Sessions = NewSessions()
	}
	if deps.Hints == nil {
		deps.Hints = NewHints()
	}
	if deps.Log == nil {
		deps.Log = Append
	}
	mux.HandleFunc("POST /v1/bridge/turn", deps.turn)
	mux.HandleFunc("POST /v1/bridge/review", deps.review)
	mux.HandleFunc("POST /v1/bridge/feedback", deps.feedback)
	mux.HandleFunc("GET /v1/bridge/session", deps.session)
	mux.HandleFunc("POST /v1/bridge/lineage", deps.lineage)
	mux.HandleFunc("GET /v1/bridge/router", deps.status)
}

// Decide routes one turn: resolves the parent a derived session follows
// (§5.8), classifies unless the policy decides without a classification,
// Chooses, and — for the main or gateway agent, on any rule but R1 —
// commits the session state. Control-arm sessions get the control tier
// back with the router's own choice logged as a shadow decision.
func (d *Deps) Decide(ctx context.Context, in DecideInput) Decided {
	start := time.Now()
	cfg := d.Config
	arm := Arm(in.Session, cfg.Experiment)
	pc := cfg.PolicyConfig()

	// §5.8: an explicit parent beats the marked-firstWords fallback.
	parent := in.ParentSession
	if parent == "" {
		if p, ok := d.Sessions.ParentOf(in.Session, in.FirstWords); ok {
			parent = p
		}
	}
	if parent != "" {
		d.Sessions.InheritFrom(in.Key, parent)
	}

	prev := d.Sessions.Get(in.Key)
	calls, failures := in.ToolCalls, in.ToolFailures
	if !in.HasToolStats {
		calls, failures = d.Sessions.Stats(in.Session)
	}
	sinceLast := d.Sessions.SinceLast(in.Key)

	// Classify only when the policy would use it: R1 (fixed agent),
	// R2 (plan mode), and direct compaction commands/prompts decide without one.
	_, fixed := pc.FixedAgents[in.Agent]
	var classified *Verdict
	var classifyErr string
	var classifyMs int
	source := "default"
	isDirectCompact := IsCompactPrompt(in.Prompt)
	if isDirectCompact {
		source = "rule"
	} else if !fixed && !in.PlanMode && d.Classify != nil && in.Prompt != "" {
		q := Question{
			Message:        in.Prompt,
			PreviousTier:   "",
			Agent:          in.Agent,
			Criteria:       in.Criteria,
			PreviousAnswer: in.PreviousAnswer,
		}
		if q.Criteria == nil {
			q.Criteria = criteriaOf(cfg)
		}
		if prev != nil {
			q.PreviousTier = prev.Tier
		}
		cstart := time.Now()
		if v, err := d.Classify.Classify(ctx, q); err == nil {
			classified, source = v, classifySource(cfg.Classifier)
		} else {
			// say why: a timeout and an error magpie answered need
			// different fixes. Lengths only, never the prompt itself.
			classifyErr = classifyFailure(err)
			short := in.Session
			if len(short) > 8 {
				short = short[:8]
			}
			logf("classify failed: session %s %s agent %s, prompt %d runes, previous_answer %d runes, after %dms: %s",
				short, in.Harness, in.Agent, utf8.RuneCountInString(in.Prompt), utf8.RuneCountInString(in.PreviousAnswer),
				time.Since(cstart).Milliseconds(), classifyErr)
		}
		// the classifier's own time, success or failure (SP14 §E): the
		// Decide's LatencyMs includes it, and used to be the only number.
		classifyMs = int(time.Since(cstart).Milliseconds())
	}

	var review *ReviewVerdict
	if in.Agent == "main" {
		review = d.Sessions.TakeReview(in.Key)
	}
	decision := Choose(PolicyInput{
		Agent:        in.Agent,
		Review:       review,
		PlanMode:     in.PlanMode,
		IsCompact:    isDirectCompact,
		Classified:   classified,
		Prev:         prev,
		ToolCalls:    calls,
		ToolFailures: failures,
		SinceLast:    sinceLast,
		Now:          start,
	}, pc)

	// §5.2: only the main or gateway agent carries state, and R1 writes
	// nothing back.
	if (in.Agent == "main" || in.Agent == "gateway") && decision.Reason != "R1-fixed" {
		d.Sessions.Commit(in.Key, decision.Next)
	}

	res := Decided{
		Tier:      decision.Tier,
		Reason:    decision.Reason,
		Source:    source,
		Arm:       arm,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}
	if classified != nil {
		res.Confidence = classified.TierConfidence
	}
	turnID := in.TurnID
	if turnID == "" {
		turnID = generatedTurnID()
	}
	ev := Event{
		Kind: "decide", Session: in.Session, Harness: in.Harness, Agent: in.Agent,
		Tier: decision.Tier, Reason: decision.Reason, Source: source,
		Arm: arm, Confidence: res.Confidence, LatencyMs: res.LatencyMs,
		TurnID: turnID, WouldReview: decision.WouldReview,
		PreviousAnswer: classified != nil && in.PreviousAnswer != "",
		ClassifyError:  classifyErr,
		ClassifyMs:     classifyMs,
	}
	if classified != nil {
		ev.ClassifiedTier, ev.Classifier = classified.Tier, classified.Source
		c := classified.TierConfidence
		ev.TierConfidence = &c
		if prev != nil {
			v := classified.Dissatisfied
			ev.Dissatisfied = &v
		}
		if classified.Compact > 0 {
			cmp := classified.Compact
			ev.Compact = &cmp
		}
	}
	if review != nil {
		u, c := review.Unresolved, review.Confidence
		ev.Unresolved, ev.ReviewConfidence = &u, &c
	}
	if arm == "control" {
		res.Shadow, res.Tier = true, cfg.Experiment.ControlTier
		ev.Kind, ev.ShadowTier = "shadow", decision.Tier
	}
	d.log(ev)
	d.noteLive(in.Key, ev)
	return res
}

// noteLive remembers the decision that judged a key: the harness, the
// agent and the reason are not in the session state, only in the event
// (SP14 §C, 坑 3).
func (d *Deps) noteLive(key string, ev Event) {
	if ev.Kind != "decide" && ev.Kind != "shadow" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.live == nil {
		d.live = map[string]Event{}
	}
	d.live[key] = ev
}

// LiveWindow is how long a session counts as running (SP14 §C). It is a
// display window, not a storage policy.
const LiveWindow = 5 * time.Minute

// LiveCards are the sessions running right now, with the decision that
// judged each of them (SP14 §C).
func (d *Deps) LiveCards(window time.Duration) []LiveCard {
	live := []LiveSession{}
	if d.Sessions != nil {
		live = d.Sessions.Live(window)
	}
	cards := make([]LiveCard, 0, len(live))
	if len(live) == 0 {
		return cards
	}
	d.mu.Lock()
	events := make(map[string]Event, len(d.live))
	for k, v := range d.live {
		events[k] = v
	}
	d.mu.Unlock()
	for _, s := range live {
		card := LiveCard{LiveSession: s}
		if ev, ok := events[s.Key]; ok {
			card.Harness = ev.Harness
			card.Agent = ev.Agent
			card.Reason = ev.Reason
			card.TierConfidence = ev.TierConfidence
			card.LatencyMs = ev.LatencyMs
			card.ClassifyMs = ev.ClassifyMs
		}
		if tc, ok := d.Config.Tiers[s.Tier]; ok {
			card.Group = tc.Group
		}
		cards = append(cards, card)
	}
	return cards
}

// generatedTurnID names a turn the harness did not: "gw-" and 8 hex chars.
func generatedTurnID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("gw-%d", time.Now().UnixNano()&0xffffffff)
	}
	return "gw-" + hex.EncodeToString(b[:])
}

// classifySource names where a verdict came from (§6.4's source field).
func classifySource(classifier string) string {
	if classifier == "typesafe/jev-latest" {
		return "jev"
	}
	return "llm"
}

func criteriaOf(cfg Config) map[Tier]string {
	out := make(map[Tier]string, len(cfg.Tiers))
	for tier, tc := range cfg.Tiers {
		out[tier] = tc.Criteria
	}
	return out
}

// log records an event and keeps the last 20 decisions for status.
func (d *Deps) log(ev Event) {
	if d.Log != nil {
		_ = d.Log(ev)
	}
	if ev.Kind != "decide" && ev.Kind != "shadow" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.decisions = append(d.decisions, ev)
	if len(d.decisions) > 20 {
		d.decisions = d.decisions[len(d.decisions)-20:]
	}
}

// turn handles POST /v1/bridge/turn (§6.4).
func (d *Deps) turn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session       string `json:"session"`
		Prompt        string `json:"prompt"`
		Harness       string `json:"harness"`
		TurnID        string `json:"turn_id"`
		Agent         string `json:"agent"`
		PlanMode      bool   `json:"plan_mode"`
		Cwd           string `json:"cwd"`
		ParentSession string `json:"parent_session"`
		ToolCalls     *int   `json:"tool_calls"`
		ToolFailures  *int   `json:"tool_failures"`
		StoreHint     bool   `json:"store_hint"`
		// SP11: the assistant's reply before this prompt
		PreviousAnswer string `json:"previous_answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "not a /turn request", http.StatusBadRequest)
		return
	}
	if req.Session == "" || req.Prompt == "" {
		http.Error(w, "session and prompt are required", http.StatusBadRequest)
		return
	}
	if req.Agent == "" {
		req.Agent = "main"
	}
	hash := PromptHash(req.Prompt)
	in := DecideInput{
		Session: req.Session, Key: req.Session, Harness: req.Harness,
		Agent: req.Agent, Prompt: req.Prompt, TurnID: req.TurnID,
		PlanMode: req.PlanMode, ParentSession: req.ParentSession,
		Criteria:       ProjectCriteria(req.Cwd), // §4.6: the project file retunes criteria
		PreviousAnswer: req.PreviousAnswer,
	}
	if req.ToolCalls != nil && req.ToolFailures != nil {
		in.HasToolStats, in.ToolCalls, in.ToolFailures = true, *req.ToolCalls, *req.ToolFailures
	}
	res := d.Decide(r.Context(), in)
	if req.StoreHint {
		d.Hints.Put(Hint{Key: HintKey{Session: req.Session, TurnID: req.TurnID, PromptHash: hash}, Tier: res.Tier})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tier":         res.Tier,
		"group":        "group/" + d.Config.Tiers[res.Tier].Group,
		"claude_alias": d.Config.Tiers[res.Tier].ClaudeAlias,
		"reason":       res.Reason,
		"source":       res.Source,
		"confidence":   res.Confidence,
		"arm":          res.Arm,
		"latency_ms":   res.LatencyMs,
	})
}

// feedback handles POST /v1/bridge/feedback: logged, always 204.
func (d *Deps) feedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string `json:"session"`
		Kind    string `json:"kind"`
		Value   string `json:"value"`
		At      string `json:"at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.Session == "" || req.Kind == "" {
		http.Error(w, "session and kind are required", http.StatusBadRequest)
		return
	}
	if req.Kind == "manual_model_switch" {
		d.Sessions.MarkPinned(req.Session)
	}
	ev := Event{Kind: "feedback", Session: req.Session, Extra: req.Kind}
	if req.Value != "" {
		ev.Extra += " " + req.Value
	}
	if req.At != "" {
		ev.Time = req.At
	}
	_ = d.logEvent(ev)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) logEvent(ev Event) error {
	if d.Log != nil {
		return d.Log(ev)
	}
	return nil
}

// session handles GET /v1/bridge/session?id=…: the tier a fork subagent
// pins to (§5.8).
func (d *Deps) session(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	state := d.Sessions.Get(id)
	if state == nil {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	group := ""
	if tc, ok := d.Config.Tiers[state.Tier]; ok {
		group = "group/" + tc.Group
	}
	updated := ""
	if at, ok := d.Sessions.UpdatedAt(id); ok {
		updated = at.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tier": state.Tier, "group": group, "updated_at": updated,
	})
}

// lineage handles POST /v1/bridge/lineage: mark a session derived.
func (d *Deps) lineage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session       string `json:"session"`
		ParentSession string `json:"parent_session"`
		Source        string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.Session == "" {
		http.Error(w, "session is required", http.StatusBadRequest)
		return
	}
	d.Sessions.MarkDerived(req.Session, req.ParentSession)
	_ = d.logEvent(Event{Kind: "lineage", Session: req.Session, Extra: req.Source})
	w.WriteHeader(http.StatusNoContent)
}

// status handles GET /v1/bridge/router: what `mbridge router status` reads.
func (d *Deps) status(w http.ResponseWriter, r *http.Request) {
	tiers := map[string]map[string]string{}
	for tier, tc := range d.Config.Tiers {
		tiers[string(tier)] = map[string]string{"group": tc.Group, "claude_alias": tc.ClaudeAlias}
	}
	d.mu.Lock()
	decisions := append([]Event(nil), d.decisions...)
	d.mu.Unlock()
	// SP14 §A/§B: the process and its configuration, as they are in force
	v := d.StatusView()
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":        true,
		"router_group": d.Config.RouterGroup,
		// SP10: the clients wait this long for /turn
		"turn_budget_ms":   d.Config.TurnBudgetMs(),
		"tiers":            tiers,
		"experiment":       v.Config.Experiment,
		"decisions":        decisions,
		"version":          v.Version,
		"pid":              v.PID,
		"uptime_s":         v.UptimeS,
		"listen":           v.Listen,
		"listen_pending":   v.ListenPending,
		"magpie_version":   v.MagpieVersion,
		"magpie_ok":        v.MagpieOK,
		"service":          v.Service,
		"config_error":     v.ConfigError,
		"config":           v.Config,
		"live":             v.Live,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// classifyFailure names why a classification failed: the classify timeout,
// the client giving up first, or magpie's own error.
func classifyFailure(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout (classify_timeout_ms): " + err.Error()
	case errors.Is(err, context.Canceled):
		return "canceled: the client stopped waiting: " + err.Error()
	}
	return err.Error()
}
