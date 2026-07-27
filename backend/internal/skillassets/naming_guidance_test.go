package skillassets

import (
	"io/fs"
	"strings"
	"testing"
)

// namingGuidanceFiles are the shipped, agent-facing artifacts that describe how
// to spawn a session. The daemon embeds this skill and installs it for every
// worker to read, so on this path documentation is executable configuration: an
// agent that reads "--name is required" supplies a name, and an explicit name
// outranks the computed one — which is precisely how the prior implementation's
// computed-name path became unreachable, and how orchestrators dispatching with
// `--prompt "/address-issue <id>"` ended up with workers named after the prompt.
//
// Listing the files explicitly (rather than walking for whatever happens to
// exist) is what lets the guard fail when one goes missing.
var namingGuidanceFiles = []string{
	"using-ao/SKILL.md",
	"using-ao/references.md",
	"using-ao/commands/doctor.md",
	"using-ao/commands/drain.md",
	"using-ao/commands/import.md",
	"using-ao/commands/orchestrator.md",
	"using-ao/commands/pause.md",
	"using-ao/commands/preview.md",
	"using-ao/commands/project.md",
	"using-ao/commands/review.md",
	"using-ao/commands/resume.md",
	"using-ao/commands/send.md",
	"using-ao/commands/session.md",
	"using-ao/commands/shutdown.md",
	"using-ao/commands/spawn.md",
	"using-ao/commands/start.md",
	"using-ao/commands/status.md",
	"using-ao/commands/stop.md",
	"using-ao/commands/stop-work.md",
}

// TestShippedGuidanceDoesNotTeachAgentsToNameSessions is the guard the design
// calls for in place of a convention. Prose regressing here silently re-breaks
// the feature, which is what earns it a test.
//
// The missing-file half matters on its own: the prior implementation's first
// version of this guard skipped files it could not read, so it would have passed
// vacuously the moment a file was renamed.
func TestShippedGuidanceDoesNotTeachAgentsToNameSessions(t *testing.T) {
	for _, name := range namingGuidanceFiles {
		t.Run(name, func(t *testing.T) {
			body, err := files.ReadFile(name)
			if err != nil {
				t.Fatalf("shipped guidance %s is missing: %v — update namingGuidanceFiles rather than letting the guard skip it", name, err)
			}
			for i, line := range strings.Split(string(body), "\n") {
				if (strings.Contains(line, "ao spawn") || strings.Contains(line, "aong spawn")) && strings.Contains(line, "--name") {
					t.Errorf("%s:%d pairs a spawn instruction with the name flag; the daemon computes the name:\n\t%s", name, i+1, strings.TrimSpace(line))
				}
				if strings.Contains(line, "--name") && strings.Contains(line, "Required") {
					t.Errorf("%s:%d describes --name as required:\n\t%s", name, i+1, strings.TrimSpace(line))
				}
			}
		})
	}
}

// Every command doc the skill ships must be covered, so adding one cannot
// quietly escape the guard above.
func TestNamingGuidanceCoversEveryShippedCommandDoc(t *testing.T) {
	covered := make(map[string]bool, len(namingGuidanceFiles))
	for _, name := range namingGuidanceFiles {
		covered[name] = true
	}
	err := fs.WalkDir(files, "using-ao/commands", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if !covered[path] {
			t.Errorf("shipped command doc %s is not covered by namingGuidanceFiles", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk shipped command docs: %v", err)
	}
}
