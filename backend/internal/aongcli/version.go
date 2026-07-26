package aongcli

import "strings"

// Build metadata, stamped by release tooling with -ldflags exactly as `ao`'s is.
// aong deliberately keeps its own vars rather than importing the `ao` CLI
// package: linking that package in would pull the whole daemon into a binary
// whose entire job is to shell out to it.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func versionString() string {
	parts := []string{Version}
	if Commit != "" {
		parts = append(parts, "commit "+Commit)
	}
	if Date != "" {
		parts = append(parts, "built "+Date)
	}
	return strings.Join(parts, " ")
}
