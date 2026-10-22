package router

import (
	"context"
	"testing"
	"time"

	"github.com/weiping/magpie-bridge/internal/wire"
)

// TestTheCardTakesTheHarnessFromTheLastDecide: the harness, the agent
// and the reason are in the event, not in the session state (SP14 §C,
// 坑 3).
func TestTheCardTakesTheHarnessFromTheLastDecide(t *testing.T) {
	d, _, _ := testDeps(t, &Verdict{Tier: TierBalanced, TierConfidence: 0.82})
	d.Decide(context.Background(), DecideInput{
		Session: "sess", Key: "sess", Harness: "codex", Agent: "main", Prompt: "hello there",
	})

	cards := d.LiveCards(LiveWindow)
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	c := cards[0]
	if c.Harness != "codex" || c.Agent != "main" {
		t.Fatalf("card %+v", c)
	}
	if c.Reason == "" {
		t.Fatalf("no reason: %+v", c)
	}
	if c.TierConfidence == nil || *c.TierConfidence != 0.82 {
		t.Fatalf("confidence %+v", c.TierConfidence)
	}
	if c.Group == "" {
		t.Fatalf("no group: %+v", c)
	}
	if c.Tier != TierBalanced {
		t.Fatalf("tier %q", c.Tier)
	}
}

// TestTheCardSaysUnknownWhenTheEventIsGone: a session whose event has
// rolled out of the log has no harness — the page says "unknown", not an
// empty string.
func TestTheCardSaysUnknownWhenTheEventIsGone(t *testing.T) {
	d, _, _ := testDeps(t, nil)
	d.Sessions.Commit("sess", TurnState{Tier: TierFast})

	cards := d.LiveCards(LiveWindow)
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	if cards[0].Harness != "" || cards[0].Agent != "" {
		t.Fatalf("card %+v", cards[0])
	}
	if cards[0].Tier != TierFast {
		t.Fatalf("tier %q", cards[0].Tier)
	}
}

// TestTheCardsOfOneSessionKeepTheirOwnDecision: a gateway conversation
// has one card per subagent, and each card carries its own decision
// (SP14 §C, 坑 1).
func TestTheCardsOfOneSessionKeepTheirOwnDecision(t *testing.T) {
	d, _, _ := testDeps(t, &Verdict{Tier: TierBalanced})
	d.Decide(context.Background(), DecideInput{
		Session: "sess", Key: "sess|0123456789abcdef01234567", Harness: "gateway",
		Agent: "gateway", Prompt: "hello there", FirstWords: "0123456789abcdef01234567",
	})
	d.Decide(context.Background(), DecideInput{
		Session: "sess", Key: "sess|abcdef0123456789abcdef01", Harness: "gateway",
		Agent: "gateway", Prompt: "another turn", FirstWords: "abcdef0123456789abcdef01",
	})

	cards := d.LiveCards(LiveWindow)
	if len(cards) != 2 {
		t.Fatalf("cards %+v", cards)
	}
	for _, c := range cards {
		if c.Session != "sess" {
			t.Fatalf("session %q", c.Session)
		}
	}
}

// TestTheCardHasNoToolsOnTheHarnessPath: the /turn path's tool counts
// are not stored, so the page shows "—" (SP14 §C, 坑 4).
func TestTheCardHasNoToolsOnTheHarnessPath(t *testing.T) {
	d, _, _ := testDeps(t, &Verdict{Tier: TierBalanced})
	d.Decide(context.Background(), DecideInput{
		Session: "sess", Key: "sess", Harness: "claude-code", Agent: "main", Prompt: "hello there",
	})
	cards := d.LiveCards(LiveWindow)
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	if cards[0].Tools != 0 || cards[0].ToolFailures != 0 {
		t.Fatalf("tools %+v", cards[0])
	}
}

// TestTheCardHasTheGatewayTools: the gateway path observes the tools.
func TestTheCardHasTheGatewayTools(t *testing.T) {
	d, _, _ := testDeps(t, &Verdict{Tier: TierBalanced})
	d.Sessions.Observe("sess", toolStats(24, 2))
	d.Decide(context.Background(), DecideInput{
		Session: "sess", Key: "sess|0123456789abcdef01234567", Harness: "gateway",
		Agent: "gateway", Prompt: "hello there", FirstWords: "0123456789abcdef01234567",
	})
	cards := d.LiveCards(LiveWindow)
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	if cards[0].Tools != 24 || cards[0].ToolFailures != 2 {
		t.Fatalf("tools %+v", cards[0])
	}
}

// TestTheLiveWindowIsFiveMinutes: the display window, not a storage
// policy.
func TestTheLiveWindowIsFiveMinutes(t *testing.T) {
	if LiveWindow != 5*time.Minute {
		t.Fatalf("window %v", LiveWindow)
	}
}

// toolStats is the gateway's count of tool calls and failures.
func toolStats(calls, failures int) wire.ToolStats {
	return wire.ToolStats{Calls: calls, Failures: failures}
}
