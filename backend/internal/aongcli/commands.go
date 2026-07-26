package aongcli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// resumeHint is the one sentence every gating verb owes the operator: how to
// undo it. `drain` and `stop-work` both leave the fleet persistently gated, and
// a gate with no named way back is how a fleet ends up paused across a restart
// with nobody realising why nothing is being picked up.
const resumeHint = "Fleet stays gated until `aong resume`."

func newStartCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the local AO services",
		Long: "Start the local AO services.\n\n" +
			"On a systemd deployment this starts the AO user units that are installed,\n" +
			"in dependency order. On a host with no AO service units there is nothing\n" +
			"for aong to start, and it says so rather than silently doing nothing.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.runStart(cmd.Context(), cmd)
		},
	}
}

func (c *commandContext) runStart(ctx context.Context, cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	env, err := c.detectEnvironment(ctx)
	if err != nil {
		return err
	}
	if env.Kind != envSystemd {
		return fmt.Errorf("no AO service units found on this host (environment: %s); "+
			"start the daemon with `ao daemon`, or open the desktop app with `ao start`", env.Kind)
	}
	for _, unit := range env.LoadedUnits {
		output, err := c.deps.RunCommand(ctx, env.systemctl, "--user", "start", unit)
		if err != nil {
			return fmt.Errorf("systemctl --user start %s: %w%s", unit, err, indentedOutput(output))
		}
		if _, err := fmt.Fprintf(out, "started %s\n", unit); err != nil {
			return err
		}
	}
	return nil
}

func newStatusCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon, service, and fleet state",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.runStatus(cmd.Context(), cmd)
		},
	}
}

func (c *commandContext) runStatus(ctx context.Context, cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	// Daemon state and fleet pause state already come from `ao status`; aong
	// adds the service-unit layer, which `ao status` does not report at all.
	if err := c.echoAO(ctx, out, "status"); err != nil {
		return err
	}
	env, err := c.detectEnvironment(ctx)
	if err != nil {
		// status is a read-only report and must still describe the daemon it
		// just queried, so a systemd probe failure is reported in place of the
		// environment rather than swallowed or turned into a failed command.
		_, printErr := fmt.Fprintf(out, "environment: unknown (%s)\n", err)
		return printErr
	}
	if _, err := fmt.Fprintf(out, "environment: %s\n", env.Kind); err != nil {
		return err
	}
	if env.Kind != envSystemd {
		return nil
	}
	if _, err := fmt.Fprintln(out, "services:"); err != nil {
		return err
	}
	for _, unit := range env.LoadedUnits {
		active, err := c.unitProperty(ctx, env.systemctl, unit, "ActiveState")
		if err != nil {
			active = "unknown"
		}
		if _, err := fmt.Fprintf(out, "  %s: %s\n", unit, active); err != nil {
			return err
		}
	}
	return nil
}

func newDrainCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "drain",
		Short: "Gate new work and let live workers finish, terminating each at idle",
		Long: "Gate new work fleet-wide. Tracker intake and new spawns stop; workers that\n" +
			"are already running finish their current work and are terminated as they\n" +
			"reach idle. Nothing is interrupted mid-flight.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.runGating(cmd.Context(), cmd, []string{"pause", "--all"})
		},
	}
}

func newStopWorkCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "stop-work",
		Short: "Terminate all live work now, including orchestrators and Prime",
		Long: "Terminate every live session across the fleet immediately — workers,\n" +
			"orchestrators, and the fleet Prime — instead of waiting for them to drain.\n" +
			"The daemon keeps running; use `aong shutdown` to stop it too.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.runGating(cmd.Context(), cmd, []string{"pause", "--all", "--hard"})
		},
	}
}

func (c *commandContext) runGating(ctx context.Context, cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	if err := c.echoAO(ctx, out, args...); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, resumeHint)
	return err
}

func newResumeCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Restore fleet-wide intake and spawns",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.echoAO(cmd.Context(), cmd.OutOrStdout(), "resume", "--all")
		},
	}
}

func newStopCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon; agent sessions keep running",
		Long: "Stop the AO daemon.\n\n" +
			"Agent sessions are not the daemon's children and are not stopped. They keep\n" +
			"running unsupervised until the daemon comes back. Use `aong shutdown` to\n" +
			"stop the work and the daemon together.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if err := ctx.echoAO(cmd.Context(), out, "stop"); err != nil {
				return err
			}
			_, err := fmt.Fprintln(out, "Agent sessions keep running and are now unsupervised. Use `aong shutdown` to stop work too.")
			return err
		},
	}
}

func newShutdownCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "shutdown",
		Short: "Stop all work, then stop the daemon",
		Long: "Stop everything: terminate all live sessions, then stop the daemon.\n\n" +
			"Work is stopped first. If that fails the daemon is left running, because\n" +
			"live agents with no supervisor is worse than a shutdown you can retry.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.runShutdown(cmd.Context(), cmd)
		},
	}
}

// runShutdown stops work, then the daemon.
//
// The only interesting question is when it is safe NOT to stop work first, and
// `ao`'s daemon state can answer exactly one case honestly: "stopped" means
// there is no run file, so there is nothing to gate. Every other label is
// ambiguous — "stale" covers both a run file pointing at a dead process and a
// live process that failed the ownership probe, and "unhealthy"/"not_ready" are
// what a transient probe failure produces against a perfectly live daemon — so
// none of them may license skipping the gate.
//
// A failed stop-work therefore always aborts. It is tempting to make an
// exception for the ambiguous states so a host whose daemon is already gone can
// still be reconciled, but every such exception ends in the same place: `ao
// stop` deletes a stale run file and reports success, so shutdown would exit 0
// while a live daemon it could not reach kept running. Refusing, and naming the
// verb that does reconcile a stale run file, is the honest answer — `aong stop`
// is right there, and it does not claim to have stopped any work.
func (c *commandContext) runShutdown(ctx context.Context, cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	absent, err := c.daemonAbsent(ctx)
	if err != nil {
		return err
	}
	if absent {
		if _, err := fmt.Fprintln(out, "No daemon to gate; skipping stop-work."); err != nil {
			return err
		}
	} else if err := c.echoAO(ctx, out, "pause", "--all", "--hard"); err != nil {
		return fmt.Errorf("%w\n\nWork was not stopped, so the daemon is still running. "+
			"If the daemon is already gone, `aong stop` reconciles a stale run file", err)
	}
	return c.echoAO(ctx, out, "stop")
}

// daemonAbsent reports whether `ao status` proves there is nothing to gate.
// Only "stopped" proves it; see runShutdown for why no other state may.
func (c *commandContext) daemonAbsent(ctx context.Context) (bool, error) {
	out, err := c.runAO(ctx, "status", "--json")
	if err != nil {
		return false, err
	}
	var status struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, fmt.Errorf("parse `ao status --json`: %w", err)
	}
	return status.State == "stopped", nil
}
