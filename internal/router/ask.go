package router

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/weiping/magpie-bridge/internal/magpie"
)

// AskVia is the classifier's ask callback over official magpie (SP8): a
// TypeSafe decider (typesafe/…) is asked at magpie's System One endpoint,
// any other model through its Chat Completions, both with magpie's keys.
// A routing group (SP15: group/<id>, or a bare group name) is asked at
// System One too: magpie resolves the group's decision models there, so the
// body names the group ref (bare names get the group/ prefix).
func AskVia(c *magpie.Client) func(ctx context.Context, model, body string) (string, error) {
	return func(ctx context.Context, model, body string) (string, error) {
		systemOne := strings.HasPrefix(model, "typesafe/")
		ref := model
		if isClassifierGroup(model) {
			systemOne = true
			if !strings.HasPrefix(model, "group/") {
				ref = "group/" + model
			}
		}
		if !systemOne {
			return c.Chat(ctx, model, []byte(body))
		}
		// System One finds the decider by the body's model prefix; the
		// classifier writes the decider's own name there (jev-latest)
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			return "", err
		}
		m["model"], _ = json.Marshal(ref)
		b, err := json.Marshal(m)
		if err != nil {
			return "", err
		}
		out, err := c.SystemOne(ctx, b)
		return string(out), err
	}
}
