package status

import (
	"regexp"
	"strings"
	"testing"
)

// TestThePageFetchesOnlyMbridgesOwnEndpoints: the status page reads
// mbridge itself and asks it to restart mbridge; it fetches nothing else
// (SP14 §3.1).
func TestThePageFetchesOnlyMbridgesOwnEndpoints(t *testing.T) {
	html := string(Page())
	fetch := regexp.MustCompile(`fetch\(\s*["']([^"']+)`)
	got := fetch.FindAllStringSubmatch(html, -1)
	if len(got) < 2 {
		t.Fatal("the page fetches nothing")
	}
	allowed := map[string]bool{"/v1/bridge/router": true, "/v1/bridge/restart": true}
	for _, m := range got {
		path := m[1]
		if i := strings.IndexByte(path, '?'); i > -1 {
			path = path[:i]
		}
		if !allowed[path] {
			t.Fatalf("the page fetches %q", m[1])
		}
	}
}

// TestThePageRefreshesEveryFiveSeconds: the page is meant to be watched.
func TestThePageRefreshesEveryFiveSeconds(t *testing.T) {
	html := string(Page())
	if !strings.Contains(html, "REFRESH_MS = 5000") {
		t.Fatal("the refresh is not five seconds")
	}
	if !strings.Contains(html, "setInterval(load, REFRESH_MS)") {
		t.Fatal("nothing schedules the refresh")
	}
}

// TestThePageHasNoExternalAssets: one file, so it works with no network.
func TestThePageHasNoExternalAssets(t *testing.T) {
	html := string(Page())
	for _, m := range regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(html, -1) {
		if m[1] == "" || strings.HasPrefix(m[1], "#") {
			continue
		}
		t.Fatalf("the page loads %q", m[1])
	}
}

// TestThePageSaysWhatItDoesNotKnow: the honest boundaries are on the page
// (SP14 §3.1).
func TestThePageSaysWhatItDoesNotKnow(t *testing.T) {
	html := string(Page())
	for _, want := range []string{
		"unknown", "notAsked", "notMeasured", "sampleShort", "feedbackOnly", "unpriced",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	// the salt is not drawn, and the page says the criteria may be
	// overridden by a project file it cannot show
	if strings.Contains(html, "salt") {
		t.Error("the page mentions the salt")
	}
	if !strings.Contains(html, "criteria") {
		t.Error("the page says nothing about project criteria")
	}
}

// TestThePageCountsBothLanguages: the reason table is in both (SP14 §C).
func TestThePageCountsBothLanguages(t *testing.T) {
	html := string(Page())
	if !strings.Contains(html, "R6-adopt") || !strings.Contains(html, "R2-plan-mode") {
		t.Fatal("the reason table is missing reasons")
	}
	if !strings.Contains(html, "en:") || !strings.Contains(html, "zh:") {
		t.Fatal("the page is not bilingual")
	}
	for _, reason := range []string{"R1-fixed", "R3-tools", "R3-review", "R4-escalation-hold",
		"R6-lower", "R6-keep", "R7-carry", "R8-default", "R-held", "codex-title", "hint"} {
		if !strings.Contains(html, `"`+reason+`"`) {
			t.Errorf("the reason table lacks %q", reason)
		}
	}
}
