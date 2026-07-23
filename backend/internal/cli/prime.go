package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type primeSettingsView struct {
	Settings          domain.PrimeSettings `json:"settings"`
	LegacyEnvironment primeLegacyEnv       `json:"legacyEnvironment"`
}

type primeLegacyEnv struct {
	Configured  bool   `json:"configured"`
	ProjectID   string `json:"projectId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type primeSettingsRequest struct {
	Settings domain.PrimeSettings `json:"settings"`
}

func newPrimeCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prime",
		Short: "Manage daemon-global fleet Prime",
	}
	cmd.AddCommand(newPrimeSettingsCommand(ctx))
	cmd.AddCommand(newPrimeEnableCommand(ctx))
	cmd.AddCommand(newPrimeDisableCommand(ctx))
	cmd.AddCommand(newPrimePromptCommand(ctx))
	return cmd
}

func newPrimeSettingsCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Print daemon-global fleet Prime settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := getPrimeSettings(cmd, ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			printPrimeSettings(cmd, view)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output settings as JSON")
	return cmd
}

func newPrimeEnableCommand(ctx *commandContext) *cobra.Command {
	var opts primeSettingsFlags
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable fleet Prime and optionally update its settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := getPrimeSettings(cmd, ctx)
			if err != nil {
				return err
			}
			settings, err := opts.apply(cmd, view.Settings)
			if err != nil {
				return err
			}
			settings.Enabled = true
			updated, err := putPrimeSettings(cmd, ctx, settings)
			if err != nil {
				return err
			}
			printPrimeSettings(cmd, updated)
			return nil
		},
	}
	opts.bind(cmd)
	return cmd
}

func newPrimeDisableCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable fleet Prime and retire any active Prime on the next supervisor tick",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := getPrimeSettings(cmd, ctx)
			if err != nil {
				return err
			}
			settings := view.Settings
			settings.Enabled = false
			updated, err := putPrimeSettings(cmd, ctx, settings)
			if err != nil {
				return err
			}
			printPrimeSettings(cmd, updated)
			return nil
		},
	}
	return cmd
}

func newPrimePromptCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Print the exact assembled fleet Prime system prompt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var res rolePromptResult
			if err := ctx.getJSON(cmd.Context(), "prime/prompt", &res); err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), res.Prompt)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the role prompt as JSON")
	return cmd
}

type primeSettingsFlags struct {
	name         string
	agent        string
	model        string
	effort       string
	rules        string
	rulesFile    string
	wakeInterval string
}

func (f *primeSettingsFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.name, "name", "", "Fleet Prime display name")
	cmd.Flags().StringVar(&f.agent, "agent", "", "Agent harness for Prime")
	cmd.Flags().StringVar(&f.model, "model", "", "Model pin for Prime")
	cmd.Flags().StringVar(&f.effort, "effort", "", "Effort pin for Prime")
	cmd.Flags().StringVar(&f.rules, "rules", "", "Inline standing instructions for Prime")
	cmd.Flags().StringVar(&f.rulesFile, "rules-file", "", "File containing standing instructions for Prime")
	cmd.Flags().StringVar(&f.wakeInterval, "wake-interval", "", "Idle wake interval, e.g. 15m")
}

func (f primeSettingsFlags) apply(cmd *cobra.Command, settings domain.PrimeSettings) (domain.PrimeSettings, error) {
	if cmd.Flags().Changed("name") {
		settings.DisplayName = strings.TrimSpace(f.name)
	}
	if cmd.Flags().Changed("agent") {
		settings.Harness = domain.AgentHarness(strings.TrimSpace(f.agent))
	}
	if cmd.Flags().Changed("model") {
		settings.AgentConfig.Model = strings.TrimSpace(f.model)
	}
	if cmd.Flags().Changed("effort") {
		settings.AgentConfig.Effort = domain.Effort(strings.TrimSpace(f.effort))
	}
	if cmd.Flags().Changed("rules") {
		settings.Rules = strings.TrimSpace(f.rules)
	}
	if cmd.Flags().Changed("rules-file") {
		settings.RulesFile = strings.TrimSpace(f.rulesFile)
	}
	if cmd.Flags().Changed("wake-interval") {
		settings.WakeInterval = strings.TrimSpace(f.wakeInterval)
	}
	return settings, nil
}

func getPrimeSettings(cmd *cobra.Command, ctx *commandContext) (primeSettingsView, error) {
	var view primeSettingsView
	if err := ctx.getJSON(cmd.Context(), "prime/settings", &view); err != nil {
		return primeSettingsView{}, err
	}
	return view, nil
}

func putPrimeSettings(cmd *cobra.Command, ctx *commandContext, settings domain.PrimeSettings) (primeSettingsView, error) {
	var view primeSettingsView
	if err := ctx.putJSON(cmd.Context(), "prime/settings", primeSettingsRequest{Settings: settings}, &view); err != nil {
		return primeSettingsView{}, err
	}
	return view, nil
}

func printPrimeSettings(cmd *cobra.Command, view primeSettingsView) {
	s := view.Settings
	agent := string(s.Harness)
	if agent == "" {
		agent = "-"
	}
	model := s.AgentConfig.Model
	if model == "" {
		model = "-"
	}
	effort := string(s.AgentConfig.Effort)
	if effort == "" {
		effort = "-"
	}
	legacyProject := "-"
	if view.LegacyEnvironment.ProjectID != "" {
		legacyProject = view.LegacyEnvironment.ProjectID
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Prime enabled=%v name=%q agent=%s model=%s effort=%s wakeInterval=%s legacyEnv=%v legacyProject=%s\n",
		s.Enabled, s.DisplayName, agent, model, effort, s.WakeInterval, view.LegacyEnvironment.Configured, legacyProject)
}
