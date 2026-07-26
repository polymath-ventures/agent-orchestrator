package httpd

import "runtime/debug"

// buildRevisionUnknown is what a daemon reports when its binary carries no VCS
// stamp. It is a value rather than an omitted field on purpose: a caller
// checking provenance must be able to tell "this build cannot say" apart from
// "this daemon predates the field", and an absent key conflates the two.
const buildRevisionUnknown = "unknown"

// buildRevision reports the commit this binary was built from.
//
// The daemon already reports where its binary lives (executablePath), which
// answers "which file is answering" but not "what is that file". Anyone without
// read access to the deploy host's binary and journal — another account, CI, a
// browser — otherwise has no way to confirm that a running daemon is the
// artifact a given commit produced.
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	return formatBuildRevision(info, ok)
}

// formatBuildRevision is split from its caller so both stamped shapes stay
// testable: a `go test` binary carries no vcs settings, so through
// debug.ReadBuildInfo alone only the unknown path is ever reachable.
//
// A dirty build is reported as one suffixed string rather than as a separate
// boolean beside the revision. Two fields would be two copies of one truth that
// have to agree, and the consumer this exists for — a deploy gate asking "is
// this exactly commit X, built clean?" — answers that with a single equality
// check, which a suffixed revision fails exactly when it should.
func formatBuildRevision(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return buildRevisionUnknown
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision == "" {
		return buildRevisionUnknown
	}
	if modified == "true" {
		return revision + "-dirty"
	}
	return revision
}
