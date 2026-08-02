package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const supervisedExitReportTimeout = 5 * time.Second

const supervisedErrorTailBytes = 4096
const supervisedLaunchErrorWindow = 10 * time.Second
const supervisedPaneCaptureTimeout = time.Second

func newAgentProcessCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{
		Use:    "agent-process",
		Short:  "Run an AO-managed agent process (internal)",
		Hidden: true,
	}
	root.AddCommand(newAgentProcessSuperviseCommand(ctx))
	return root
}

func newAgentProcessSuperviseCommand(ctx *commandContext) *cobra.Command {
	var sessionID string
	var launchID string
	cmd := &cobra.Command{
		Use:    "supervise --session <id> --launch <id> -- <command> [args...]",
		Short:  "Supervise one managed agent process (internal)",
		Hidden: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{fmt.Errorf("agent command is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID = strings.TrimSpace(sessionID)
			launchID = strings.TrimSpace(launchID)
			if !sessionIDPattern.MatchString(sessionID) {
				return usageError{fmt.Errorf("invalid session id")}
			}
			if !sessionIDPattern.MatchString(launchID) {
				return usageError{fmt.Errorf("invalid launch id")}
			}
			ctx.runSupervisedProcess(cmd.Context(), sessionID, launchID, args)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "AO session id")
	cmd.Flags().StringVar(&launchID, "launch", "", "AO process launch id")
	return cmd
}

func (c *commandContext) runSupervisedProcess(ctx context.Context, sessionID, launchID string, argv []string) {
	child := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv is constructed by the selected agent adapter.
	child.Stdin = c.deps.In
	child.Stdout = c.deps.Out
	// Preserve the terminal file descriptor itself. Wrapping stderr in an
	// io.Writer makes os/exec insert a pipe: descendants can inherit its write
	// end and keep Wait blocked after the managed process has already exited.
	child.Stderr = c.deps.Err

	paneBoundary := c.captureSupervisedPaneBoundary()
	startedAt := c.deps.Now()
	if err := child.Start(); err != nil {
		_, _ = fmt.Fprintf(c.deps.Err, "ao: start managed agent: %v\n", err)
		c.reportSupervisedExit(sessionID, launchID, err.Error())
		return
	}

	// The child shares the terminal foreground process group and therefore
	// receives Ctrl-C directly. Consume the supervisor's copy so it remains
	// alive long enough to reap the child and publish the exit observation.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	waitErr := child.Wait()
	signal.Stop(interrupts)

	errorDetail := ""
	if waitErr != nil && c.deps.Now().Sub(startedAt) <= supervisedLaunchErrorWindow {
		errorDetail = c.captureSupervisedLaunchError(waitErr, paneBoundary)
	}
	c.reportSupervisedExit(sessionID, launchID, errorDetail)
}

type supervisedPaneBoundary struct {
	paneID string
	before string
}

func (c *commandContext) captureSupervisedPaneBoundary() supervisedPaneBoundary {
	paneID := strings.TrimSpace(os.Getenv("TMUX_PANE"))
	if paneID == "" {
		return supervisedPaneBoundary{}
	}
	before, err := c.captureSupervisedPane(paneID)
	if err != nil {
		return supervisedPaneBoundary{}
	}
	return supervisedPaneBoundary{paneID: paneID, before: before}
}

func (c *commandContext) captureSupervisedLaunchError(waitErr error, boundary supervisedPaneBoundary) string {
	if boundary.paneID == "" {
		return waitErr.Error()
	}
	after, err := c.captureSupervisedPane(boundary.paneID)
	if err != nil {
		return waitErr.Error()
	}
	detail := paneOutputAfterBoundary(boundary.before, after)
	if detail == "" {
		return waitErr.Error()
	}
	return boundedUTF8Tail(detail, supervisedErrorTailBytes)
}

func (c *commandContext) captureSupervisedPane(paneID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), supervisedPaneCaptureTimeout)
	defer cancel()
	output, err := c.deps.CommandOutput(ctx, "tmux", "capture-pane", "-p", "-t", paneID, "-S", "-100")
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func paneOutputAfterBoundary(before, after string) string {
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if before == "" {
		return after
	}
	boundary := strings.LastIndex(after, before)
	if boundary < 0 {
		return ""
	}
	return strings.TrimSpace(after[boundary+len(before):])
}

func boundedUTF8Tail(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func (c *commandContext) reportSupervisedExit(sessionID, launchID, errorDetail string) {
	ctx, cancel := context.WithTimeout(context.Background(), supervisedExitReportTimeout)
	defer cancel()
	path := "sessions/" + sessionID + "/activity"
	req := setActivityAPIRequest{State: "exited", Event: "process-exited", LaunchID: launchID, Error: errorDetail}
	if err := c.postJSON(ctx, path, req, nil); err != nil {
		// Reconciliation will recover this event from process absence. Keep the
		// delivery failure visible without preventing the terminal's shell.
		c.reportHookFailure("agent-process", "process-exited", sessionID, err)
	}
}
