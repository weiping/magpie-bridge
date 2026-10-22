package router

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weiping/magpie-bridge/internal/magpie"
)

// WindowKinds are the report windows the status page offers (SP14 §9.2).
var WindowKinds = []string{"1d", "7d", "30d"}

// DefaultWindow is the window a reader gets who asks for none. Not 1d:
// the experiment's SampleShort (an arm under 100 sessions) is almost
// always true in a day, and the page would only ever say "too few
// samples". `mbridge router report` keeps its own 14d.
const DefaultWindow = "7d"

// WindowKind is one of the three, or the default.
func WindowKind(s string) string {
	for _, k := range WindowKinds {
		if s == k {
			return k
		}
	}
	return DefaultWindow
}

// WindowLength is how far back a window reaches.
func WindowLength(kind string) time.Duration {
	switch WindowKind(kind) {
	case "1d":
		return 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// Coverage is how many of the turns asked the classifier (SP14 §E). The
// denominator is the turns that asked, not all of them: R1 (fixed
// agents), R2 (plan mode) and direct compaction commands never do.
type Coverage struct {
	Asked int `json:"asked"`
	Total int `json:"total"`
}

// ClassifyStats is what came of those questions.
type ClassifyStats struct {
	Asked      int               `json:"asked"`
	Failures   int               `json:"failures"`
	ErrorKinds map[string]int    `json:"error_kinds"`
	ErrorOne   map[string]string `json:"error_example"`
}

// WindowView is the aggregate over one window (SP14 §F/§G). The three
// are computed once every thirty seconds; the page only reads them.
type WindowView struct {
	Kind  string    `json:"kind"`
	Since time.Time `json:"since"`
	// Coverage and Classify are about the classifier.
	Coverage Coverage      `json:"coverage"`
	Classify ClassifyStats `json:"classify"`
	// the counts behind the distributions; the page draws the shares
	Tiers     map[string]int `json:"tiers"`
	Reasons   map[string]int `json:"reasons"`
	Harnesses map[string]int `json:"harnesses"`
	Arms      map[string]int `json:"arms"`
	Agents    map[string]int `json:"agents"`
	Hints     int            `json:"hints"`
	// both arms, and the three calibration tables
	Report    Report            `json:"report"`
	Calibrate []ScoreCalibration `json:"calibrate"`
	// PRStates says the merged-PR rate came from feedback alone: in
	// process mbridge cannot ask gh, `mbridge router report` can.
	PRStates string `json:"pr_states"`
}

// PRStatesFeedbackOnly is the mark the page shows next to the merge
// rate (SP14 §G).
const PRStatesFeedbackOnly = "feedback-only"

// ErrorKind is which of the three failures a classify_error is (SP14 §E).
// The third is magpie's own words, and is never folded into the first
// two: it needs a different fix.
func ErrorKind(msg string) string {
	switch {
	case strings.HasPrefix(msg, "timeout (classify_timeout_ms)"):
		return "timeout"
	case strings.HasPrefix(msg, "canceled: the client stopped waiting"):
		return "canceled"
	default:
		return "magpie"
	}
}

// readEvents is the incremental reader: it returns the events in the
// part of the file after offset, and the offset to read from next time.
// A file that got shorter (truncated, or replaced) starts over.
func readEvents(path string, offset int64) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if st.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}
	buf, err := io.ReadAll(io.NewSectionReader(f, offset, st.Size()-offset))
	if err != nil {
		return nil, offset, err
	}
	// only whole lines: a line still being written is read next tick
	cut := strings.LastIndexByte(string(buf), '\n')
	if cut < 0 {
		return nil, offset + int64(len(buf)), nil
	}
	var events []Event
	for _, line := range strings.Split(string(buf[:cut]), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events, offset + int64(cut) + 1, nil
}

// ReadEvents is router.jsonl read whole (the CLI's report).
func ReadEvents(path string) ([]Event, error) {
	events, _, err := readEvents(path, 0)
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	return events, err
}

// Aggregator computes the three windows from router.jsonl and magpie's
// ledger, on a timer (SP14 §3.2). Reading and aggregating on every page
// load would re-scan the whole log and re-run magpie's CLI each time.
type Aggregator struct {
	path   string
	every  time.Duration
	usage  func(context.Context, string) ([]magpie.UsageRow, error)
	now    func() time.Time
	// Config is the configuration the windows are computed against: the
	// calibration tables are read against the thresholds in force.
	Config *Config
	mu     sync.RWMutex
	offset int64
	events []Event
	wins   map[string]WindowView
}

// NewAggregator aggregates path's events with usage for the ledger.
func NewAggregator(path string, usage func(context.Context, string) ([]magpie.UsageRow, error), every time.Duration) *Aggregator {
	return &Aggregator{
		path: path, every: every, usage: usage, now: time.Now,
		wins: map[string]WindowView{},
	}
}

// Run aggregates every `every` until ctx is done.
func (a *Aggregator) Run(ctx context.Context) {
	t := time.NewTicker(a.every)
	defer t.Stop()
	a.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Tick(ctx)
		}
	}
}

// Tick aggregates once: the new lines of the log, then the windows.
func (a *Aggregator) Tick(ctx context.Context) {
	events, offset, err := readEvents(a.path, a.offset)
	if err != nil && !os.IsNotExist(err) {
		return // the log is unreadable for now; the next tick tries again
	}
	a.mu.Lock()
	a.offset = offset
	a.events = append(a.events, events...)
	all := append([]Event(nil), a.events...)
	a.mu.Unlock()

	var rows []magpie.UsageRow
	if a.usage != nil {
		// 30 days is the widest window; the narrower ones filter it
		if got, err := a.usage(ctx, "30d"); err == nil {
			for _, r := range got {
				if r.Session != "" {
					rows = append(rows, r)
				}
			}
		}
	}
	now := a.now()
	wins := map[string]WindowView{}
	for _, kind := range WindowKinds {
		wins[kind] = AggregateWindow(a.cfg(), all, rows, kind, now)
	}
	a.mu.Lock()
	a.wins = wins
	a.mu.Unlock()
}

// cfg is the configuration the windows are computed against; the
// aggregator reads it from the store it was given.
func (a *Aggregator) cfg() Config {
	if a.Config != nil {
		return *a.Config
	}
	return Config{}
}

// View is one window, computed already. An unknown kind is the default.
func (a *Aggregator) View(kind string) WindowView {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if v, ok := a.wins[WindowKind(kind)]; ok {
		return v
	}
	return WindowView{Kind: WindowKind(kind), Tiers: map[string]int{}, Reasons: map[string]int{},
		Harnesses: map[string]int{}, Arms: map[string]int{}, Agents: map[string]int{},
		Classify: ClassifyStats{ErrorKinds: map[string]int{}, ErrorOne: map[string]string{}},
		PRStates: PRStatesFeedbackOnly}
}

// AggregateWindow is the aggregate over one window: the counts, the
// classifier's share, both arms and the calibration tables (SP14 §F/§G).
func AggregateWindow(cfg Config, events []Event, rows []magpie.UsageRow, kind string, now time.Time) WindowView {
	kind = WindowKind(kind)
	since := now.Add(-WindowLength(kind))
	v := WindowView{
		Kind: kind, Since: since,
		Tiers: map[string]int{}, Reasons: map[string]int{}, Harnesses: map[string]int{},
		Arms: map[string]int{}, Agents: map[string]int{},
		Classify: ClassifyStats{ErrorKinds: map[string]int{}, ErrorOne: map[string]string{}},
		PRStates: PRStatesFeedbackOnly,
	}
	// only decide turns are turns: aux (Codex's thread title) and
	// proxy_passthrough are not (SP14 §F)
	decides := []Event{}
	for _, ev := range events {
		switch ev.Kind {
		case "hint_consumed":
			if at, ok := eventTime(ev); ok && at.After(since) {
				v.Hints++
			}
		case "decide", "shadow":
			if at, ok := eventTime(ev); ok && at.After(since) {
				decides = append(decides, ev)
			}
		}
	}
	for _, ev := range decides {
		v.Tiers[string(ev.Tier)]++
		if ev.Reason != "" {
			v.Reasons[ev.Reason]++
		}
		// a harness mbridge has not seen is its own name, not "other"
		if ev.Harness != "" {
			v.Harnesses[ev.Harness]++
		}
		if ev.Arm != "" {
			v.Arms[ev.Arm]++
		}
		if ev.Agent == "main" {
			v.Agents["main"]++
		} else {
			v.Agents["sub"]++
		}
		v.Coverage.Total++
		// Classifier empty = the classifier was never asked: that is the
		// only way to tell "not asked" from "asked and got nothing"
		if ev.Classifier == "" {
			continue
		}
		v.Coverage.Asked++
		v.Classify.Asked++
		if ev.ClassifyError == "" {
			continue
		}
		v.Classify.Failures++
		k := ErrorKind(ev.ClassifyError)
		v.Classify.ErrorKinds[k]++
		if _, ok := v.Classify.ErrorOne[k]; !ok {
			v.Classify.ErrorOne[k] = ev.ClassifyError
		}
	}
	var records []magpie.UsageRow
	for _, r := range rows {
		if r.Time.After(since) {
			records = append(records, r)
		}
	}
	// PRStates is nil: in process mbridge cannot ask gh, so the merged-PR
	// rate falls back to the feedback sessions and says so
	v.Report = Aggregate(ReportInput{Records: records, Events: decides, Now: now, Since: WindowLength(kind)})
	v.Calibrate = Calibrate(decides, cfg, CalibrateFilter{Since: since})
	return v
}

// eventTime is when an event happened.
func eventTime(ev Event) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, ev.Time)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}