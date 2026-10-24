package provider

import (
	"encoding/json"
	"math"
	"strings"
)

// DecideAsk is a System One request (body) as via takes it: Vercel's
// TypeSafe API takes it as it is; its evaluation models name the model in
// a header and ask a noul as a boolean; Workers AI takes the state and
// questions as the input of a run of the model, and a Clef there takes
// the request as it is, its model named as Clef names itself.
func DecideAsk(via, model string, body []byte) []byte {
	var q map[string]any
	if via == ViaSystemOne || via == ViaVercel || json.Unmarshal(body, &q) != nil {
		return body
	}
	delete(q, "model")
	switch via {
	case ViaVercelEval:
		qs, _ := q["questions"].(map[string]any)
		for _, v := range qs {
			if x, ok := v.(map[string]any); ok && x["type"] == "noul" {
				x["type"] = "boolean"
			}
		}
		b, _ := json.Marshal(q)
		return b
	case ViaCloudflare:
		if CloudflareClef(model) {
			q["model"] = strings.TrimPrefix(model, "@cf/cloudflare/")
			b, _ := json.Marshal(q)
			return b
		}
		b, _ := json.Marshal(map[string]any{"model": model, "input": q})
		return b
	}
	return body
}

// cloudflareAnswer is Workers AI's answer as System One gives it: out of
// the envelopes Cloudflare wraps a run in. A Clef answers as it is asked
// (one envelope), but the Jev Workers AI serves answers in the envelope
// of a System One answer too, so a Jev's answer comes in two (ARNO on
// Discord: cloudflare-jev's Jev answered, and nothing read the answers
// out of it). The envelope is peeled while what is left holds another
// and no answers of its own, so the answer is the same one a Clef's is:
// the answers, at the top.
func cloudflareAnswer(b []byte) []byte {
	for range 4 { // Cloudflare's own, the Jev's, and room for two more
		var env struct {
			Answers json.RawMessage `json:"answers"`
			Result  json.RawMessage `json:"result"`
		}
		if json.Unmarshal(b, &env) != nil {
			return b
		}
		if len(env.Answers) > 0 { // the answer is here already
			return b
		}
		if len(env.Result) == 0 || env.Result[0] != '{' {
			return b
		}
		b = env.Result
	}
	return b
}

// DecideAnswer is a gateway's answer as System One gives it: out of
// Cloudflare's envelope, or, from Vercel's evaluation models (its TypeSafe
// API answers as System One does), a boolean's probability as a
// noul, a confidence from the probabilities where there is none, and its
// usage named as System One names it.
func DecideAnswer(via string, b []byte) []byte {
	switch via {
	case ViaCloudflare:
		return cloudflareAnswer(b)
	case ViaVercelEval:
		var v struct {
			Model   string                    `json:"model"`
			Answers map[string]map[string]any `json:"answers"`
			Usage   struct {
				Input  int `json:"inputTokens"`
				Output int `json:"outputTokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(b, &v) != nil || v.Answers == nil {
			return b
		}
		for _, a := range v.Answers {
			if a["type"] == "boolean" {
				a["type"], a["noul"] = "noul", a["probability"]
				continue
			}
			if _, ok := a["confidence"]; !ok {
				if ps, ok := a["probabilities"].(map[string]any); ok {
					a["confidence"] = Confidence(ps)
				}
			}
		}
		out, _ := json.Marshal(map[string]any{"model": v.Model, "answers": v.Answers,
			"usage": map[string]int{"input_tokens": v.Usage.Input, "output_tokens": v.Usage.Output}})
		return out
	}
	return b
}

// Confidence is how sure a set of probabilities is of its likeliest, as
// System One gives it: 0 with every option as likely, 1 with one certain
// (its 0.87 of three options is 0.8).
func Confidence(ps map[string]any) float64 {
	top, n := 0.0, 0
	for _, v := range ps {
		if f, ok := v.(float64); ok {
			top, n = math.Max(top, f), n+1
		}
	}
	if n < 2 {
		return 1
	}
	return max(0, (top-1/float64(n))/(1-1/float64(n)))
}
