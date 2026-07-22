package domain

import "testing"

func TestWorkerMixValidate(t *testing.T) {
	tests := []struct {
		name    string
		mix     WorkerMix
		wantErr bool
	}{
		{"empty ok", WorkerMix{}, false},
		{"nil ok", nil, false},
		{
			"valid 60/30/10",
			WorkerMix{
				{Harness: HarnessCodex, Weight: 60},
				{Harness: HarnessDroid, Weight: 30},
				{Harness: HarnessClaudeCode, Weight: 10},
			},
			false,
		},
		{"single 100", WorkerMix{{Harness: HarnessClaudeCode, Weight: 100}}, false},
		{
			"sum under 100",
			WorkerMix{{Harness: HarnessCodex, Weight: 60}, {Harness: HarnessClaudeCode, Weight: 30}},
			true,
		},
		{
			"sum over 100",
			WorkerMix{{Harness: HarnessCodex, Weight: 60}, {Harness: HarnessClaudeCode, Weight: 60}},
			true,
		},
		{"unknown harness", WorkerMix{{Harness: "nope", Weight: 100}}, true},
		{"weight zero", WorkerMix{{Harness: HarnessClaudeCode, Weight: 0}, {Harness: HarnessCodex, Weight: 100}}, true},
		{"weight over 100", WorkerMix{{Harness: HarnessClaudeCode, Weight: 101}}, true},
		{"negative weight", WorkerMix{{Harness: HarnessClaudeCode, Weight: -10}, {Harness: HarnessCodex, Weight: 110}}, true},
		{
			"compatible model ok",
			WorkerMix{{Harness: HarnessClaudeCode, Model: "opus", Weight: 100}},
			false,
		},
		{
			"cross-provider model rejected",
			WorkerMix{{Harness: HarnessCodex, Model: "claude-opus-4-8", Weight: 100}},
			true,
		},
		{
			// An unclassifiable model on a known harness stays permitted — guarding
			// only fires on a known-vs-known mismatch.
			"unknown model on known harness ok",
			WorkerMix{{Harness: HarnessCodex, Model: "some-internal-model", Weight: 100}},
			false,
		},
		{
			// Fable is explicitly allowed as a user-weighted bucket (GH #61 bans
			// only auto-defaulting to fable, never an explicit weight).
			"explicit fable allowed",
			WorkerMix{{Harness: HarnessClaudeCode, Model: "claude-fable-5", Weight: 100}},
			false,
		},
		{
			"duplicate bucket rejected",
			WorkerMix{
				{Harness: HarnessClaudeCode, Model: "opus", Weight: 50},
				{Harness: HarnessClaudeCode, Model: "opus", Weight: 50},
			},
			true,
		},
		{
			// A padded model validates its provider but would count against a
			// different (trimmed) bucket key at spawn, so it is rejected outright.
			"whitespace-padded model rejected",
			WorkerMix{{Harness: HarnessClaudeCode, Model: " opus ", Weight: 100}},
			true,
		},
		{
			// Same harness, different model is NOT a duplicate — the model is part
			// of the bucket identity.
			"same harness different model ok",
			WorkerMix{
				{Harness: HarnessClaudeCode, Model: "opus", Weight: 50},
				{Harness: HarnessClaudeCode, Model: "sonnet", Weight: 50},
			},
			false,
		},
		{
			"same harness and model with different effort ok",
			WorkerMix{
				{Harness: HarnessCodex, Model: "gpt-5-codex", Effort: EffortLow, Weight: 50},
				{Harness: HarnessCodex, Model: "gpt-5-codex", Effort: EffortHigh, Weight: 50},
			},
			false,
		},
		{
			"invalid effort rejected",
			WorkerMix{{Harness: HarnessCodex, Effort: "turbo", Weight: 100}},
			true,
		},
		{
			"native-equivalent fugu efforts are duplicate buckets",
			WorkerMix{
				{Harness: HarnessCodexFugu, Model: "fugu", Effort: EffortMax, Weight: 50},
				{Harness: HarnessCodexFugu, Model: "fugu", Effort: EffortXHigh, Weight: 50},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.mix.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorkerMixSelectEmpty(t *testing.T) {
	if _, ok := (WorkerMix{}).Select(nil); ok {
		t.Fatal("empty mix should select nothing")
	}
	if _, ok := WorkerMix(nil).Select(map[BucketKey]int{{Harness: HarnessCodex}: 3}); ok {
		t.Fatal("nil mix should select nothing even with a populated census")
	}
}

func TestWorkerMixSelectSingle(t *testing.T) {
	mix := WorkerMix{{Harness: HarnessCodexFugu, Model: "fugu", Effort: EffortMax, Weight: 100}}
	got, ok := mix.Select(map[BucketKey]int{{Harness: HarnessCodexFugu, Model: "fugu", Effort: EffortXHigh}: 99})
	if !ok || got.Harness != HarnessCodexFugu || got.Model != "fugu" || got.Effort != EffortMax {
		t.Fatalf("single-bucket select = %+v ok=%v, want codex-fugu/fugu/max", got, ok)
	}
}

// TestWorkerMixSelectConverges drives the selector the way Spawn does — pick a
// bucket, increment its running count, repeat — and asserts the fleet lands on
// the exact target apportionment. 60/30/10 over 10 spawns is 6/3/1, and the
// deterministic earliest-row tie-break makes the whole sequence reproducible.
func TestWorkerMixSelectConverges(t *testing.T) {
	mix := WorkerMix{
		{Harness: HarnessCodex, Weight: 60},
		{Harness: HarnessDroid, Weight: 30},
		{Harness: HarnessClaudeCode, Weight: 10},
	}
	running := map[BucketKey]int{}
	for i := 0; i < 10; i++ {
		pick, ok := mix.Select(running)
		if !ok {
			t.Fatalf("spawn %d: mix selected nothing", i)
		}
		running[pick.BucketKey()]++
	}
	want := map[AgentHarness]int{HarnessCodex: 6, HarnessDroid: 3, HarnessClaudeCode: 1}
	for h, w := range want {
		if got := running[BucketKey{Harness: h}]; got != w {
			t.Fatalf("after 10 spawns %s = %d, want %d (running=%v)", h, got, w, running)
		}
	}
}

// TestWorkerMixSelectDeterministic pins the purity contract: Select consults no
// clock, no random source, and no retained state, so identical inputs must
// return an identical bucket however many times it is called.
func TestWorkerMixSelectDeterministic(t *testing.T) {
	mix := WorkerMix{
		{Harness: HarnessCodex, Weight: 60},
		{Harness: HarnessDroid, Weight: 30},
		{Harness: HarnessClaudeCode, Weight: 10},
	}
	running := map[BucketKey]int{
		{Harness: HarnessCodex}:      4,
		{Harness: HarnessDroid}:      1,
		{Harness: HarnessClaudeCode}: 0,
	}
	first, ok := mix.Select(running)
	if !ok {
		t.Fatal("first select returned nothing")
	}
	for i := 0; i < 5; i++ {
		got, ok := mix.Select(running)
		if !ok {
			t.Fatalf("select %d returned nothing", i)
		}
		if got != first {
			t.Fatalf("select %d = %+v, want %+v (selection must be a pure function of mix and census)", i, got, first)
		}
	}
}

// TestWorkerMixSelectTieBreaksToEarliestRow pins the tie rule. Both buckets
// below yield weight/(live+1) == 30, so the winner is decided purely by mix
// order; swapping the rows must swap the winner.
func TestWorkerMixSelectTieBreaksToEarliestRow(t *testing.T) {
	// heavy carries one live worker (60/(1+1) = 30); light carries none
	// (30/(0+1) = 30). Both averages are exactly 30, so only order decides.
	heavy := WorkerMixEntry{Harness: HarnessCodex, Weight: 60}
	light := WorkerMixEntry{Harness: HarnessClaudeCode, Weight: 30}
	running := map[BucketKey]int{{Harness: HarnessCodex}: 1}

	got, ok := WorkerMix{heavy, light}.Select(running)
	if !ok || got.Harness != HarnessCodex {
		t.Fatalf("select = %+v ok=%v, want codex (earliest row wins the tie)", got, ok)
	}
	got, ok = WorkerMix{light, heavy}.Select(running)
	if !ok || got.Harness != HarnessClaudeCode {
		t.Fatalf("select = %+v ok=%v, want claude-code (earliest row wins the tie)", got, ok)
	}
}

// TestWorkerMixSelectKeyTrimsModel confirms bucket identity is trimmed, so a
// selector fed a padded config model still matches the trimmed model recorded on
// running sessions rather than over-serving that bucket forever.
func TestWorkerMixSelectKeyTrimsModel(t *testing.T) {
	mix := WorkerMix{
		{Harness: HarnessClaudeCode, Model: " opus", Weight: 50},
		{Harness: HarnessCodex, Weight: 50},
	}
	// The claude bucket already has one running session, counted under the trimmed
	// model as Spawn would persist it.
	running := map[BucketKey]int{{Harness: HarnessClaudeCode, Model: "opus"}: 1}
	got, ok := mix.Select(running)
	if !ok || got.Harness != HarnessCodex {
		t.Fatalf("select = %+v ok=%v, want codex (claude bucket already served)", got, ok)
	}
}

// TestWorkerMixSelectIgnoresForeignBuckets confirms running sessions that match
// no configured bucket (e.g. an explicit haiku deploy) don't perturb selection.
func TestWorkerMixSelectIgnoresForeignBuckets(t *testing.T) {
	mix := WorkerMix{
		{Harness: HarnessCodex, Weight: 50},
		{Harness: HarnessClaudeCode, Weight: 50},
	}
	running := map[BucketKey]int{
		{Harness: HarnessClaudeCode, Model: "haiku"}: 5, // foreign: bucket has no model pin
		{Harness: HarnessDroid}:                      3, // foreign: harness not in mix
	}
	// Both configured buckets are still at count 0, so the earliest row wins.
	got, ok := mix.Select(running)
	if !ok || got.Harness != HarnessCodex {
		t.Fatalf("select = %+v ok=%v, want codex (foreign buckets ignored)", got, ok)
	}
}
