package httpd

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The stamped shapes are unreachable from a `go test` binary — the test binary
// carries no vcs settings of its own — so the rules are pinned against
// constructed build info. Without this, only the unknown path would ever be
// exercised, and the one case the fields exist for (a real deploy reporting its
// exact commit) would ship untested.
func TestFormatBuildProvenance(t *testing.T) {
	const sha = "5ea0e09be000c3b019a9764002f4b0d6b3d799e6"
	info := func(settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: settings}
	}

	for _, tc := range []struct {
		name         string
		info         *debug.BuildInfo
		ok           bool
		wantRevision string
		wantModified bool
	}{
		{
			name: "clean build reports its revision and says so",
			info: info(
				debug.BuildSetting{Key: "vcs", Value: "git"},
				debug.BuildSetting{Key: "vcs.revision", Value: sha},
				debug.BuildSetting{Key: "vcs.modified", Value: "false"},
			),
			ok:           true,
			wantRevision: sha,
			wantModified: false,
		},
		{
			name: "dirty build reports the revision it was built near, flagged",
			info: info(
				debug.BuildSetting{Key: "vcs.revision", Value: sha},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			ok:           true,
			wantRevision: sha,
			wantModified: true,
		},
		{
			// Fail closed. A missing setting means the tree's state was never
			// established, and a gate that reads "clean" here would accept a build
			// whose provenance it could not actually confirm.
			name:         "revision with no modified setting is not assumed clean",
			info:         info(debug.BuildSetting{Key: "vcs.revision", Value: sha}),
			ok:           true,
			wantRevision: sha,
			wantModified: true,
		},
		{
			name: "unrecognized modified value is not assumed clean either",
			info: info(
				debug.BuildSetting{Key: "vcs.revision", Value: sha},
				debug.BuildSetting{Key: "vcs.modified", Value: "maybe"},
			),
			ok:           true,
			wantRevision: sha,
			wantModified: true,
		},
		{
			name:         "build with no vcs stamps at all is explicitly unknown",
			info:         info(debug.BuildSetting{Key: "-trimpath", Value: "true"}),
			ok:           true,
			wantRevision: buildRevisionUnknown,
			wantModified: true,
		},
		{
			name:         "unreadable build info is explicitly unknown, not empty",
			info:         nil,
			ok:           false,
			wantRevision: buildRevisionUnknown,
			wantModified: true,
		},
		{
			name:         "ok with a nil info must not panic",
			info:         nil,
			ok:           true,
			wantRevision: buildRevisionUnknown,
			wantModified: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revision, modified := formatBuildProvenance(tc.info, tc.ok)
			if revision != tc.wantRevision {
				t.Errorf("revision = %q, want %q", revision, tc.wantRevision)
			}
			if modified != tc.wantModified {
				t.Errorf("modified = %v, want %v", modified, tc.wantModified)
			}
		})
	}
}

// The revision's whole purpose is that a caller who cannot read the binary can
// still learn what it is, so it must never be silently absent. An omitted field
// and an unknown build are different facts, and only the second one is allowed
// to be indistinguishable from "this build carries no stamp".
func TestBuildProvenanceRevisionIsNeverEmpty(t *testing.T) {
	revision, _ := buildProvenance()
	if strings.TrimSpace(revision) == "" {
		t.Fatal("buildProvenance() revision is empty; a consumer cannot distinguish that from a missing field")
	}
}
