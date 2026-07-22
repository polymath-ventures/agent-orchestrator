package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	Kind        configDriftKind
	Spec        any
	Live        any
	SpecPresent bool
	LivePresent bool
}

type configDriftKind string

const (
	driftChanged    configDriftKind = "changed"
	driftMissing    configDriftKind = "missing"
	driftUnexpected configDriftKind = "unexpected"
)

var fieldPathPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

var secretKeyPattern = regexp.MustCompile(`(?i)(secret|token|passw(or)?d|passphrase|api[_-]?key|private[_-]?key|access[_-]?key|credential|cookie|session|signing|cert|database[_-]?url|conn(ection)?[_-]?str|dsn|auth)`)
var patKeyPattern = regexp.MustCompile(`(?i)(^|[_-])pat([_-]|$)`)

func looksSecretKey(key string) bool {
	return secretKeyPattern.MatchString(key) || patKeyPattern.MatchString(key)
}

func secretEnvKeys(config map[string]any, allowed map[string]struct{}) []string {
	env, ok := config["env"].(map[string]any)
	if !ok {
		return nil
	}
	var offenders []string
	for key := range env {
		if _, exempt := allowed[key]; !exempt && looksSecretKey(key) {
			offenders = append(offenders, key)
		}
	}
	sort.Strings(offenders)
	return offenders
}

func redactSensitiveValue(path string, value any) any {
	leaf := path
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		leaf = path[idx+1:]
	}
	if path == "env" || strings.HasPrefix(path, "env.") || looksSecretKey(leaf) {
		return "<redacted>"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			out[key] = redactSensitiveValue(childPath, child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactSensitiveValue(path, child)
		}
		return out
	default:
		return value
	}
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
	// The config model has no nullable values at any depth: a JSON null decodes to
	// the zero value (an omitted field, or an empty string inside a string map),
	// which can never converge — apply/diff would forever see null-in-spec vs
	// absent-or-empty-in-live. Reject a null anywhere in the spec at the door.
	if err := rejectSpecNulls("", obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// rejectSpecNulls returns an error naming the first null value found anywhere in
// a parsed spec (walking objects and arrays), so an operator gets a clear "omit
// the field" message instead of a silently non-converging apply/diff.
func rejectSpecNulls(path string, v any) error {
	switch val := v.(type) {
	case nil:
		if path == "" {
			return errors.New("spec value is null: omit the field instead of setting it to null")
		}
		return fmt.Errorf("spec field %q is null: omit the field instead of setting it to null", path)
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			if err := rejectSpecNulls(child, val[k]); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range val {
			if err := rejectSpecNulls(fmt.Sprintf("%s[%d]", path, i), item); err != nil {
				return err
			}
		}
	}
	return nil
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
		if !present && isAbsentEquivalent(specVal) {
			continue
		}
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
	return diffConfigWithUnexpected(live, spec, false)
}

func diffConfigUnexpected(live, spec map[string]any) []configDrift {
	return diffConfigWithUnexpected(live, spec, true)
}

func diffConfigWithUnexpected(live, spec map[string]any, includeUnexpected bool) []configDrift {
	var drift []configDrift
	for k, specVal := range spec {
		liveVal, present := live[k]
		if !present && isAbsentEquivalent(specVal) {
			continue
		}
		if !present || !reflect.DeepEqual(liveVal, specVal) {
			kind := driftChanged
			if !present {
				kind = driftMissing
			}
			drift = append(drift, configDrift{
				Field: k, Kind: kind, Spec: specVal, Live: liveVal,
				SpecPresent: true, LivePresent: present,
			})
		}
	}
	if includeUnexpected {
		for k, liveVal := range live {
			if _, present := spec[k]; present || isAbsentEquivalent(liveVal) {
				continue
			}
			drift = append(drift, configDrift{
				Field: k, Kind: driftUnexpected, Live: liveVal, LivePresent: true,
			})
		}
	}
	sort.Slice(drift, func(i, j int) bool { return drift[i].Field < drift[j].Field })
	return drift
}

// isAbsentEquivalent reports values that Go's ProjectConfig JSON omits under
// omitempty. A hand-written spec naming one of these has converged when live
// omits the field; if live is nonzero, applying the zero still clears it.
func isAbsentEquivalent(v any) bool {
	switch value := v.(type) {
	case bool:
		return !value
	case string:
		return value == ""
	case json.Number:
		n, err := strconv.ParseFloat(string(value), 64)
		return err == nil && n == 0
	case float64:
		return value == 0
	case float32:
		return value == 0
	case int:
		return value == 0
	case int64:
		return value == 0
	case map[string]any:
		return len(value) == 0
	case []any:
		return len(value) == 0
	default:
		return false
	}
}

func fieldPathParts(path string) ([]string, error) {
	parts := strings.Split(path, ".")
	if !fieldPathPattern.MatchString(path) {
		return nil, fmt.Errorf("invalid --only field path %q: expected dotted object keys", path)
	}
	return parts, nil
}

func cloneConfigValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			out[k] = cloneConfigValue(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = cloneConfigValue(child)
		}
		return out
	default:
		return value
	}
}

func readFieldPath(root map[string]any, path string, parts []string) (any, error) {
	var current any = root
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("--only field path %s is not present in spec", path)
		}
		next, present := obj[part]
		if !present {
			return nil, fmt.Errorf("--only field path %s is not present in spec", path)
		}
		current = next
	}
	return cloneConfigValue(current), nil
}

func readOptionalFieldPath(root map[string]any, parts []string) (any, bool) {
	var current any = root
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, present := obj[part]
		if !present {
			return nil, false
		}
		current = next
	}
	return current, true
}

func writeFieldPath(root map[string]any, parts []string, value any) {
	current := root
	for _, part := range parts[:len(parts)-1] {
		child, ok := current[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			current[part] = child
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
}

// mergeOnlyFields clones live and copies only selected safe dotted paths from
// spec. It returns the selected paths whose values actually differ.
func mergeOnlyFields(live, spec map[string]any, onlyPaths []string) (map[string]any, []string, error) {
	merged, _ := cloneConfigValue(live).(map[string]any)
	changed := make([]string, 0, len(onlyPaths))
	for _, path := range onlyPaths {
		parts, err := fieldPathParts(path)
		if err != nil {
			return nil, nil, err
		}
		specValue, err := readFieldPath(spec, path, parts)
		if err != nil {
			return nil, nil, err
		}
		liveValue, present := readOptionalFieldPath(live, parts)
		if !present && isAbsentEquivalent(specValue) {
			continue
		}
		if !present || !reflect.DeepEqual(liveValue, specValue) {
			changed = append(changed, path)
			writeFieldPath(merged, parts, specValue)
		}
	}
	sort.Strings(changed)
	return merged, changed, nil
}
