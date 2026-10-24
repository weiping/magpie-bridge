package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiping/magpie-bridge/internal/magpie"
)

// jsonMarshal is one event as a JSON line.
func jsonMarshal(ev Event) ([]byte, error) { return json.Marshal(ev) }

// httpGet reads a URL's JSON body into v.
func httpGet(url string) (map[string]any, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var body map[string]any
	return body, json.NewDecoder(res.Body).Decode(&body)
}

// decode reads a response body into v.
func decode(res *http.Response, v any) error {
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(v)
}

// winEvents are a day's worth of turns: two asked the classifier, one
// did not (R2), one ask timed out, one ask was refused by magpie.
func winEvents(now time.Time) []Event {
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	c := 0.82
	return []Event{
		{Kind: "decide", Time: at(2 * time.Hour), Session: "s1", Harness: "codex", Agent: "main",
			Arm: "router", Tier: TierBalanced, Reason: "R6-adopt", Source: "jev",
			Classifier: "typesafe/jev-latest", TierConfidence: &c},
		{Kind: "decide", Time: at(3 * time.Hour), Session: "s2", Harness: "claude-code", Agent: "main",
			Arm: "router", Tier: TierFast, Reason: "R2-plan-mode", Source: "rule"},
		{Kind: "decide", Time: at(4 * time.Hour), Session: "s3", Harness: "gateway", Agent: "gateway",
			Arm: "control", Tier: TierPerformance, Reason: "R6-lower", Classifier: "typesafe/jev-latest",
			ClassifyError: "timeout (classify_timeout_ms): context deadline exceeded"},
		{Kind: "decide", Time: at(5 * time.Hour), Session: "s4", Harness: "gateway", Agent: "gateway",
			Arm: "router", Tier: TierBalanced, Reason: "R6-adopt", Classifier: "deepseek/deepseek-flash#plain",
			ClassifyError: "magpie: upstream 502"},
		{Kind: "aux", Time: at(6 * time.Hour), Session: "s5", Harness: "codex", Agent: "gateway",
			Tier: TierFast, Reason: "codex-title"},
		{Kind: "proxy_passthrough", Time: at(6 * time.Hour), Session: "s6", Harness: "codex"},
		{Kind: "hint_consumed", Time: at(6 * time.Hour), Session: "s1", Harness: "codex"},
		{Kind: "decide", Time: at(3 * 24 * time.Hour), Session: "week", Harness: "codex", Agent: "main",
			Arm: "router", Tier: TierFast, Reason: "R6-adopt"},
		{Kind: "decide", Time: at(10 * 24 * time.Hour), Session: "old", Harness: "codex", Agent: "main",
			Arm: "router", Tier: TierFast, Reason: "R6-adopt"},
	}
}

// TestTheReasonHistogramSkipsAuxAndPassthrough: a thread title is not a
// turn, and neither is a body too big to route (SP14 §F).
func TestTheReasonHistogramSkipsAuxAndPassthrough(t *testing.T) {
	now := time.Now()
	v := AggregateWindow(Config{}, winEvents(now), nil, "7d", now)
	if v.Tiers[string(TierFast)] != 2 {
		t.Fatalf("tiers %v", v.Tiers)
	}
	if v.Reasons["R6-adopt"] != 3 || v.Reasons["R2-plan-mode"] != 1 {
		t.Fatalf("reasons %v", v.Reasons)
	}
	if v.Reasons["codex-title"] != 0 {
		t.Fatalf("aux counted as a turn: %v", v.Reasons)
	}
	if v.Hints != 1 {
		t.Fatalf("hints %d", v.Hints)
	}
	if v.Coverage.Total != 5 {
		t.Fatalf("coverage %+v", v.Coverage)
	}
}

// TestTheCoverageDenominatorIsTheTurnsThatAsked: R2 and the compaction
// rule never ask the classifier, so "below 100%" is normal (SP14 §E).
func TestTheCoverageDenominatorIsTheTurnsThatAsked(t *testing.T) {
	now := time.Now()
	v := AggregateWindow(Config{}, winEvents(now), nil, "7d", now)
	if v.Coverage.Asked != 3 || v.Coverage.Total != 5 {
		t.Fatalf("coverage %+v", v.Coverage)
	}
}

// TestTheClassifyFailuresFallIntoThreeKinds: a timeout and a client that
// stopped waiting need different fixes; anything else is magpie's own
// words and is not folded into either (SP14 §E).
func TestTheClassifyFailuresFallIntoThreeKinds(t *testing.T) {
	now := time.Now()
	events := winEvents(now)
	events = append(events, Event{Kind: "decide", Time: now.Add(-time.Hour).Format(time.RFC3339),
		Session: "s7", Harness: "codex", Agent: "main", Tier: TierFast, Reason: "R7-carry",
		Classifier:    "typesafe/jev-latest",
		ClassifyError: "canceled: the client stopped waiting: context canceled"})
	v := AggregateWindow(Config{}, events, nil, "7d", now)
	if v.Classify.Failures != 3 {
		t.Fatalf("failures %+v", v.Classify)
	}
	for kind, want := range map[string]int{"timeout": 1, "canceled": 1, "magpie": 1} {
		if v.Classify.ErrorKinds[kind] != want {
			t.Fatalf("%s %v", kind, v.Classify.ErrorKinds)
		}
	}
	if !strings.Contains(v.Classify.ErrorOne["magpie"], "502") {
		t.Fatalf("magpie's own words were rewritten: %q", v.Classify.ErrorOne["magpie"])
	}
}

// TestTheWindowsReachFurtherBack: 1d ≤ 7d ≤ 30d for the same events.
func TestTheWindowsReachFurtherBack(t *testing.T) {
	now := time.Now()
	one := AggregateWindow(Config{}, winEvents(now), nil, "1d", now)
	seven := AggregateWindow(Config{}, winEvents(now), nil, "7d", now)
	thirty := AggregateWindow(Config{}, winEvents(now), nil, "30d", now)
	if one.Coverage.Total >= seven.Coverage.Total {
		t.Fatalf("1d %+v 7d %+v", one.Coverage, seven.Coverage)
	}
	if seven.Coverage.Total >= thirty.Coverage.Total {
		t.Fatalf("7d %+v 30d %+v", seven.Coverage, thirty.Coverage)
	}
	if thirty.Tiers[string(TierFast)] != 3 {
		t.Fatalf("30d tiers %v", thirty.Tiers)
	}
}

// TestAnUnknownWindowIsTheDefault: a page that asks for 14d gets 7d.
func TestAnUnknownWindowIsTheDefault(t *testing.T) {
	if WindowKind("14d") != DefaultWindow {
		t.Fatalf("window %q", WindowKind("14d"))
	}
	if WindowKind("30d") != "30d" {
		t.Fatalf("window %q", WindowKind("30d"))
	}
	if WindowLength("30d") != 30*24*time.Hour {
		t.Fatalf("length %v", WindowLength("30d"))
	}
}

// TestTheAggregateSaysTheMergeRateIsFromFeedbackOnly: in process mbridge
// cannot ask gh (SP14 §G).
func TestTheAggregateSaysTheMergeRateIsFromFeedbackOnly(t *testing.T) {
	now := time.Now()
	v := AggregateWindow(Config{}, winEvents(now), nil, "7d", now)
	if v.PRStates != PRStatesFeedbackOnly {
		t.Fatalf("pr states %q", v.PRStates)
	}
}

// TestTheAggregatorReadsTheLogOnce: the second tick reads only the new
// lines, and the windows grow (SP14 §3.2).
func TestTheAggregatorReadsTheLogOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "router.jsonl")
	now := time.Now()
	a := NewAggregator(path, nil, time.Hour)
	a.now = func() time.Time { return now }
	writeLog(t, path, winEvents(now)[:2])
	a.Tick(context.Background())
	if got := a.View("7d").Coverage.Total; got != 2 {
		t.Fatalf("first tick %d", got)
	}
	writeLog(t, path, winEvents(now)[2:4])
	a.Tick(context.Background())
	if got := a.View("7d").Coverage.Total; got != 4 {
		t.Fatalf("second tick %d", got)
	}
	// and again: nothing new, nothing counted twice
	a.Tick(context.Background())
	if got := a.View("7d").Coverage.Total; got != 4 {
		t.Fatalf("third tick %d", got)
	}
}

// TestTheAggregatorReadsMagpiesLedgerOnce: the usage rows are filtered
// per window, so the widest window covers them all.
func TestTheAggregatorReadsMagpiesLedgerOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var periods []string
	a := NewAggregator(filepath.Join(dir, "router.jsonl"),
		func(ctx context.Context, period string) ([]magpie.UsageRow, error) {
			periods = append(periods, period)
			return []magpie.UsageRow{
				{Session: "s1", Time: now.Add(-2 * time.Hour), CostUSD: 1, Priced: true, Status: 200},
				{Session: "old", Time: now.Add(-10 * 24 * time.Hour), CostUSD: 5, Priced: true, Status: 200},
			}, nil
		}, time.Hour)
	a.now = func() time.Time { return now }
	writeLog(t, filepath.Join(dir, "router.jsonl"), winEvents(now))
	a.Tick(context.Background())

	if len(periods) != 1 || periods[0] != "30d" {
		t.Fatalf("periods %v", periods)
	}
	if a.View("1d").Report.Router.TotalCost != 1 {
		t.Fatalf("1d cost %+v", a.View("1d").Report.Router)
	}
	if a.View("30d").Report.Router.TotalCost != 6 {
		t.Fatalf("30d cost %+v", a.View("30d").Report.Router)
	}
}

// TestTheEndpointReadsTheWindowItIsAskedFor: window moves the aggregate
// and nothing else (SP14 §3.2).
func TestTheEndpointReadsTheWindowItIsAskedFor(t *testing.T) {
	d, _, _ := testDeps(t, nil)
	now := time.Now()
	d.Windows = func(kind string) WindowView {
		return AggregateWindow(d.Config, winEvents(now), nil, kind, now)
	}
	srv := testServer(d)
	defer srv.Close()

	body, err := httpGet(srv.URL + "/v1/bridge/router?window=30d")
	if err != nil {
		t.Fatal(err)
	}
	w, ok := body["window"].(map[string]any)
	if !ok {
		t.Fatalf("window %v", body["window"])
	}
	if w["kind"] != "30d" {
		t.Fatalf("kind %v", w["kind"])
	}
	// the sections that are "now" do not move with the window
	for _, key := range []string{"config", "live", "pid", "version"} {
		if _, ok := body[key]; !ok {
			t.Errorf("the endpoint lacks %q", key)
		}
	}
}

// writeLog appends events as JSON lines.
func writeLog(t *testing.T, path string, events []Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, ev := range events {
		b, err := jsonMarshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAFailedClassificationIsStillAnAskedOne: a turn whose classification
// failed has no Classifier (there was no verdict) but it did ask, and the
// failure is counted among the asked ones (SP14 §E). Counting only the
// answered ones would make the failure rate always zero.
func TestAFailedClassificationIsStillAnAskedOne(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Kind: "decide", Time: now.Add(-time.Hour).Format(time.RFC3339), Session: "s1",
			Harness: "codex", Agent: "main", Tier: TierBalanced, Reason: "R7-carry",
			ClassifyError: "timeout (classify_timeout_ms): context deadline exceeded"},
		{Kind: "decide", Time: now.Add(-2 * time.Hour).Format(time.RFC3339), Session: "s2",
			Harness: "codex", Agent: "main", Tier: TierFast, Reason: "R2-plan-mode", Source: "rule"},
	}
	v := AggregateWindow(Config{}, events, nil, "7d", now)
	if v.Coverage.Total != 2 {
		t.Fatalf("total %d", v.Coverage.Total)
	}
	if v.Coverage.Asked != 1 {
		t.Fatalf("asked %d", v.Coverage.Asked)
	}
	if v.Classify.Failures != 1 {
		t.Fatalf("failures %d", v.Classify.Failures)
	}
	if v.Classify.ErrorKinds["timeout"] != 1 {
		t.Fatalf("error kinds %v", v.Classify.ErrorKinds)
	}
}

// TestTheWindowKeepsTheLastTurns: the classifier's panel shows the last
// turns of the window (SP14 §E).
func TestTheWindowKeepsTheLastTurns(t *testing.T) {
	now := time.Now()
	var events []Event
	for i := 0; i < 15; i++ {
		events = append(events, Event{Kind: "decide",
			Time:    now.Add(-time.Duration(15-i) * time.Hour).Format(time.RFC3339),
			Session: fmt.Sprintf("s-%02d", i), Harness: "codex", Agent: "main",
			Tier: TierBalanced, Reason: "R6-adopt", Classifier: "typesafe/jev-latest"})
	}
	v := AggregateWindow(Config{}, events, nil, "7d", now)
	if len(v.Turns) != 10 {
		t.Fatalf("turns %d", len(v.Turns))
	}
	if v.Turns[len(v.Turns)-1].Session != "s-14" {
		t.Fatalf("the newest turn is %q", v.Turns[len(v.Turns)-1].Session)
	}
}

// TestTheWindowOnlyChangesTheAggregateSections: the window moves the
// aggregate and nothing else. The process, the configuration, the live
// cards and the decisions are "now": a reader who asks for 1d and a
// reader who asks for 30d must see the same ones (SP14 §3.1/§3.2).
func TestTheWindowOnlyChangesTheAggregateSections(t *testing.T) {
	d, _, _ := testDeps(t, nil)
	now := time.Now()
	d.Windows = func(kind string) WindowView {
		return AggregateWindow(d.Config, winEvents(now), nil, kind, now)
	}
	one := d.StatusPayload("1d")
	thirty := d.StatusPayload("30d")

	if kind := one["window"].(WindowView).Kind; kind != "1d" {
		t.Fatalf("the 1d window says %v", kind)
	}
	if kind := thirty["window"].(WindowView).Kind; kind != "30d" {
		t.Fatalf("the 30d window says %v", kind)
	}
	for key, a := range one {
		if key == "window" {
			continue
		}
		b, ok := thirty[key]
		if !ok {
			t.Errorf("the 30d payload lacks %q", key)
			continue
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%q moved with the window: %v vs %v", key, a, b)
		}
	}
	// and the aggregate itself does move, or the comparison above is vacuous
	if reflect.DeepEqual(one["window"], thirty["window"]) {
		t.Error("the aggregate did not move with the window")
	}
}

// tableByScore finds one score's calibration table.
func tableByScore(tables []ScoreCalibration, score string) ScoreCalibration {
	for _, t := range tables {
		if t.Score == score {
			return t
		}
	}
	return ScoreCalibration{}
}

// totalN is how many turns a table holds: the sum of its bands.
func totalN(sc ScoreCalibration) int {
	n := 0
	for _, b := range sc.Bands {
		n += b.N
	}
	return n
}

// TestTheConfidenceMeanSkipsTheTurnsThatDidNotAsk: the tier score is
// the classifier's confidence, so a turn that never asked it has no
// sample in it. A turn with no confidence, and a turn whose pick the
// router did not follow, are not zeros in the mean — they are not in it.
// The dissatisfaction score is a different thing: a turn that did not
// ask can still be dissatisfied, and that turn belongs in the
// dissatisfied table (SP14 §E, SP7 §5.2).
func TestTheConfidenceMeanSkipsTheTurnsThatDidNotAsk(t *testing.T) {
	conf, dis := 0.9, 0.8
	events := []Event{
		// asked, and the router followed the pick: a sample of the tier score
		{Kind: "decide", Session: "s1", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierFast, ClassifiedTier: TierFast, Reason: "R6-adopt",
			Classifier: "typesafe/jev-latest", TierConfidence: &conf},
		// never asked: no confidence at all, so no sample
		{Kind: "decide", Session: "s2", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierBalanced, Reason: "R2-plan-mode", Source: "rule"},
		// asked, but the pick was not followed: this confidence is not the
		// score of a turn that ran on it
		{Kind: "decide", Session: "s3", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierFast, ClassifiedTier: TierPerformance, Reason: "R6-lower",
			Classifier: "typesafe/jev-latest", TierConfidence: &conf},
		// never asked, but the user was dissatisfied: this turn belongs in
		// the dissatisfied table
		{Kind: "decide", Session: "s4", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierBalanced, Reason: "R2-plan-mode", Source: "rule", Dissatisfied: &dis},
	}
	tables := Calibrate(events, testConfig(""), CalibrateFilter{})
	tier := tableByScore(tables, "tier")
	diss := tableByScore(tables, "dissatisfied")

	if n := totalN(tier); n != 1 {
		t.Fatalf("the tier score holds %d turns, want only the asked-and-followed one: %v", n, tier.Bands)
	}
	if n := totalN(diss); n != 1 {
		t.Fatalf("the dissatisfied score holds %d turns, want only the dissatisfied one: %v", n, diss.Bands)
	}
	// a table with one turn is not a long enough sample to draw a rate
	// from: the page must say "—" instead of 0%
	if !tier.SampleShort {
		t.Error("a table with one turn claims to be long enough")
	}
}

// TestTheCalibrationTableLeavesOutTheTurnsThatWereNotFollowed: a turn
// the router took somewhere other than the classifier's pick is not a
// sample of the tier score — the confidence belongs to a tier the turn
// did not run on. R2-plan-mode never asked at all, R6-lower asked and
// was talked out of the answer. Both stay out of every band of the tier
// table, and both stay in the dissatisfied table (SP7 §5.2).
func TestTheCalibrationTableLeavesOutTheTurnsThatWereNotFollowed(t *testing.T) {
	conf, dis := 0.9, 0.9
	events := []Event{
		// followed the pick: the one turn the tier table may hold
		{Kind: "decide", Session: "s1", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierFast, ClassifiedTier: TierFast, Reason: "R6-adopt",
			Classifier: "typesafe/jev-latest", TierConfidence: &conf},
		// plan mode: never asked, so it has no confidence at all
		{Kind: "decide", Session: "s2", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierBalanced, Reason: "R2-plan-mode", Source: "rule", Dissatisfied: &dis},
		// the router went below the classifier's pick
		{Kind: "decide", Session: "s3", Harness: "codex", Agent: "main", Arm: "router",
			Tier: TierFast, ClassifiedTier: TierPerformance, Reason: "R6-lower",
			Classifier: "typesafe/jev-latest", TierConfidence: &conf, Dissatisfied: &dis},
	}
	tables := Calibrate(events, testConfig(""), CalibrateFilter{})
	tier := tableByScore(tables, "tier")
	diss := tableByScore(tables, "dissatisfied")

	if n := totalN(tier); n != 1 {
		t.Fatalf("the tier table holds %d turns, want only the followed one: %v", n, tier.Bands)
	}
	// and it is in the band its own confidence falls into, not in one of
	// the others. The ladder is calBands: 0.95-1, 0.85-0.95, 0.7-0.85,
	// 0.5-0.7, 0-0.5 — 0.9 is the second one. The band is named here and
	// not taken from the code, or this test would agree with whatever
	// the code says.
	const wantBand = 1
	for i, b := range tier.Bands {
		want := 0
		if i == wantBand {
			want = 1
		}
		if b.N != want {
			t.Errorf("tier band %d holds %d turns, want %d", i, b.N, want)
		}
	}
	// the two turns that were not followed are still dissatisfied turns
	if n := totalN(diss); n != 2 {
		t.Fatalf("the dissatisfied table holds %d turns, want 2: %v", n, diss.Bands)
	}
}
