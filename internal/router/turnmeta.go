package router

import (
	"encoding/json"
	"net/http"
)

// maxTurnMetaBody is the largest body CodexTurnID will parse.
const maxTurnMetaBody = 1 << 20 // 1 MiB

// turnMetaHeader is the header Codex projects client_metadata into.
const turnMetaHeader = "x-codex-turn-metadata"

// CodexTurnID reads the turn id a Codex request carries: from the
// x-codex-turn-metadata header first (it is projected there from the
// body's client_metadata), else from the Responses body's
// client_metadata["x-codex-turn-metadata"] object's turn_id field.
// Anything unreadable returns "".
func CodexTurnID(h http.Header, body []byte) string {
	return codexMetaField(h, body, "turn_id")
}

// CodexThreadSource is the turn metadata's thread_source: "user" for a
// turn the user typed, "thread_title" for the TUI's request that names the
// thread (Codex 0.162.1). "" when the request carries none.
func CodexThreadSource(h http.Header, body []byte) string {
	return codexMetaField(h, body, "thread_source")
}

// codexMetaField reads one string field of the turn metadata, from the
// header first and then the body.
func codexMetaField(h http.Header, body []byte, field string) string {
	if v := fieldFromValue(h.Get(turnMetaHeader), field); v != "" {
		return v
	}
	if len(body) == 0 || len(body) > maxTurnMetaBody {
		return ""
	}
	var req struct {
		ClientMetadata json.RawMessage `json:"client_metadata"`
	}
	if json.Unmarshal(body, &req) != nil {
		return ""
	}
	var meta map[string]json.RawMessage
	if !rawInto(req.ClientMetadata, &meta) {
		return ""
	}
	raw, ok := meta[turnMetaHeader]
	if !ok {
		return ""
	}
	var value string
	if !rawInto(raw, &value) {
		// The value may be the metadata object itself, not a string.
		var obj map[string]json.RawMessage
		if !rawInto(raw, &obj) {
			return ""
		}
		return fieldFromObject(obj, field)
	}
	return fieldFromValue(value, field)
}

// rawInto unmarshals a RawMessage into v, tolerating one level of
// JSON-encoding-as-string (the spike showed client_metadata arriving as a
// JSON string).
func rawInto(raw json.RawMessage, v any) bool {
	if len(raw) == 0 {
		return false
	}
	if err := json.Unmarshal(raw, v); err == nil {
		return true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	return json.Unmarshal([]byte(s), v) == nil
}

// fieldFromValue parses a header/metadata string and returns one field.
func fieldFromValue(s, field string) string {
	if s == "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &obj) != nil {
		return ""
	}
	return fieldFromObject(obj, field)
}

// fieldFromObject is a string field of the metadata object, "" otherwise.
func fieldFromObject(obj map[string]json.RawMessage, field string) string {
	raw, ok := obj[field]
	if !ok {
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v
}
