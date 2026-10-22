// Package restart serves mbridge's restart endpoint (SP14 §3.3). It has
// no key and mbridge has no authentication, so the only thing keeping a
// web page from restarting the machine's mbridge over and over is the
// check below: loopback for the browser, same-origin for the request.
package restart

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Register mounts POST /v1/bridge/restart. listen is mbridge's own
// address; do performs the restart.
func Register(mux *http.ServeMux, listen string, do func(context.Context) error) {
	mux.HandleFunc("POST /v1/bridge/restart", func(w http.ResponseWriter, r *http.Request) {
		// answer 403 before deciding anything about restarting
		if !SameOrigin(r, listen) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]any{"error": "cross-origin request refused"})
			return
		}
		if err := do(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]any{"restarting": true})
	})
}

// SameOrigin is whether this request came from a page on this address.
// A page on another site can POST to loopback without asking (a simple
// request needs no preflight), so the header that says "same-origin" is
// the only thing that stops it — and a request with no such header is
// refused rather than allowed.
func SameOrigin(r *http.Request, listen string) bool {
	// a host that is not loopback can be a name that resolves to it
	// (DNS rebinding): mbridge only serves on loopback
	if !isLoopback(r.Host) {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host || u.Host == listen
}

// isLoopback is whether host (with or without a port) is this machine.
func isLoopback(host string) bool {
	h := host
	if strings.HasPrefix(h, "[") { // [::1]:3426
		if i := strings.Index(h, "]"); i > 0 {
			h = h[1:i]
		}
	} else if i := strings.LastIndex(h, ":"); i > -1 && !strings.Contains(h[:i], ":") {
		h = h[:i]
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}