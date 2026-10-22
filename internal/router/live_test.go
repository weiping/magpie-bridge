package router

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/weiping/magpie-bridge/internal/wire"
)

// TestSessionOfKey: a state key is the session on the harness path, and
// session|firstWords on the gateway path. The session id comes from the
// client and may contain a "|", so only a 24-hex suffix after the LAST
// "|" is first words (SP14 §C, 坑 1).
func TestSessionOfKey(t *testing.T) {
	words := "0123456789abcdef01234567"
	for _, tc := range []struct{ key, want string }{
		{"sess", "sess"},
		{"sess|" + words, "sess"},
		{"sess|ion|pipe", "sess|ion|pipe"},
		{"sess|short", "sess|short"},
		{"sess|0123456789ABCDEF01234567", "sess|0123456789ABCDEF01234567"}, // upper case is not ours
		{"sess|" + words + "|" + words, "sess|" + words},
	} {
		if got := SessionOfKey(tc.key); got != tc.want {
			t.Errorf("SessionOfKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestLiveKeepsBothCardsOfOneSession: a gateway conversation has one
// state key per subagent, and they are listed apart.
func TestLiveKeepsBothCardsOfOneSession(t *testing.T) {
	s := NewSessions()
	base := time.Now()
	sessionClock = func() time.Time { return base }
	t.Cleanup(func() { sessionClock = time.Now })

	s.Commit("sess|0123456789abcdef01234567", TurnState{Tier: TierBalanced})
	s.Commit("sess|abcdef0123456789abcdef01", TurnState{Tier: TierFast})

	live := s.Live(time.Minute)
	if len(live) != 2 {
		t.Fatalf("live %+v", live)
	}
	for _, c := range live {
		if c.Session != "sess" {
			t.Fatalf("session %q", c.Session)
		}
	}
	if live[0].Tier == live[1].Tier {
		t.Fatalf("the two cards are the same: %+v", live)
	}
}

// TestLiveKeepsASessionIDWithAPipe: a session id that contains a "|" is
// not cut in half.
func TestLiveKeepsASessionIDWithAPipe(t *testing.T) {
	s := NewSessions()
	s.Commit("sess|ion|pipe", TurnState{Tier: TierFast})
	live := s.Live(time.Minute)
	if len(live) != 1 || live[0].Session != "sess|ion|pipe" {
		t.Fatalf("live %+v", live)
	}
}

// TestLiveSkipsEntriesWithoutALastAt: an inherited tier is not a turn of
// its own, and would render as "1677 years ago".
func TestLiveSkipsEntriesWithoutALastAt(t *testing.T) {
	s := NewSessions()
	s.Commit("parent", TurnState{Tier: TierPerformance})
	s.InheritFrom("child", "parent")
	live := s.Live(time.Minute)
	if len(live) != 1 || live[0].Key != "parent" {
		t.Fatalf("live %+v", live)
	}
}

// TestLiveSkipsSessionsOutsideTheWindow: five minutes is a display
// window, not a storage policy.
func TestLiveSkipsSessionsOutsideTheWindow(t *testing.T) {
	s := NewSessions()
	base := time.Now()
	sessionClock = func() time.Time { return base }
	t.Cleanup(func() { sessionClock = time.Now })
	s.Commit("old", TurnState{Tier: TierFast})
	sessionClock = func() time.Time { return base.Add(6 * time.Minute) }
	s.Commit("new", TurnState{Tier: TierFast})

	live := s.Live(5 * time.Minute)
	if len(live) != 1 || live[0].Key != "new" {
		t.Fatalf("live %+v", live)
	}
}

// TestLiveReturnsAtMostTwentyNewestFirst: the page shows 20 and says how
// many more there are.
func TestLiveReturnsAtMostTwentyNewestFirst(t *testing.T) {
	s := NewSessions()
	base := time.Now()
	sessionClock = func() time.Time { return base }
	t.Cleanup(func() { sessionClock = time.Now })
	for i := 0; i < 25; i++ {
		s.Commit(fmt.Sprintf("s-%02d", i), TurnState{Tier: TierFast})
		sessionClock = func() time.Time { return base.Add(time.Duration(i) * time.Second) }
	}
	live := s.Live(time.Hour)
	if len(live) != 20 {
		t.Fatalf("live %d", len(live))
	}
	if live[0].Key != "s-24" {
		t.Fatalf("newest first: %q", live[0].Key)
	}
}

// TestLiveFiltersBeforeTruncating: the window is applied before the
// twenty-card cut, so old sessions do not push new ones out.
func TestLiveFiltersBeforeTruncating(t *testing.T) {
	s := NewSessions()
	base := time.Now()
	sessionClock = func() time.Time { return base }
	t.Cleanup(func() { sessionClock = time.Now })
	for i := 0; i < 21; i++ {
		s.Commit(fmt.Sprintf("old-%02d", i), TurnState{Tier: TierFast})
	}
	sessionClock = func() time.Time { return base.Add(10 * time.Minute) }
	for i := 0; i < 4; i++ {
		s.Commit(fmt.Sprintf("new-%d", i), TurnState{Tier: TierFast})
	}
	live := s.Live(5 * time.Minute)
	if len(live) != 4 {
		t.Fatalf("live %d: %+v", len(live), live)
	}
	for _, c := range live {
		if c.Key[:3] != "new" {
			t.Fatalf("old card %q survived the window", c.Key)
		}
	}
}

// TestLiveReturnsAnEmptySlice: no sessions is an empty list, not null.
func TestLiveReturnsAnEmptySlice(t *testing.T) {
	s := NewSessions()
	live := s.Live(time.Minute)
	if live == nil {
		t.Fatal("nil")
	}
	if len(live) != 0 {
		t.Fatalf("live %+v", live)
	}
}

// TestTheLiveCardTakesTheToolsAndThePinFromTheSession: the tool counts
// and the pin belong to the session, not to the key.
func TestTheLiveCardTakesTheToolsAndThePinFromTheSession(t *testing.T) {
	s := NewSessions()
	base := time.Now()
	sessionClock = func() time.Time { return base }
	t.Cleanup(func() { sessionClock = time.Now })

	s.Observe("sess", wire.ToolStats{Calls: 24, Failures: 2})
	s.MarkPinned("sess")
	s.Commit("sess|0123456789abcdef01234567", TurnState{Tier: TierBalanced})

	live := s.Live(time.Minute)
	if len(live) != 1 {
		t.Fatalf("live %+v", live)
	}
	if live[0].Tools != 24 || live[0].ToolFailures != 2 {
		t.Fatalf("tools %+v", live[0])
	}
	if !live[0].Pinned {
		t.Fatalf("pinned %+v", live[0])
	}
}

// slowClassifier takes its time, so the classifier's own cost shows up.
type slowClassifier struct{ d time.Duration }

func (s slowClassifier) Classify(ctx context.Context, q Question) (*Verdict, error) {
	time.Sleep(s.d)
	return &Verdict{Tier: TierBalanced, TierConfidence: 0.8}, nil
}

func (s slowClassifier) Review(ctx context.Context, q ReviewQuestion) (*ReviewVerdict, error) {
	return nil, nil
}

// TestClassifyMsIsRecorded: the classifier's own time, and it is part of
// the Decide's time (SP14 §E).
func TestClassifyMsIsRecorded(t *testing.T) {
	cfg, err := Load(writeGlobal(t, validJSON), "")
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	d := &Deps{Config: cfg, Sessions: NewSessions(), Hints: NewHints(),
		Classify: slowClassifier{d: 30 * time.Millisecond},
		Log:      func(ev Event) error { events = append(events, ev); return nil }}

	d.Decide(context.Background(), DecideInput{Session: "s1", Key: "s1", Harness: "codex", Agent: "main", Prompt: "hello"})

	if len(events) != 1 {
		t.Fatalf("events %+v", events)
	}
	ev := events[0]
	if ev.ClassifyMs < 25 {
		t.Fatalf("classify ms %d", ev.ClassifyMs)
	}
	if ev.LatencyMs < ev.ClassifyMs {
		t.Fatalf("latency %d < classify %d", ev.LatencyMs, ev.ClassifyMs)
	}
}

// TestClassifyMsIsZeroOnOldEvents: events written before the field
// existed have none, and read as 0.
func TestClassifyMsIsZeroOnOldEvents(t *testing.T) {
	var ev Event
	line := `{"kind":"decide","t":"2026-10-01T00:00:00Z","session":"s1","tier":"fast","latency_ms":41}`
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ClassifyMs != 0 {
		t.Fatalf("classify ms %d", ev.ClassifyMs)
	}
	if ev.LatencyMs != 41 {
		t.Fatalf("latency %d", ev.LatencyMs)
	}
}
