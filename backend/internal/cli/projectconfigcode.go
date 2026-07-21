package cli

import (
	"bytes"
	"encoding/json"
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
type configDrift struct {
	Field string
	Spec  any
	Live  any
}

// parseConfigObject decodes a JSON object into map[string]any using UseNumber so
// integer fields (e.g. maxLiveWorkers) survive the decode/re-encode round-trip
// exactly. A JSON `null` config decodes to an empty object.
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

// canonicalizeConfig returns config JSON in a canonical form: keys sorted at
// every level (Go sorts map keys on marshal), two-space indentation, and a
// trailing newline. Two canonicalizations of the same logical config are
// byte-identical, making exports diff-friendly and round-trip stable.
func canonicalizeConfig(raw []byte) ([]byte, error) {
	obj, err := parseConfigObject(raw)
	if err != nil {
		return nil, err
	}
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
// disagrees with live config. Keys not named in spec are ignored. Results are
// sorted by field name.
func diffConfig(live, spec map[string]any) []configDrift {
	var drift []configDrift
	for k, specVal := range spec {
		liveVal := live[k]
		if !reflect.DeepEqual(liveVal, specVal) {
			drift = append(drift, configDrift{Field: k, Spec: specVal, Live: liveVal})
		}
	}
	sort.Slice(drift, func(i, j int) bool { return drift[i].Field < drift[j].Field })
	return drift
}
