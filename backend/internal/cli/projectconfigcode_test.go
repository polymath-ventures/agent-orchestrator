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

// mustNumber builds a json.Number the way UseNumber-decoded config carries them,
// so test expectations compare like-for-like with parsed config values.
func mustNumber(s string) any {
	return json.Number(s)
}
