package cli

import (
	"bytes"
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
		Config     json.RawMessage `json:"config"`
		ConfigETag string          `json:"configETag"`
	} `json:"project"`
}

func newProjectConfigCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Export, apply, and diff a project's config as code",
		Long: "Treat a project's stored config as versionable JSON.\n\n" +
			"  export <project>        print the full config as canonical JSON\n" +
			"  apply  <project> <file> apply only the fields named in a spec file\n" +
			"  diff   <project> <file> report drift between a spec file and live config",
	}
	cmd.AddCommand(newProjectConfigExportCommand(ctx))
	cmd.AddCommand(newProjectConfigApplyCommand(ctx))
	cmd.AddCommand(newProjectConfigDiffCommand(ctx))
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
	spec, err := parseSpecObject(raw)
	if err != nil {
		return nil, usageError{fmt.Errorf("parse spec file %s: %w", path, err)}
	}
	return spec, nil
}

// fetchProjectConfig reads a project's live config as a canonical map. Returns a
// clear error if the project id is empty (usage) or the daemon call fails.
func (ctx *commandContext) fetchProjectConfig(cmd *cobra.Command, id string) (map[string]any, string, error) {
	var raw projectConfigRaw
	if err := ctx.getJSON(cmd.Context(), "projects/"+url.PathEscape(id), &raw); err != nil {
		return nil, "", err
	}
	config, err := parseConfigObject(raw.Project.Config)
	return config, raw.Project.ConfigETag, err
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
			config, _, err := ctx.fetchProjectConfig(cmd, id)
			if err != nil {
				return err
			}
			allowed := map[string]struct{}{}
			for _, key := range strings.Split(os.Getenv("AO_PROJECT_CONFIG_ALLOW_ENV_KEYS"), ",") {
				if key = strings.TrimSpace(key); key != "" {
					allowed[key] = struct{}{}
				}
			}
			if offenders := secretEnvKeys(config, allowed); len(offenders) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: exported config contains secret-shaped env key(s): %s; review before committing (set AO_PROJECT_CONFIG_ALLOW_ENV_KEYS to exempt exact non-secret keys)\n",
					strings.Join(offenders, ", "))
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
	var onlyPaths []string
	cmd := &cobra.Command{
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
			live, etag, err := ctx.fetchProjectConfig(cmd, id)
			if err != nil {
				return err
			}
			var merged map[string]any
			var changed []string
			if len(onlyPaths) > 0 {
				merged, changed, err = mergeOnlyFields(live, spec, onlyPaths)
				if err != nil {
					return usageError{err}
				}
			} else {
				merged, changed = overlayConfig(live, spec)
			}
			if len(changed) == 0 {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "no changes: project %s config already matches spec\n", id)
				return err
			}
			body := map[string]any{"config": merged}
			if etag == "" {
				return errors.New("daemon project response did not include configETag; update the daemon before applying config")
			}
			if err := ctx.putJSONIfMatch(cmd.Context(), "projects/"+url.PathEscape(id)+"/config", etag, body, nil); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "updated config for project %s (%d field(s): %s)\n",
				id, len(changed), strings.Join(changed, ", "))
			return err
		},
	}
	cmd.Flags().StringArrayVar(&onlyPaths, "only", nil,
		"restore only this dotted object path from the spec (repeatable)")
	return cmd
}

func newProjectConfigDiffCommand(ctx *commandContext) *cobra.Command {
	var includeUnexpected bool
	cmd := &cobra.Command{
		Use:   "diff <project> <file>",
		Short: "Report drift between a spec file and a project's live config",
		Long: "Compare a JSON config spec against live config. Only fields named in " +
			"<file> are compared. Prints each drifted field (spec vs live) and exits " +
			"nonzero when they disagree, so it can gate CI or a scheduled drift check.",
		Args: projectAndSpecArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			spec, err := readSpecFile(args[1])
			if err != nil {
				return err
			}
			live, _, err := ctx.fetchProjectConfig(cmd, id)
			if err != nil {
				return err
			}
			drift := diffConfigWithUnexpected(live, spec, includeUnexpected)
			if len(drift) == 0 {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "no drift: project %s config matches spec\n", id)
				return err
			}
			out := cmd.OutOrStdout()
			var b strings.Builder
			fmt.Fprintf(&b, "drift in project %s config (%d field(s)):\n", id, len(drift))
			for _, d := range drift {
				specValue := "(absent)"
				if d.SpecPresent {
					specValue = specScalar(d.Field, d.Spec)
				}
				fmt.Fprintf(&b, "  %q [%s]: spec=%s live=%s\n",
					d.Field, d.Kind, specValue, liveScalar(d))
			}
			if _, err := fmt.Fprint(out, b.String()); err != nil {
				return err
			}
			// Non-usage error → nonzero exit; SilenceErrors keeps it off stderr.
			return fmt.Errorf("config drift: %d field(s) differ", len(drift))
		},
	}
	cmd.Flags().BoolVar(&includeUnexpected, "unexpected", false,
		"also report meaningful live fields absent from the spec")
	return cmd
}

// jsonScalar renders a decoded JSON value compactly for drift output. An
// explicit JSON null renders as `null` (json.Marshal(nil)).
func jsonScalar(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// sensitiveConfigField reports whether a config field's values may carry secrets
// (e.g. env vars) and so must be redacted in diff output, which can be captured
// in CI logs. Export stays lossless (it is the restore source and its secret
// exposure is documented); diff only needs to show which field drifted.
func sensitiveConfigField(field string) bool {
	return field == "env" || strings.HasPrefix(field, "env.") || looksSecretKey(field)
}

// specScalar renders the spec side of a drift entry, redacting the value of a
// sensitive field.
func specScalar(field string, v any) string {
	if sensitiveConfigField(field) {
		return "<redacted>"
	}
	return jsonScalar(redactSensitiveValue(field, v))
}

// liveScalar renders the live side of a drift entry: "(absent)" when the field is
// absent from live config (distinct from an explicit JSON null), redacted when
// the field is sensitive, else the JSON value.
func liveScalar(d configDrift) string {
	if !d.LivePresent {
		return "(absent)"
	}
	if sensitiveConfigField(d.Field) {
		return "<redacted>"
	}
	return jsonScalar(redactSensitiveValue(d.Field, d.Live))
}
