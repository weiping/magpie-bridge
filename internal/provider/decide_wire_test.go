package provider

import (
	"encoding/json"
	"testing"
)

// Workers AI's answer comes in Cloudflare's envelope, and the Jev it
// serves answers in one more: the Jev's own "state" and "result" (ARNO on
// Discord: cloudflare-jev's Jev answered, and nothing read the answers
// out of it). The envelope is peeled while what is left holds another,
// so the answers are at the top, as a Clef's and TypeSafe's own are.
func TestCloudflareAnswerPeelsTheEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"a Clef's answer, in Cloudflare's envelope",
			`{"result":{"model":"clef","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":143,"output_tokens":0}},"success":true,"errors":[],"messages":[]}`},
		{"a Jev's answer, in Cloudflare's and the Jev's own envelope",
			`{"result":{"state":"Completed","result":{"model":"jev-1.13.0","answers":{"ok":{"type":"noul","noul":0.7}},"usage":{"input_tokens":278,"output_tokens":20}}},"success":true,"errors":[],"messages":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := DecideAnswer(ViaCloudflare, []byte(tc.in))
			var out struct {
				Model   string          `json:"model"`
				Answers json.RawMessage `json:"answers"`
			}
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("the answer is not json: %v: %s", err, b)
			}
			if out.Model == "" || len(out.Answers) == 0 {
				t.Fatalf("the answers are not at the top: %s", b)
			}
		})
	}
}

// An answer with no envelope in it is left as it is, and one that is not
// an answer at all is left as it is too: a failed run's errors are read
// as they are.
func TestCloudflareAnswerLeavesWhatItCannotPeel(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"an error", `{"success":false,"errors":[{"code":7003,"message":"Bad input"}],"messages":[]}`},
		{"an answer with no envelope", `{"model":"clef","answers":{"ok":{"type":"noul","noul":0.9}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(DecideAnswer(ViaCloudflare, []byte(tc.in))); got != tc.in {
				t.Fatalf("the answer was changed: %s", got)
			}
		})
	}
}
