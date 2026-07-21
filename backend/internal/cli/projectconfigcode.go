package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// This file holds the pure, daemon-independent core of the `ao project config`
// export/apply/diff commands. It operates on config as raw JSON (a
// map[string]any decoded with UseNumber) rather than the typed CLI config
// mirror, so no field the daemon serializes is ever dropped and "named in the
// spec" maps exactly to "key present in the parsed spec object" — the surgical
// contract. See openspec/changes/add-project-config-as-code/design.md.

// configDrift is one field where a spec value disagrees with live config.
// LivePresent distinguishes a live value that is explicitly JSON null (present,
// Live == nil) from one that is absent from live config entirely (not present).
type configDrift struct {
	Field       string
	Spec        any
	Live        any
	LivePresent bool
}

// parseConfigObject decodes a JSON object into map[string]any using UseNumber so
// integer fields (e.g. maxLiveWorkers) survive the decode/re-encode round-trip
// exactly. It is lenient: an empty or `null` payload decodes to an empty object,
// which is correct for reading a project's live config (an unset config is
// serialized as null / absent). Spec files use the strict parseSpecObject.
func parseConfigObject(raw []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

// parseSpecObject strictly parses an operator-supplied config spec: it requires
// exactly one JSON object (not null, not an array/scalar) followed by EOF, so an
// empty file, a bare `null`, trailing garbage (`{…}{…}`, `{…}}`), or a null-valued
// field is a hard error rather than a silent no-op or a partial apply. UseNumber
// preserves integers.
func parseSpecObject(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("spec is empty: expected a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("spec is JSON null: expected a JSON object")
	}
	// Require EOF after the first object. dec.More() is unreliable at the top
	// level (it returns false before a stray `}`/`]`), so decode a second value
	// and insist it is io.EOF — this rejects `{…}}`, `{…}]`, and `{…}{…}`.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("spec has trailing content after the JSON object")
	}
	// The config model has no nullable fields (every field is omitempty and a
	// JSON null decodes to the zero value, which the daemon then omits). An
	// explicit null in a spec can therefore never converge (apply/diff would
	// forever see null-in-spec vs absent-in-live), so reject it at the door.
	for k, v := range obj {
		if v == nil {
			return nil, fmt.Errorf("spec field %q is null: omit the field instead of setting it to null", k)
		}
	}
	return obj, nil
}

// canonicalizeConfig returns config JSON in a canonical form: keys sorted at
// every level (Go sorts map keys on marshal), two-space indentation, and a
// trailing newline. Two canonicalizations of the same logical config are
// byte-identical, making exports diff-friendly and round-trip stable.
func canonicalizeConfig(raw []byte) ([]byte, error) {
	obj, err := parseConfigObject(raw)
	if err != nil {
		return nil, err
	}
	return canonicalizeConfigMap(obj)
}

// canonicalizeConfigMap renders an already-parsed config map in canonical form
// (sorted keys, two-space indent, trailing newline).
func canonicalizeConfigMap(obj map[string]any) ([]byte, error) {
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// overlayConfig returns a new config map equal to live with every top-level key
// present in spec overlaid (a named key's value fully replaces the live value),
// plus the sorted list of keys whose overlaid value differs from live. live is
// not mutated.
func overlayConfig(live, spec map[string]any) (map[string]any, []string) {
	merged := make(map[string]any, len(live))
	for k, v := range live {
		merged[k] = v
	}
	var changed []string
	for k, specVal := range spec {
		liveVal, present := live[k]
		if !present || !reflect.DeepEqual(liveVal, specVal) {
			changed = append(changed, k)
		}
		merged[k] = specVal
	}
	sort.Strings(changed)
	return merged, changed
}

// diffConfig reports, for each top-level key named in spec, where the spec value
// disagrees with live config. A key absent from live is drift (mirrors
// overlayConfig, which treats it as a change) and is recorded with
// LivePresent=false so callers can render "absent" distinctly from an explicit
// JSON null. Keys not named in spec are ignored. Results are sorted by field name.
func diffConfig(live, spec map[string]any) []configDrift {
	var drift []configDrift
	for k, specVal := range spec {
		liveVal, present := live[k]
		if !present || !reflect.DeepEqual(liveVal, specVal) {
			drift = append(drift, configDrift{Field: k, Spec: specVal, Live: liveVal, LivePresent: present})
		}
	}
	sort.Slice(drift, func(i, j int) bool { return drift[i].Field < drift[j].Field })
	return drift
}
