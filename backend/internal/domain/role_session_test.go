package domain_test

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestSessionKindIsRole(t *testing.T) {
	t.Parallel()

	cases := map[domain.SessionKind]bool{
		domain.KindPrime:        true,
		domain.KindOrchestrator: true,
		domain.KindWorker:       false,
		domain.SessionKind(""):  false,
		"bogus":                 false,
	}
	for kind, want := range cases {
		if got := kind.IsRole(); got != want {
			t.Errorf("SessionKind(%q).IsRole() = %v, want %v", kind, got, want)
		}
	}
}

func TestRoleTargetConstructors(t *testing.T) {
	t.Parallel()

	prime := domain.PrimeTarget()
	if prime.Kind != domain.KindPrime {
		t.Errorf("PrimeTarget().Kind = %q, want %q", prime.Kind, domain.KindPrime)
	}
	if prime.ProjectID != "" {
		t.Errorf("PrimeTarget().ProjectID = %q, want empty (prime is projectless)", prime.ProjectID)
	}

	orch := domain.OrchestratorTarget("acme")
	if orch.Kind != domain.KindOrchestrator {
		t.Errorf("OrchestratorTarget().Kind = %q, want %q", orch.Kind, domain.KindOrchestrator)
	}
	if orch.ProjectID != "acme" {
		t.Errorf("OrchestratorTarget().ProjectID = %q, want %q", orch.ProjectID, "acme")
	}
}

func TestRoleTargetValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  domain.RoleTarget
		wantErr bool
	}{
		{name: "prime", target: domain.PrimeTarget()},
		{name: "orchestrator", target: domain.OrchestratorTarget("acme")},
		{
			name:    "orchestrator without project",
			target:  domain.RoleTarget{Kind: domain.KindOrchestrator},
			wantErr: true,
		},
		{
			name:    "prime with project",
			target:  domain.RoleTarget{Kind: domain.KindPrime, ProjectID: "acme"},
			wantErr: true,
		},
		{
			name:    "worker is not a role",
			target:  domain.RoleTarget{Kind: domain.KindWorker, ProjectID: "acme"},
			wantErr: true,
		},
		{
			name:    "empty kind",
			target:  domain.RoleTarget{},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.target.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// The lock key replaces the "__fleet_prime__" magic constant the service used to
// borrow from the orchestrator project lock map. Distinct targets must never
// collide, or two role reconciles would serialize against each other (or worse,
// not serialize at all).
func TestRoleTargetKeyIsStableAndDistinct(t *testing.T) {
	t.Parallel()

	seen := map[string]domain.RoleTarget{}
	targets := []domain.RoleTarget{
		domain.PrimeTarget(),
		domain.OrchestratorTarget("acme"),
		domain.OrchestratorTarget("beta"),
	}
	for _, target := range targets {
		key := target.Key()
		if key == "" {
			t.Fatalf("Key() for %+v is empty", target)
		}
		if prior, dup := seen[key]; dup {
			t.Fatalf("Key() collision: %+v and %+v both produced %q", prior, target, key)
		}
		seen[key] = target
		if again := target.Key(); again != key {
			t.Errorf("Key() is not stable: got %q then %q", key, again)
		}
	}

	// A project literally named after the old magic constant must not collide
	// with the prime target.
	if domain.OrchestratorTarget("__fleet_prime__").Key() == domain.PrimeTarget().Key() {
		t.Error("orchestrator project named __fleet_prime__ collides with the prime lock key")
	}
}

func TestRoleTargetForSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rec    domain.SessionRecord
		want   domain.RoleTarget
		wantOK bool
	}{
		{
			name:   "prime session",
			rec:    domain.SessionRecord{Kind: domain.KindPrime},
			want:   domain.PrimeTarget(),
			wantOK: true,
		},
		{
			name:   "orchestrator session",
			rec:    domain.SessionRecord{Kind: domain.KindOrchestrator, ProjectID: "acme"},
			want:   domain.OrchestratorTarget("acme"),
			wantOK: true,
		},
		{
			name:   "worker session has no role target",
			rec:    domain.SessionRecord{Kind: domain.KindWorker, ProjectID: "acme"},
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.RoleTargetForSession(tc.rec)
			if ok != tc.wantOK {
				t.Fatalf("RoleTargetForSession() ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("RoleTargetForSession() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
