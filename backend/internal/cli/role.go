package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// rolePromptResult mirrors the daemon's RolePromptResponse body for
// GET /api/v1/projects/{id}/roles/{role}/prompt.
type rolePromptResult struct {
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

// supportedRoles are the roles whose assembled prompt can be inspected. The CLI
// validates against this set so an unknown role is a usage error (exit 2)
// rather than a round trip to the daemon.
var supportedRoles = map[string]bool{"worker": true, "orchestrator": true, "reviewer": true}

func newRoleCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Inspect per-role configuration",
	}
	cmd.AddCommand(newRolePromptCommand(ctx))
	return cmd
}

func newRolePromptCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "prompt <project> <role>",
		Short: "Print the exact assembled system prompt a role receives for a project",
		Long: "Print the exact, fully-assembled system prompt an agent role receives for a " +
			"project — the base scaffold plus every injected operator instruction source. " +
			"Role is one of: worker, orchestrator, reviewer.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return usageError{err}
			}
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("usage: project id is required")}
			}
			role := strings.TrimSpace(args[1])
			if !supportedRoles[role] {
				return usageError{fmt.Errorf("usage: role must be one of worker, orchestrator, reviewer (got %q)", args[1])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			project := strings.TrimSpace(args[0])
			role := strings.TrimSpace(args[1])
			var res rolePromptResult
			path := "projects/" + url.PathEscape(project) + "/roles/" + url.PathEscape(role) + "/prompt"
			if err := ctx.getJSON(cmd.Context(), path, &res); err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Prompt)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the role prompt as JSON")
	return cmd
}
