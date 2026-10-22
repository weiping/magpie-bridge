package restart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// restartServer is the endpoint with a counter of what it did.
func restartServer(t *testing.T, listen string, calls *int, fail error) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, listen, func(context.Context) error {
		*calls++
		return fail
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestRestartRefusesCrossSite: any page can POST to loopback without
// asking, so a request that does not say it is same-origin is refused —
// and the restart does not happen (SP14 §3.3).
func TestRestartRefusesCrossSite(t *testing.T) {
	calls := 0
	srv := restartServer(t, "127.0.0.1:3426", &calls, nil)

	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "none", "Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "same-site", "Origin": "https://news.example"},
		{"Origin": "https://evil.example"},
		{}, // no headers at all is not a pass: a local process that wants a
		// restart runs `mbridge service restart`, not this endpoint
	} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/bridge/restart", nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%v answered %d", headers, res.StatusCode)
		}
	}
	if calls != 0 {
		t.Fatalf("the restart ran %d times", calls)
	}
}

// TestRestartRefusesANonLoopbackHost: a name that resolves to loopback
// (DNS rebinding) is not same-origin.
func TestRestartRefusesANonLoopbackHost(t *testing.T) {
	calls := 0
	srv := restartServer(t, "127.0.0.1:3426", &calls, nil)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/bridge/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "mbridge.example"
	req.Header.Set("Origin", "http://mbridge.example")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", res.StatusCode)
	}
	if calls != 0 {
		t.Fatalf("the restart ran %d times", calls)
	}
}

// TestRestartAllowsTheStatusPage: the page is served from mbridge's own
// address, so its request is same-origin.
func TestRestartAllowsTheStatusPage(t *testing.T) {
	calls := 0
	srv := restartServer(t, "127.0.0.1:3426", &calls, nil)

	res, err := sameOriginPost(t, srv.URL+"/v1/bridge/restart")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", res.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("the restart ran %d times", calls)
	}
}

// sameOriginPost is the request a browser makes from a page served by
// mbridge itself.
func sameOriginPost(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", url[:strings.Index(url, "/v1")])
	return http.DefaultClient.Do(req)
}

// TestSameOriginDecidesIt: the rule itself, one case at a time.
func TestSameOriginDecidesIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		host    string
		listen  string
		origin  string
		fetch   string
		sameOrg bool
	}{
		{name: "the page on loopback", host: "127.0.0.1:3426", listen: "127.0.0.1:3426",
			origin: "http://127.0.0.1:3426", sameOrg: true},
		{name: "localhost", host: "localhost:3426", listen: "127.0.0.1:3426",
			origin: "http://localhost:3426", sameOrg: true},
		{name: "ipv6 loopback", host: "[::1]:3426", listen: "[::1]:3426",
			origin: "http://[::1]:3426", sameOrg: true},
		{name: "same-origin without an Origin header", host: "127.0.0.1:3426", listen: "127.0.0.1:3426",
			fetch: "same-origin", sameOrg: true},
		{name: "another site", host: "127.0.0.1:3426", listen: "127.0.0.1:3426",
			origin: "https://evil.example"},
		{name: "a rebound host", host: "mbridge.example", listen: "127.0.0.1:3426",
			origin: "http://mbridge.example"},
		{name: "no headers", host: "127.0.0.1:3426", listen: "127.0.0.1:3426"},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://"+tc.listen+"/v1/bridge/restart", nil)
		r.Host = tc.host
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.fetch != "" {
			r.Header.Set("Sec-Fetch-Site", tc.fetch)
		}
		if got := SameOrigin(r, tc.listen); got != tc.sameOrg {
			t.Errorf("%s: SameOrigin = %v", tc.name, got)
		}
	}
}

// TestRestartReportsItsFailure: the endpoint says what went wrong.
func TestRestartReportsItsFailure(t *testing.T) {
	calls := 0
	srv := restartServer(t, "127.0.0.1:3426", &calls, errRestart)
	res, err := sameOriginPost(t, srv.URL+"/v1/bridge/restart")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", res.StatusCode)
	}
}

var errRestart = &restartError{"no unit installed"}

type restartError struct{ msg string }

func (e *restartError) Error() string { return e.msg }