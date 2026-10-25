// Package magpie is mbridge's only way to official magpie (SP8 spec §5.4):
// its public HTTP endpoints, its CLI and its usage CSV. Nothing else in
// mbridge talks to magpie, so when magpie changes, this package and the
// contract tests are what change.
package magpie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/weiping/magpie-bridge/internal/runcmd"
)

// UserAgent is what mbridge's own calls through magpie carry.
const UserAgent = "mbridge/1"

// GroupPrefix is how a model names a magpie routing group.
const GroupPrefix = "group/"

// DefaultURL is magpie's gateway on this computer.
const DefaultURL = "http://127.0.0.1:3425"

// Client reaches one magpie: its gateway at BaseURL, its CLI at Bin.
type Client struct {
	BaseURL string
	Key     string // a gateway key, when magpie asks for one
	Bin     string
	HTTP    *http.Client
	// Run runs the CLI; tests replace it
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// New is a client of the magpie at baseURL, its CLI found on PATH.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultURL
	}
	bin := "magpie"
	if p, err := exec.LookPath("magpie"); err == nil {
		bin = p
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Bin: bin, HTTP: &http.Client{}, Run: run}
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runcmd.CommandContext(ctx, name, args...).CombinedOutput()
}

// Error is magpie answering a call with an error.
type Error struct {
	Op     string
	Status int
	Msg    string
	Err    error // the cause, when there is one (a deadline, say)
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("magpie %s: %s", e.Op, e.Msg)
	}
	return fmt.Sprintf("magpie %s: %d %s", e.Op, e.Status, e.Msg)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, headers ...string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// magpie was reached but didn't answer in time, or the caller
			// stopped waiting: not "can't reach"
			return nil, &Error{Op: method + " " + path, Msg: "no answer from magpie in time: " + ctxErr.Error(), Err: ctxErr}
		}
		return nil, &Error{Op: method + " " + path, Msg: "can't reach magpie at " + strings.TrimPrefix(c.BaseURL, "http://") + ": " + err.Error(), Err: err}
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, &Error{Op: method + " " + path, Status: res.StatusCode, Msg: msg}
	}
	return out, nil
}

// SystemOne posts a System One request (Jev's) as it is.
func (c *Client) SystemOne(ctx context.Context, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPost, "/v1/systemone", body)
}

// Chat posts a Chat Completions body and returns the first choice's text.
func (c *Client) Chat(ctx context.Context, model string, body []byte) (string, error) {
	out, err := c.do(ctx, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		return "", err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &r); err != nil || len(r.Choices) == 0 {
		return "", &Error{Op: "chat " + model, Msg: "not an answer: " + strings.TrimSpace(string(out))}
	}
	return r.Choices[0].Message.Content, nil
}

// Model is one model magpie lists, with the context window it says the
// model (or group) has; 0 when it doesn't say.
type Model struct {
	ID      string
	Context int
}

// ModelList lists what magpie serves (groups included): GET /v1/models.
// Passes X-Magpie-Deciders: 1 so decision models (Jev, Clef) are included
// alongside chat models.
func (c *Client) ModelList(ctx context.Context) ([]Model, error) {
	out, err := c.do(ctx, http.MethodGet, "/v1/models", nil, "X-Magpie-Deciders", "1")
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []struct {
			ID      string `json:"id"`
			Context int    `json:"context_window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, &Error{Op: "GET /v1/models", Msg: "not a model list"}
	}
	ms := make([]Model, 0, len(r.Data))
	for _, m := range r.Data {
		ms = append(ms, Model{ID: m.ID, Context: m.Context})
	}
	return ms, nil
}

// Models lists the model ids magpie serves (groups included).
func (c *Client) Models(ctx context.Context) ([]string, error) {
	ms, err := c.ModelList(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Health is nil when magpie's gateway answers within two seconds.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := c.do(ctx, http.MethodGet, "/v1", nil)
	return err
}

// Info is magpie's version, read from its gateway's /v1 answer, and whether
// it is there at all. It is the HTTP door rather than the CLI (`magpie
// version`) on purpose: magpie's CLI starts its plugin host, and a host
// whose work never settles outlives the CLI that started it — one per poll
// of a status page, which is thousands of orphan processes a day (measured
// 2026-10-25: 595 hosts, 7 GB, swap full, and the routing classifier
// timing out behind them).
func (c *Client) Info(ctx context.Context) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := c.do(ctx, http.MethodGet, "/v1", nil)
	if err != nil {
		return "", false
	}
	var r struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(out, &r) != nil || r.Name != "magpie" {
		return "", false
	}
	return r.Version, true
}
