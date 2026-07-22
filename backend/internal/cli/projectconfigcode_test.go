package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCanonicalizeConfig_ByteStableAndSorted(t *testing.T) {
	// Same logical config, different key order and whitespace on input.
	inA := []byte(`{"sessionPrefix":"demo","defaultBranch":"main","maxLiveWorkers":3}`)
	inB := []byte(`{  "maxLiveWorkers": 3,
	"defaultBranch": "main",
		"sessionPrefix": "demo" }`)

	outA, err := canonicalizeConfig(inA)
	if err != nil {
		t.Fatalf("canonicalize inA: %v", err)
	}
	outB, err := canonicalizeConfig(inB)
	if err != nil {
		t.Fatalf("canonicalize inB: %v", err)
	}
	if string(outA) != string(outB) {
		t.Fatalf("canonical outputs differ:\nA=%s\nB=%s", outA, outB)
	}

	// Idempotent: canonicalizing canonical output yields the same bytes.
	outAA, err := canonicalizeConfig(outA)
	if err != nil {
		t.Fatalf("re-canonicalize: %v", err)
	}
	if string(outAA) != string(outA) {
		t.Fatalf("canonicalize not idempotent:\nfirst=%s\nsecond=%s", outA, outAA)
	}

	// Keys are sorted: defaultBranch before maxLiveWorkers before sessionPrefix.
	want := "{\n  \"defaultBranch\": \"main\",\n  \"maxLiveWorkers\": 3,\n  \"sessionPrefix\": \"demo\"\n}\n"
	if string(outA) != want {
		t.Fatalf("canonical form = %q, want %q", outA, want)
	}
}

func TestCanonicalizeConfig_PreservesIntegers(t *testing.T) {
	in := []byte(`{"maxLiveWorkers":10}`)
	out, err := canonicalizeConfig(in)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	// Integer must not be reformatted to 10.0 / 1e1.
	want := "{\n  \"maxLiveWorkers\": 10\n}\n"
	if string(out) != want {
		t.Fatalf("integer round-trip = %q, want %q", out, want)
	}
}

func TestOverlayConfig_ChangesOnlyNamedKeys(t *testing.T) {
	live := map[string]any{
		"defaultBranch":  "main",
		"sessionPrefix":  "demo",
		"maxLiveWorkers": mustNumber("3"),
		"env":            map[string]any{"FOO": "bar"},
	}
	spec := map[string]any{
		"sessionPrefix":  "prod",
		"maxLiveWorkers": mustNumber("5"),
	}

	merged, changed := overlayConfig(live, spec)

	// Named keys take new values.
	if merged["sessionPrefix"] != "prod" {
		t.Fatalf("sessionPrefix = %v, want prod", merged["sessionPrefix"])
	}
	if merged["maxLiveWorkers"] != mustNumber("5") {
		t.Fatalf("maxLiveWorkers = %v, want 5", merged["maxLiveWorkers"])
	}
	// Unnamed keys are untouched.
	if merged["defaultBranch"] != "main" {
		t.Fatalf("defaultBranch = %v, want main (unchanged)", merged["defaultBranch"])
	}
	if !reflect.DeepEqual(merged["env"], map[string]any{"FOO": "bar"}) {
		t.Fatalf("env = %v, want unchanged", merged["env"])
	}
	// Overlay must not mutate the live map in place.
	if live["sessionPrefix"] != "demo" {
		t.Fatalf("overlay mutated live map: sessionPrefix = %v", live["sessionPrefix"])
	}
	// changed lists exactly the keys whose value differs, sorted.
	if !reflect.DeepEqual(changed, []string{"maxLiveWorkers", "sessionPrefix"}) {
		t.Fatalf("changed = %v, want [maxLiveWorkers sessionPrefix]", changed)
	}
}

func TestOverlayConfig_EqualSpecReportsNoChange(t *testing.T) {
	live := map[string]any{
		"defaultBranch":  "main",
		"maxLiveWorkers": mustNumber("3"),
	}
	// Spec equals the full live config.
	spec := map[string]any{
		"defaultBranch":  "main",
		"maxLiveWorkers": mustNumber("3"),
	}
	merged, changed := overlayConfig(live, spec)
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
	if !reflect.DeepEqual(merged, live) {
		t.Fatalf("merged = %v, want equal to live", merged)
	}
}

func TestDiffConfig_MatchingHasNoDrift(t *testing.T) {
	live := map[string]any{"defaultBranch": "main", "sessionPrefix": "demo"}
	spec := map[string]any{"defaultBranch": "main"}
	drift := diffConfig(live, spec)
	if len(drift) != 0 {
		t.Fatalf("drift = %v, want none", drift)
	}
}

func TestDiffConfig_NamesEachDriftedField(t *testing.T) {
	live := map[string]any{
		"defaultBranch":  "main",
		"maxLiveWorkers": mustNumber("3"),
		"sessionPrefix":  "demo",
	}
	spec := map[string]any{
		"defaultBranch":  "release",
		"maxLiveWorkers": mustNumber("5"),
	}
	drift := diffConfig(live, spec)
	if len(drift) != 2 {
		t.Fatalf("drift count = %d, want 2 (%v)", len(drift), drift)
	}
	// Sorted by field name.
	if drift[0].Field != "defaultBranch" || drift[1].Field != "maxLiveWorkers" {
		t.Fatalf("drift fields = %s,%s want defaultBranch,maxLiveWorkers", drift[0].Field, drift[1].Field)
	}
	if drift[0].Spec != "release" || drift[0].Live != "main" {
		t.Fatalf("defaultBranch drift = spec %v live %v, want release/main", drift[0].Spec, drift[0].Live)
	}
}

func TestDiffConfig_IgnoresUnnamedFields(t *testing.T) {
	live := map[string]any{"defaultBranch": "main", "sessionPrefix": "changed-in-live"}
	spec := map[string]any{"defaultBranch": "main"} // only names defaultBranch
	drift := diffConfig(live, spec)
	if len(drift) != 0 {
		t.Fatalf("drift = %v, want none (sessionPrefix not named in spec)", drift)
	}
}

func TestDiffConfig_DistinguishesNullFromAbsent(t *testing.T) {
	// Spec sets a field to explicit null; live does not have the field at all.
	live := map[string]any{"defaultBranch": "main"}
	spec := map[string]any{"sessionPrefix": nil}
	drift := diffConfig(live, spec)
	if len(drift) != 1 {
		t.Fatalf("drift = %v, want one entry (null spec vs absent live is a change)", drift)
	}
	if drift[0].LivePresent {
		t.Fatalf("LivePresent = true, want false (field absent from live)")
	}

	// Now live explicitly holds null too → no drift.
	live2 := map[string]any{"sessionPrefix": nil}
	if d := diffConfig(live2, spec); len(d) != 0 {
		t.Fatalf("drift = %v, want none (null == null)", d)
	}
}

func TestAbsentEquivalentValuesConvergeButStillClearNonZeroLiveValues(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{
		"false":        false,
		"zero":         json.Number("0"),
		"empty-string": "",
		"empty-object": map[string]any{},
		"empty-array":  []any{},
	} {
		t.Run(name, func(t *testing.T) {
			live := map[string]any{}
			spec := map[string]any{"field": value}
			merged, changed := overlayConfig(live, spec)
			if len(changed) != 0 {
				t.Fatalf("changed = %v, want none", changed)
			}
			if _, present := merged["field"]; present {
				t.Fatalf("merged reintroduced omitted-equivalent value %#v", value)
			}
			if drift := diffConfig(live, spec); len(drift) != 0 {
				t.Fatalf("drift = %v, want none", drift)
			}
		})
	}

	live := map[string]any{"field": json.Number("7")}
	spec := map[string]any{"field": json.Number("0")}
	merged, changed := overlayConfig(live, spec)
	if !reflect.DeepEqual(changed, []string{"field"}) || merged["field"] != json.Number("0") {
		t.Fatalf("clear nonzero: merged=%v changed=%v", merged, changed)
	}
}

func TestDiffConfigUnexpectedModeReportsMeaningfulLiveOnlyFields(t *testing.T) {
	t.Parallel()

	live := map[string]any{
		"defaultBranch": "main",
		"sessionPrefix": "unexpected",
		"empty":         "",
	}
	spec := map[string]any{"defaultBranch": "main"}

	if drift := diffConfig(live, spec); len(drift) != 0 {
		t.Fatalf("default drift = %v, want none", drift)
	}
	drift := diffConfigUnexpected(live, spec)
	if len(drift) != 1 || drift[0].Field != "sessionPrefix" || drift[0].Kind != driftUnexpected {
		t.Fatalf("unexpected drift = %#v, want sessionPrefix only", drift)
	}
}

func TestMergeOnlyFieldsRestoresNestedPathsWithoutMutatingLive(t *testing.T) {
	t.Parallel()

	live := map[string]any{
		"worker": map[string]any{
			"agent":       "codex",
			"agentConfig": map[string]any{"model": "old", "effort": "high"},
		},
		"defaultBranch": "main",
	}
	spec := map[string]any{
		"worker": map[string]any{
			"agent":       "claude-code",
			"agentConfig": map[string]any{"model": "new", "effort": "low"},
		},
	}
	merged, changed, err := mergeOnlyFields(live, spec, []string{"worker.agentConfig.model"})
	if err != nil {
		t.Fatalf("mergeOnlyFields: %v", err)
	}
	if !reflect.DeepEqual(changed, []string{"worker.agentConfig.model"}) {
		t.Fatalf("changed = %v", changed)
	}
	worker := merged["worker"].(map[string]any)
	if worker["agent"] != "codex" {
		t.Fatalf("worker.agent = %v, want live codex", worker["agent"])
	}
	cfg := worker["agentConfig"].(map[string]any)
	if cfg["model"] != "new" || cfg["effort"] != "high" {
		t.Fatalf("merged agentConfig = %v", cfg)
	}
	if live["worker"].(map[string]any)["agentConfig"].(map[string]any)["model"] != "old" {
		t.Fatal("mergeOnlyFields mutated live")
	}
}

func TestMergeOnlyFieldsRejectsUnsafeAndMissingPaths(t *testing.T) {
	t.Parallel()

	spec := map[string]any{"worker": map[string]any{"agent": "codex"}}
	for _, path := range []string{"worker.agent.missing", "worker..agent"} {
		if _, _, err := mergeOnlyFields(map[string]any{}, spec, []string{path}); err == nil {
			t.Errorf("mergeOnlyFields accepted %q", path)
		}
	}
}

func TestLooksSecretKeyUsesPATBoundaries(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"GITHUB_TOKEN", "api_key", "DB_PASSWORD", "MY_PAT"} {
		if !looksSecretKey(key) {
			t.Errorf("looksSecretKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"PATH", "COMPAT", "POLYPOWERS_REPO"} {
		if looksSecretKey(key) {
			t.Errorf("looksSecretKey(%q) = true, want false", key)
		}
	}
}

func TestParseSpecObject_RejectsEmptyNullAndTrailing(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"whitespace":       "   \n ",
		"null":             "null",
		"trailing-object":  `{"a":1}{"b":2}`,
		"trailing-brace":   `{"a":1}}`,
		"trailing-bracket": `{"a":1}]`,
		"array":            `[1,2,3]`,
		"scalar":           `42`,
		"invalid":          `{not json`,
	}
	for name, in := range cases {
		if _, err := parseSpecObject([]byte(in)); err == nil {
			t.Errorf("parseSpecObject(%s=%q) = nil error, want error", name, in)
		}
	}
}

func TestParseSpecObject_AcceptsObjectWithTrailingWhitespace(t *testing.T) {
	// Canonical exports end with a trailing newline — must still parse.
	spec, err := parseSpecObject([]byte("{\n  \"defaultBranch\": \"main\"\n}\n"))
	if err != nil {
		t.Fatalf("parseSpecObject: %v", err)
	}
	if spec["defaultBranch"] != "main" {
		t.Fatalf("spec = %v, want defaultBranch=main", spec)
	}
}

func TestParseSpecObject_RejectsNullValuedField(t *testing.T) {
	// The config model has no nullable fields, so an explicit null can never
	// converge — it must be rejected at parse time.
	if _, err := parseSpecObject([]byte(`{"sessionPrefix":null}`)); err == nil {
		t.Fatal("parseSpecObject accepted a null-valued field; want error")
	}
	// A normal object still parses.
	if _, err := parseSpecObject([]byte(`{"sessionPrefix":"demo"}`)); err != nil {
		t.Fatalf("parseSpecObject rejected a valid object: %v", err)
	}

	// Nested nulls (e.g. inside the env map or a nested object) are rejected too —
	// they decode to an empty value the daemon can't distinguish from unset.
	nested := []string{
		`{"env":{"TOKEN":null}}`,
		`{"worker":{"agentConfig":{"model":null}}}`,
		`{"symlinks":["a",null]}`,
	}
	for _, in := range nested {
		if _, err := parseSpecObject([]byte(in)); err == nil {
			t.Errorf("parseSpecObject(%s) = nil error, want nested-null rejected", in)
		}
	}
}

// mustNumber builds a json.Number the way UseNumber-decoded config carries them,
// so test expectations compare like-for-like with parsed config values.
func mustNumber(s string) any {
	return json.Number(s)
}
