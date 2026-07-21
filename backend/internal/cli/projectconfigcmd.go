package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	return cmd
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
