package router

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weiping/magpie-bridge/internal/wire"
)

// sessionClock lets tests move time.
var sessionClock = time.Now

// sessionState is one session's router state.
type sessionState struct {
	tier         TurnState
	toolCalls    int
	toolFailures int
	lastAt       time.Time
	parent       string // set by MarkDerived / lineage

	turns   int            // committed turns (Commit increments)
	pending *ReviewVerdict // a review that has not been read yet (SP7 §3.2)
	pinned  bool           // the user chose the model by hand (SP7 §3.2)
}

// Sessions keeps per-session router state in memory: the last committed
// TurnState, the gateway's own count of tool calls and failures, and when
// the session last made a request. Sessions idle for 24 hours are evicted.
type Sessions struct {
	mu    sync.Mutex
	m     map[string]*sessionState
	words map[string]string // firstWords → session, for derived-session fallback (§5.8)
}

// NewSessions returns an empty store.
func NewSessions() *Sessions {
	return &Sessions{m: map[string]*sessionState{}, words: map[string]string{}}
}

// state returns the session's state, creating it if needed.
func (s *Sessions) state(key string) *sessionState {
	if s.m == nil {
		s.m = map[string]*sessionState{}
	}
	st, ok := s.m[key]
	if !ok {
		st = &sessionState{}
		s.m[key] = st
	}
	return st
}

// Get returns the session's committed turn state, or nil on the first turn.
func (s *Sessions) Get(key string) *TurnState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[key]
	if !ok || st.tier.Tier == "" {
		return nil
	}
	t := st.tier
	return &t
}

// Commit writes the session's turn state (§6.5: once per turn).
func (s *Sessions) Commit(key string, t TurnState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(key)
	st.tier = t
	st.turns++
	st.lastAt = sessionClock()
	s.evictLocked(st.lastAt)
}

// Turn reports how many turns the session has committed: the review of
// turn N is only valid while Turn is still N (SP7 §3.2).
func (s *Sessions) Turn(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.m[key]; ok {
		return st.turns
	}
	return 0
}

// PutReview files a review for turn, and drops it when the session has
// moved on (that turn is over and no later turn may be judged by it).
func (s *Sessions) PutReview(key string, turn int, v ReviewVerdict) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[key]
	if !ok || st.turns != turn {
		return
	}
	rv := v
	st.pending = &rv
}

// TakeReview returns the unread review and clears it (§3.2: read once).
func (s *Sessions) TakeReview(key string) *ReviewVerdict {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[key]
	if !ok || st.pending == nil {
		return nil
	}
	v := *st.pending
	st.pending = nil
	return &v
}

// MarkPinned records that the user picked the model by hand, so the
// session is left alone until it goes idle (SP7 §3.2).
func (s *Sessions) MarkPinned(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state(session).pinned = true
}

// Pinned reports whether the session was pinned by hand.
func (s *Sessions) Pinned(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[session]
	return ok && st.pinned
}

// Observe records what the gateway saw of a request: the session's tool
// call and failure counts (every message's tool_result parts; a Responses
// body's function_call_output never marks an error, S8) and the time.
func (s *Sessions) Observe(session string, tools wire.ToolStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := sessionClock()
	st := s.state(session)
	st.toolCalls, st.toolFailures, st.lastAt = tools.Calls, tools.Failures, now
	s.evictLocked(now)
}

// UpdatedAt reports when the session's state was last committed.
func (s *Sessions) UpdatedAt(key string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[key]
	if !ok || st.tier.Tier == "" {
		return time.Time{}, false
	}
	return st.lastAt, true
}

// Stats returns the session's tool stats as of its last request.
func (s *Sessions) Stats(session string) (calls, failures int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[session]
	if !ok {
		return 0, 0
	}
	return st.toolCalls, st.toolFailures
}

// SinceLast reports how long ago the session's last request was (zero when
// the session is unknown).
func (s *Sessions) SinceLast(session string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[session]
	if !ok {
		return 0
	}
	return sessionClock().Sub(st.lastAt)
}

// MarkDerived records that session descends from parent (the /lineage
// path; the harness's parent_session reaches Decide directly).
func (s *Sessions) MarkDerived(session, parent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state(session).parent = parent
}

// NoteWords records that firstWords (the hash of a conversation's first
// user message) belongs to session, so a session marked derived without a
// known parent can find it (§5.8's fallback).
func (s *Sessions) NoteWords(session, firstWords string) {
	if firstWords == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.words[firstWords] = session
}

// ParentOf resolves the parent of a derived session: the explicitly
// marked parent if there is one; for a session marked derived without one,
// the session its firstWords point at — and nobody else's (unrelated
// sessions may share first words, §5.8).
func (s *Sessions) ParentOf(session, firstWords string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, marked := s.m[session]
	if !marked {
		return "", false
	}
	if st.parent != "" {
		return st.parent, true
	}
	if firstWords == "" {
		return "", false
	}
	if p, ok := s.words[firstWords]; ok && p != session {
		return p, true
	}
	return "", false
}

// InheritFrom copies the parent's turn state into the child as its Prev
// (§5.8: EscalatedLeft kept, LowerStreak reset); no parent state means
// nothing happens.
func (s *Sessions) InheritFrom(child, parent string) *TurnState {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[parent]
	if !ok || p.tier.Tier == "" {
		return nil
	}
	t := p.tier
	t.LowerStreak = 0
	c := s.state(child)
	c.tier = t
	return &t
}

// evictLocked drops sessions idle past 24 hours. Caller holds the lock.
func (s *Sessions) evictLocked(now time.Time) {
	if len(s.m) < 1024 {
		return
	}
	for k, st := range s.m {
		if now.Sub(st.lastAt) > 24*time.Hour {
			delete(s.m, k)
		}
	}
}

// LiveSession is one session that is being routed right now (SP14 §C).
type LiveSession struct {
	Key           string
	Session       string
	Tier          Tier
	Turns         int
	Tools         int
	ToolFailures  int
	EscalatedLeft int
	LowerStreak   int
	Pinned        bool
	Parent        string
	LastAt        time.Time
}

// LiveCard is a LiveSession with the last decision that judged it.
type LiveCard struct {
	LiveSession
	Harness        string   `json:"harness"`
	Agent          string   `json:"agent"`
	Group          string   `json:"group"`
	Reason         string   `json:"reason"`
	TierConfidence *float64 `json:"tier_confidence"`
	LatencyMs      int      `json:"latency_ms"`
	ClassifyMs     int      `json:"classify_ms"`
}

// Live lists the sessions that asked for something within window, newest
// first, at most 20 of them (SP14 §C). A session is one state key: a
// gateway conversation has one per subagent, and they are listed apart.
func (s *Sessions) Live(window time.Duration) []LiveSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := sessionClock()
	live := []LiveSession{}
	for key, st := range s.m {
		// only a session that was judged is a card: one that was only
		// observed has no tier
		if st.tier.Tier == "" {
			continue
		}
		// an inherited tier has no lastAt: it is not a turn of its own
		if st.lastAt.IsZero() || now.Sub(st.lastAt) > window {
			continue
		}
		session, words := splitKey(key)
		card := LiveSession{
			Key:           key,
			Session:       session,
			Tier:          st.tier.Tier,
			Turns:         st.turns,
			EscalatedLeft: st.tier.EscalatedLeft,
			LowerStreak:   st.tier.LowerStreak,
			LastAt:        st.lastAt,
		}
		// the tool counts, the pin and the parent belong to the session,
		// not to the key (SP14 §C, 坑 1)
		if sst, ok := s.m[session]; ok {
			card.Tools, card.ToolFailures = sst.toolCalls, sst.toolFailures
			card.Pinned = sst.pinned
		}
		if p, ok := s.parentLocked(session, words); ok {
			card.Parent = p
		}
		live = append(live, card)
	}
	// newest first, then at most 20
	sort.Slice(live, func(i, j int) bool { return live[i].LastAt.After(live[j].LastAt) })
	if len(live) > 20 {
		live = live[:20]
	}
	return live
}

// parentLocked resolves a session's parent without taking the lock.
func (s *Sessions) parentLocked(session, firstWords string) (string, bool) {
	st, marked := s.m[session]
	if !marked {
		return "", false
	}
	if st.parent != "" {
		return st.parent, true
	}
	if firstWords == "" {
		return "", false
	}
	if p, ok := s.words[firstWords]; ok && p != session {
		return p, true
	}
	return "", false
}

// SessionOfKey is the session a state key belongs to: on the harness path
// the key is the session; on the gateway path it is the session, a "|",
// and the first words of the conversation (SP14 §C, 坑 1). The session id
// comes from the client and may contain a "|", so only a 24-hex suffix
// after the LAST "|" is first words.
func SessionOfKey(key string) string {
	session, _ := splitKey(key)
	return session
}

// splitKey splits a state key into its session and its first words.
func splitKey(key string) (session, firstWords string) {
	i := strings.LastIndex(key, "|")
	if i < 0 {
		return key, ""
	}
	suffix := key[i+1:]
	if len(suffix) != 24 || !isHex(suffix) {
		return key, ""
	}
	return key[:i], suffix
}

// isHex reports whether s is lowercase hexadecimal.
func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return len(s) > 0
}
