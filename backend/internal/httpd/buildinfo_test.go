package httpd

import (
	"runtime/debug"
	"strings"
	"testing"
)

// The stamped shapes are unreachable from a `go test` binary — the test binary
// carries no vcs settings of its own — so the formatting rules are pinned
// against constructed build info. Without this, only the unknown path would
// ever be exercised, and the one case the field exists for (a real deploy
// reporting its exact commit) would ship untested.
func TestFormatBuildRevision(t *testing.T) {
	const sha = "5ea0e09be000c3b019a9764002f4b0d6b3d799e6"
	info := func(settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: settings}
	}

	for _, tc := range []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{
			name: "clean build reports the bare revision so a deploy gate can compare it for equality",
			info: info(
				debug.BuildSetting{Key: "vcs", Value: "git"},
				debug.BuildSetting{Key: "vcs.revision", Value: sha},
				debug.BuildSetting{Key: "vcs.modified", Value: "false"},
			),
			ok:   true,
			want: sha,
		},
		{
			name: "dirty build is suffixed, so it can never equality-match the commit it was built near",
			info: info(
				debug.BuildSetting{Key: "vcs.revision", Value: sha},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			ok:   true,
			want: sha + "-dirty",
		},
		{
			name: "revision without a modified setting is reported as-is rather than guessed clean",
			info: info(debug.BuildSetting{Key: "vcs.revision", Value: sha}),
			ok:   true,
			want: sha,
		},
		{
			name: "build with no vcs stamps at all is explicitly unknown",
			info: info(debug.BuildSetting{Key: "-trimpath", Value: "true"}),
			ok:   true,
			want: buildRevisionUnknown,
		},
		{
			name: "unreadable build info is explicitly unknown, not empty",
			info: nil,
			ok:   false,
			want: buildRevisionUnknown,
		},
		{
			name: "ok with a nil info must not panic",
			info: nil,
			ok:   true,
			want: buildRevisionUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBuildRevision(tc.info, tc.ok); got != tc.want {
				t.Errorf("formatBuildRevision() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The field's whole purpose is that a caller who cannot read the binary can
// still learn what it is, so it must never be silently absent. An omitted field
// and an unknown build are different facts, and only the second one is allowed
// to be indistinguishable from "this build carries no stamp".
func TestBuildRevisionIsNeverEmpty(t *testing.T) {
	got := buildRevision()
	if strings.TrimSpace(got) == "" {
		t.Fatal("buildRevision() is empty; a consumer cannot distinguish that from a missing field")
	}
}
