package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// projectConfigRaw captures a project's config as raw JSON from GET
// /projects/{id}, bypassing the typed CLI mirror (projectConfig) so every field
// the daemon serializes survives export untouched.
type projectConfigRaw struct {
	Project struct {
		Config json.RawMessage `json:"config"`
	} `json:"project"`
}

func newProjectConfigCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Export, apply, and diff a project's config as code",
		Long: "Treat a project's effective config as versionable JSON.\n\n" +
			"  export <project>        print the full config as canonical JSON\n" +
			"  apply  <project> <file> apply only the fields named in a spec file\n" +
			"  diff   <project> <file> report drift between a spec file and live config",
	}
	cmd.AddCommand(newProjectConfigExportCommand(ctx))
	cmd.AddCommand(newProjectConfigApplyCommand(ctx))
	return cmd
}

// projectAndSpecArgs validates the `<project> <file>` argument pair shared by
// apply and diff: exactly two args, project id non-empty.
func projectAndSpecArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(2)(cmd, args); err != nil {
		return usageError{err}
	}
	if strings.TrimSpace(args[0]) == "" {
		return usageError{errors.New("usage: project id is required")}
	}
	return nil
}

// readSpecFile reads and parses a JSON config spec file. A missing/unreadable
// file or invalid JSON is a usage error (exit 2) — CLI misuse, not a daemon
// failure — so the spec's top-level keys map to "the fields named in the spec".
func readSpecFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied config spec path.
	if err != nil {
		return nil, usageError{fmt.Errorf("read spec file: %w", err)}
	}
	spec, err := parseConfigObject(raw)
	if err != nil {
		return nil, usageError{fmt.Errorf("parse spec file %s: %w", path, err)}
	}
	return spec, nil
}

// fetchProjectConfig reads a project's live config as a canonical map. Returns a
// clear error if the project id is empty (usage) or the daemon call fails.
func (ctx *commandContext) fetchProjectConfig(cmd *cobra.Command, id string) (map[string]any, error) {
	var raw projectConfigRaw
	if err := ctx.getJSON(cmd.Context(), "projects/"+url.PathEscape(id), &raw); err != nil {
		return nil, err
	}
	return parseConfigObject(raw.Project.Config)
}

func newProjectConfigExportCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "export <project>",
		Short: "Print a project's full config as canonical JSON",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("usage: project id is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			config, err := ctx.fetchProjectConfig(cmd, id)
			if err != nil {
				return err
			}
			canonical, err := canonicalizeConfigMap(config)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(canonical))
			return err
		},
	}
}

func newProjectConfigApplyCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "apply <project> <file>",
		Short: "Apply only the fields named in a spec file to a project's config",
		Long: "Surgically apply a JSON config spec: only the top-level fields named " +
			"in <file> change; every other live field is preserved. A spec equal to " +
			"the current config makes no change and performs no write.",
		Args: projectAndSpecArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			spec, err := readSpecFile(args[1])
			if err != nil {
				return err
			}
			live, err := ctx.fetchProjectConfig(cmd, id)
			if err != nil {
				return err
			}
			merged, changed := overlayConfig(live, spec)
			if len(changed) == 0 {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "no changes: project %s config already matches spec\n", id)
				return err
			}
			body := map[string]any{"config": merged}
			if err := ctx.putJSON(cmd.Context(), "projects/"+url.PathEscape(id)+"/config", body, nil); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "updated config for project %s (%d field(s): %s)\n",
				id, len(changed), strings.Join(changed, ", "))
			return err
		},
	}
}
