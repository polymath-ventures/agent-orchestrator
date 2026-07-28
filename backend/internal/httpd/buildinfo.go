package httpd

import "runtime/debug"

// buildRevisionUnknown is what a daemon reports when its binary carries no VCS
// stamp. It is a value rather than an omitted field on purpose: a caller
// checking provenance must be able to tell "this build cannot say" apart from
// "this daemon predates the field", and an absent key conflates the two.
const buildRevisionUnknown = "unknown"

// buildProvenance reports the commit this binary was built from, and whether
// the tree that produced it was clean.
//
// The daemon already reports where its binary lives (executablePath), which
// answers "which file is answering" but not "what is that file". Anyone without
// read access to the deploy host's binary and journal — another account, CI, a
// browser — otherwise has no way to confirm that a running daemon is the
// artifact a given commit produced.
func buildProvenance() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	return formatBuildProvenance(info, ok)
}

// formatBuildProvenance is split from its caller so both stamped shapes stay
// testable: a `go test` binary carries no vcs settings, so through
// debug.ReadBuildInfo alone only the unknown path is ever reachable.
//
// modified fails CLOSED — it is false only on an explicit vcs.modified=false.
// A missing or unrecognized value means the tree's state was never established,
// and reporting that as clean would let a deploy gate accept a build whose
// provenance it could not actually confirm. Read it as "not known to be a clean
// build of revision", which is also why an unknown revision reports true.
func formatBuildProvenance(info *debug.BuildInfo, ok bool) (revision string, modified bool) {
	if !ok || info == nil {
		return buildRevisionUnknown, true
	}
	var vcsModified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			vcsModified = setting.Value
		}
	}
	if revision == "" {
		return buildRevisionUnknown, true
	}
	return revision, vcsModified != "false"
}
