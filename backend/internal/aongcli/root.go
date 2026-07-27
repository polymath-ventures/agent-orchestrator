// Package aongcli implements aong, a porcelain over the public `ao` CLI that
// presents a coherent lifecycle verb set for a web-first deployment.
//
// The one rule that keeps this package small: aong never implements behavior
// `ao` already provides. It shells out to the `ao` executable, and uses
// `systemctl --user` for the service-unit layer — starting units and reporting
// unit state — which `ao` has no commands for at all. It must not import daemon
// or `ao` CLI internals, open the run file, or talk to the daemon HTTP API —
// otherwise it becomes a second lifecycle implementation that can drift from
// the one it is supposed to be a view of.
package aongcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// Execute runs the aong CLI with process stdio.
func Execute() error {
	return executeWithDeps(DefaultDeps(), os.Args[1:])
}

func executeWithDeps(deps Deps, args []string) error {
	deps = deps.withDefaults()
	route, routedArgs, verbose := routeInvocation(args)
	if route == routePassthrough {
		ctx := &commandContext{deps: deps, verbose: verbose}
		return ctx.runAOPassthrough(context.Background(), routedArgs...)
	}
	if verbose {
		deps.Verbose = true
	}
	cmd := NewRootCommand(deps)
	cmd.SetArgs(routedArgs)
	return cmd.Execute()
}

// usageError marks a command-line misuse so the entrypoint can exit 2 for
// misuse versus 1 for runtime failures, matching `ao`'s convention.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// ExitCode maps a CLI error to a process exit code: 2 for usage errors, 1 for
// any other failure, 0 for success.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ue usageError
	if errors.As(err, &ue) {
		return 2
	}
	// Intentionally do not use errors.As here: once a passthroughError is
	// wrapped by an override, it is an override runtime failure and must exit 1.
	if pe, ok := err.(passthroughError); ok { //nolint:errorlint // Top-level passthrough is the only case that preserves child exit codes.
		return pe.ExitCode()
	}
	return 1
}

type silentError interface{ Silent() bool }

// ShouldPrintError reports whether the process entrypoint should print err.
// Passthrough subprocess failures have already written their own stderr, so
// printing Go's "exit status N" would make passthrough stderr non-transparent.
func ShouldPrintError(err error) bool {
	if err == nil {
		return false
	}
	// Intentionally do not use errors.As here: only top-level passthrough
	// failures have already had their stderr handled by the child process.
	silent, ok := err.(silentError) //nolint:errorlint // Wrapped passthrough failures belong to overrides and must print.
	return !ok || !silent.Silent()
}

// Deps holds the side effects aong needs. Tests replace these so every
// behavior is exercisable without a daemon, a systemd user manager, or an
// installed `ao`.
type Deps struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer

	// Executable reports the running aong binary's path; its directory is where
	// the co-installed `ao` is looked for first.
	Executable func() (string, error)
	LookPath   func(file string) (string, error)
	// RunCommand runs a command to completion and returns its combined output.
	RunCommand func(ctx context.Context, name string, args ...string) ([]byte, error)
	// RunStreamingCommand runs a command with directly connected stdio.
	RunStreamingCommand func(ctx context.Context, name string, args []string, in io.Reader, out, errOut io.Writer) error
	Verbose             bool
}

// DefaultDeps returns production dependencies.
func DefaultDeps() Deps {
	return Deps{
		In:                  os.Stdin,
		Out:                 os.Stdout,
		Err:                 os.Stderr,
		Executable:          os.Executable,
		LookPath:            exec.LookPath,
		RunCommand:          runCommand,
		RunStreamingCommand: runStreamingCommand,
	}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return aoprocess.CommandContext(ctx, name, args...).CombinedOutput()
}

func runStreamingCommand(ctx context.Context, name string, args []string, in io.Reader, out, errOut io.Writer) error {
	cmd := aoprocess.CommandContext(ctx, name, args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errOut
	return cmd.Run()
}

func (d Deps) withDefaults() Deps {
	def := DefaultDeps()
	if d.In == nil {
		d.In = def.In
	}
	if d.Out == nil {
		d.Out = def.Out
	}
	if d.Err == nil {
		d.Err = def.Err
	}
	if d.Executable == nil {
		d.Executable = def.Executable
	}
	if d.LookPath == nil {
		d.LookPath = def.LookPath
	}
	if d.RunCommand == nil {
		d.RunCommand = def.RunCommand
	}
	if d.RunStreamingCommand == nil {
		d.RunStreamingCommand = def.RunStreamingCommand
	}
	return d
}

type commandContext struct {
	deps    Deps
	verbose bool
}

type invocationRoute int

const (
	routeCobra invocationRoute = iota
	routePassthrough
)

func routeInvocation(args []string) (invocationRoute, []string, bool) {
	if len(args) == 0 {
		return routeCobra, args, false
	}

	verbose := false
	if args[0] == "--verbose" {
		verbose = true
		args = args[1:]
		if len(args) == 0 {
			return routeCobra, []string{"--help"}, verbose
		}
	}

	switch args[0] {
	case "--help", "-h", "--version", "-v":
		return routeCobra, args, verbose
	}
	if strings.HasPrefix(args[0], "-") {
		return routeCobra, args, verbose
	}

	if args[0] == "help" {
		if len(args) == 1 {
			return routeCobra, args, verbose
		}
		if strings.HasPrefix(args[1], "-") {
			return routeCobra, args, verbose
		}
		if isAONGOverride(args[1]) {
			return routeCobra, args, verbose
		}
		passthrough := append([]string{}, args[1:]...)
		passthrough = append(passthrough, "--help")
		return routePassthrough, passthrough, verbose
	}

	if isAONGOverride(args[0]) {
		return routeCobra, args, verbose
	}
	return routePassthrough, args, verbose
}

var aongOverrideNames = map[string]struct{}{
	"start":     {},
	"status":    {},
	"drain":     {},
	"resume":    {},
	"stop-work": {},
	"stop":      {},
	"shutdown":  {},
	"pause":     {},
	"doctor":    {},
}

func isAONGOverride(name string) bool {
	_, ok := aongOverrideNames[name]
	return ok
}

// NewRootCommand builds a testable root command.
func NewRootCommand(deps Deps) *cobra.Command {
	deps = deps.withDefaults()
	ctx := &commandContext{deps: deps, verbose: deps.Verbose}

	root := &cobra.Command{
		Use:   "aong",
		Short: "Agent Orchestrator fork CLI",
		Long: "aong is this fork's invocation surface over the `ao` CLI. It overrides\n" +
			"the lifecycle verbs whose upstream names are misleading, and passes every\n" +
			"other `ao` command through unchanged.\n\n" +
			"The view, the daemon, the fleet's scheduling state, and the agent sessions\n" +
			"stop independently. Quitting a view never stops work; stopping the daemon\n" +
			"never stops work. `aong shutdown` is the one verb that stops everything.\n\n" +
			"Global flags such as `--verbose` must appear before the verb; flags after\n" +
			"a passthrough verb are forwarded to `ao` unchanged.",
		Version:       versionString(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// Root is runnable so an unrecognised verb reaches this Args validator
		// and is reported as misuse (exit 2). Cobra's own "unknown command"
		// error is an ordinary error, which would exit 1 and misreport a typo
		// as a runtime failure to anything scripting aong.
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(deps.Out)
	root.SetErr(deps.Err)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err}
	})

	root.SetHelpCommand(newHelpCommand(root))
	root.AddCommand(newStartCommand(ctx))
	root.AddCommand(newStatusCommand(ctx))
	root.AddCommand(newDoctorCommand(ctx))
	root.AddCommand(newDrainCommand(ctx))
	root.AddCommand(newPauseCommand(ctx))
	root.AddCommand(newResumeCommand(ctx))
	root.AddCommand(newStopWorkCommand(ctx))
	root.AddCommand(newStopCommand(ctx))
	root.AddCommand(newShutdownCommand(ctx))

	return root
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{errors.New("expected no arguments")}
	}
	return nil
}

// newHelpCommand replaces Cobra's built-in help command. The built-in one
// resolves an unknown topic to the (now runnable) root and silently prints root
// help with exit 0, which hides the typo. Asking for help on a verb that does
// not exist is misuse, and misuse exits 2.
func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(_ *cobra.Command, args []string) error {
			target, remainder, err := root.Find(args)
			if err != nil || target == nil || len(remainder) > 0 || (len(args) > 0 && target == root) {
				return usageError{fmt.Errorf("unknown help topic %q", strings.Join(args, " "))}
			}
			return target.Help()
		},
	}
}
