// Package tmux implements ports.Runtime using tmux sessions on Darwin/Linux.
package tmux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/ptyexec"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	defaultTimeout    = 5 * time.Second
	defaultChunkBytes = 16 * 1024
	// defaultEnterDelay mirrors conpty's ptyInputEnterDelay: a pause after pasting
	// a non-empty message, before the trailing Enter, so a large multiline paste
	// does not absorb the Enter and leave the prompt unsubmitted (issue #2342).
	defaultEnterDelay = 300 * time.Millisecond
	// defaultReapGrace is how long Destroy waits between SIGTERM and SIGKILL when
	// reaping a pane's leftover background processes, giving them a chance to
	// exit cleanly (release ports) before being forced (issue #2523).
	defaultReapGrace = 5 * time.Second
)

var sessionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var getenv = os.Getenv

// Options configures a tmux Runtime. Every field has a sensible default (see
// New), so the zero value is usable.
type Options struct {
	Binary string // default "tmux" (resolved via exec.LookPath)
	Shell  string // default $SHELL else /bin/sh
	// DataDir is AO's data directory (config.Config.DataDir). It anchors the
	// tmux socket under AO-owned state -- see SocketPath and issue #160. Left
	// empty (tests, embedded use) the Runtime passes no -S and tmux picks its
	// own default socket; production wiring always supplies it, and
	// runtimeselect.New takes it as a required argument so a caller cannot
	// forget.
	DataDir    string
	Timeout    time.Duration // default 5s
	ChunkSize  int           // default 16*1024
	EnterDelay time.Duration // pause after pasting a non-empty message before pressing Enter; default defaultEnterDelay. Conpty already does this (ptyInputEnterDelay); tmux lacked it, so a large multiline paste could absorb the trailing Enter and leave the prompt unsubmitted (issue #2342).
	ReapGrace  time.Duration // grace between SIGTERM and SIGKILL when reaping a pane's leftover background processes on Destroy; default defaultReapGrace.
}

// Runtime runs agent sessions inside tmux sessions, driving them via the tmux
// CLI. It implements ports.Runtime.
type Runtime struct {
	binary string
	shell  string
	// socket is the AO-owned tmux socket every invocation targets via -S.
	// Empty means "let tmux choose" (see Options.DataDir).
	socket string
	// legacySocket is tmux's own default socket path, consulted only as a
	// transitional fallback for sessions that predate the move off /tmp. It is
	// empty whenever socket is, and its whole code path costs nothing once the
	// legacy socket file is gone. See socketFor.
	legacySocket string
	timeout      time.Duration
	chunkSize    int
	enterDelay   time.Duration
	reapGrace    time.Duration
	runner       runner
	reapSessions func(ctx context.Context, pids []int, grace time.Duration)
}

// SocketPath returns the tmux socket AO owns for a given data dir. A runtime
// session handle is app state, so it belongs under the data dir with the rest
// of it rather than on tmux's default /tmp/tmux-$UID/default, where a routine
// operator `/tmp` sweep -- or tmpfs pressure -- silently orphans every live
// session (issue #160). An empty dataDir yields an empty path, meaning "use
// tmux's default socket".
func SocketPath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "run", "tmux", "default")
}

// legacySocketPath mirrors tmux's own default-socket resolution ($TMUX_TMPDIR,
// else $TMPDIR, else /tmp), so the transitional fallback looks exactly where
// the pre-#160 daemon's sessions actually live.
func legacySocketPath() string {
	dir := getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = getenv("TMPDIR")
	}
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, tmuxSocketDirName(os.Geteuid()), "default")
}

// tmuxSocketDirName is tmux's per-user socket directory name, "tmux-<euid>".
func tmuxSocketDirName(uid int) string {
	return "tmux-" + strconv.Itoa(uid)
}

// ensureSocketDir creates the socket's parent directory, which tmux requires to
// exist before it will bind. 0700 matches tmux's own /tmp/tmux-$UID; the
// explicit Chmod tightens a directory that an earlier umask left looser, so the
// permission holds regardless of how the path came to exist.
func ensureSocketDir(socket string) error {
	if socket == "" {
		return nil
	}
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tmux runtime: create socket dir %s: %w", dir, err)
	}
	//nolint:gosec // G302 targets files; this is the socket directory, and 0700 is exactly tmux's own /tmp/tmux-$UID mode.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("tmux runtime: secure socket dir %s: %w", dir, err)
	}
	return nil
}

// Socket reports the tmux socket this Runtime talks to, or "" when it uses
// tmux's default. The daemon logs it at startup so an operator can attach by
// hand (`tmux -S <socket> attach -t <session>`), which is no longer possible
// with a bare `tmux attach` now that AO's sessions live off the default socket.
func (r *Runtime) Socket() string { return r.socket }

var _ ports.Runtime = (*Runtime)(nil)
var _ ports.Attacher = (*Runtime)(nil)

type runner interface {
	Run(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
}

// killSessionsByPID force-terminates every process in each pid's tmux pane
// session. tmux runs each pane in its own session (pane pid == session id), so
// signaling the session reaps the pane's background children — e.g. a dev
// server a worker started with `&` — that `kill-session`'s SIGHUP leaves
// running. It SIGTERMs, waits grace for a clean exit, then
// SIGKILLs survivors. Best-effort: `pkill` is absent on Windows, where tmux is
// never the runtime, so the calls simply no-op there.
func killSessionsByPID(ctx context.Context, pids []int, grace time.Duration) {
	if len(pids) == 0 {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace+5*time.Second)
	defer cancel()

	signalSessions(cleanupCtx, pids, "-TERM")
	if !sessionsHaveProcesses(cleanupCtx, pids) {
		return
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-cleanupCtx.Done():
		return
	case <-timer.C:
	}
	if !sessionsHaveProcesses(cleanupCtx, pids) {
		return
	}
	signalSessions(cleanupCtx, pids, "-KILL")
}

// signalSessions sends a pkill signal flag (e.g. "-TERM") to every process in
// each pane session, matched by session id via `pkill -s`.
func signalSessions(ctx context.Context, pids []int, sig string) {
	for _, pid := range pids {
		_ = exec.CommandContext(ctx, "pkill", sig, "-s", strconv.Itoa(pid)).Run()
	}
}

// sessionsHaveProcesses reports whether any process remains in the pane
// sessions. `pgrep` exit 1 means no matches; other failures are treated as
// survivors so Destroy stays conservative and still attempts SIGKILL.
func sessionsHaveProcesses(ctx context.Context, pids []int) bool {
	for _, pid := range pids {
		err := exec.CommandContext(ctx, "pgrep", "-s", strconv.Itoa(pid)).Run()
		if err == nil || ctx.Err() != nil {
			return true
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return true
		}
	}
	return false
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(append([]string(nil), os.Environ()...), env...)
	return cmd.CombinedOutput()
}

// New builds a tmux Runtime, filling unset Options with defaults: binary "tmux"
// (resolved via exec.LookPath), shell from $SHELL (else /bin/sh), and the
// default timeout and output chunk size.
func New(opts Options) *Runtime {
	binary := opts.Binary
	if binary == "" {
		if path, err := exec.LookPath("tmux"); err == nil {
			binary = path
		} else {
			binary = "tmux"
		}
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	shellPath := opts.Shell
	if shellPath == "" {
		shellPath = getenv("SHELL")
	}
	if shellPath == "" {
		shellPath = "/bin/sh"
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkBytes
	}
	enterDelay := opts.EnterDelay
	if enterDelay <= 0 {
		enterDelay = defaultEnterDelay
	}
	reapGrace := opts.ReapGrace
	if reapGrace <= 0 {
		reapGrace = defaultReapGrace
	}
	socket := SocketPath(opts.DataDir)
	legacySocket := ""
	if socket != "" {
		legacySocket = legacySocketPath()
	}
	return &Runtime{
		binary:       binary,
		shell:        shellPath,
		socket:       socket,
		legacySocket: legacySocket,
		timeout:      timeout,
		chunkSize:    chunkSize,
		enterDelay:   enterDelay,
		reapGrace:    reapGrace,
		runner:       execRunner{},
		reapSessions: killSessionsByPID,
	}
}

// Create starts a new tmux session in the workspace, running the agent's
// launch command with a keep-alive shell, and returns a handle to it.
func (r *Runtime) Create(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error) {
	id, err := tmuxSessionName(cfg.SessionID)
	if err != nil {
		return ports.RuntimeHandle{}, err
	}
	if cfg.WorkspacePath == "" {
		return ports.RuntimeHandle{}, errors.New("tmux runtime: workspace path is required")
	}
	if len(cfg.Argv) == 0 {
		return ports.RuntimeHandle{}, errors.New("tmux runtime: launch command is required")
	}
	if err := validateEnvKeys(cfg.Env); err != nil {
		return ports.RuntimeHandle{}, err
	}

	// New sessions always land on AO's own socket; only pre-existing ones can be
	// on the legacy socket, so Create targets r.socket directly and passes it to
	// its follow-up commands rather than re-resolving per call.
	if err := ensureSocketDir(r.socket); err != nil {
		return ports.RuntimeHandle{}, err
	}

	launchCmd := buildLaunchCommand(cfg)
	args := newSessionArgs(id, cfg.WorkspacePath, r.shell, launchCmd)
	if _, err := r.runOn(ctx, r.socket, args...); err != nil {
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: create session %s: %w", id, err)
	}
	if err := r.verifyPaneWorkingDirectory(ctx, id, cfg.WorkspacePath); err != nil {
		_ = r.Destroy(context.Background(), ports.RuntimeHandle{ID: id})
		return ports.RuntimeHandle{}, err
	}

	// Hide the status bar in the embedded terminal: it clutters the view and
	// was not designed for the in-browser display context.
	if _, err := r.runOn(ctx, r.socket, setStatusOffArgs(id)...); err != nil {
		_ = r.Destroy(context.Background(), ports.RuntimeHandle{ID: id})
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: set status %s: %w", id, err)
	}

	// Enable mouse mode so the embedded terminal's SGR wheel reports scroll the
	// pane (see setMouseOnArgs). Without it, wheel scrolling silently no-ops.
	if _, err := r.runOn(ctx, r.socket, setMouseOnArgs(id)...); err != nil {
		_ = r.Destroy(context.Background(), ports.RuntimeHandle{ID: id})
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: set mouse %s: %w", id, err)
	}

	// Size the shared window to the largest attached client, not the most recent
	// one, so a small secondary viewer (e.g. the phone) can't strip down a larger
	// client's view (see setWindowSizeLargestArgs).
	if _, err := r.runOn(ctx, r.socket, setWindowSizeLargestArgs(id)...); err != nil {
		_ = r.Destroy(context.Background(), ports.RuntimeHandle{ID: id})
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: set window-size %s: %w", id, err)
	}

	handle := ports.RuntimeHandle{ID: id}
	alive, err := r.hasSessionOn(ctx, r.socket, id)
	if err != nil {
		_ = r.Destroy(context.Background(), handle)
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: verify session %s: %w", id, err)
	}
	if !alive {
		_ = r.Destroy(context.Background(), handle)
		return ports.RuntimeHandle{}, fmt.Errorf("tmux runtime: session %s exited before ready", id)
	}
	return handle, nil
}

func (r *Runtime) verifyPaneWorkingDirectory(ctx context.Context, id, want string) error {
	out, err := r.runOn(ctx, r.socket, paneCurrentPathArgs(id)...)
	if err != nil {
		return fmt.Errorf("tmux runtime: verify working directory %s: %w", id, err)
	}
	got := strings.TrimSpace(string(out))
	if sameDirectory(got, want) {
		return nil
	}
	return fmt.Errorf("tmux runtime: session %s started in %q, want %q", id, got, want)
}

// Destroy kills the handle's tmux session and reaps the pane processes it
// leaves behind. `tmux kill-session` only SIGHUPs each pane's foreground
// process, so a worker's backgrounded children (e.g. a dev server started with
// `&`, later reparented to init) survive it and hold their ports indefinitely
// (issue #2523). To catch those, Destroy records each pane's session id before
// teardown and, after kill-session, signals the whole session (see
// killSessionsByPID). An already-gone session is treated as success (idempotent).
func (r *Runtime) Destroy(ctx context.Context, handle ports.RuntimeHandle) error {
	id, err := handleID(handle)
	if err != nil {
		return err
	}
	// Resolve the hosting socket once so the pane listing and the kill target
	// cannot disagree (see socketFor).
	socket := r.socketFor(ctx, id)

	// Capture pane session ids while the session still exists; a missing
	// session lists no panes and reaps nothing. Best-effort: failures here must
	// not block the kill-session below.
	sessionIDs := r.paneSessionIDs(ctx, socket, id)

	out, err := r.runOn(ctx, socket, killSessionArgs(id)...)
	// Reap regardless of the kill-session result: orphaned children outlive the
	// session, so they must be cleaned up even when the session was already
	// gone (a benign double-kill).
	r.reapSessions(ctx, sessionIDs, r.reapGrace)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && killSessionMissingOutput(string(out)) {
			return nil
		}
		return fmt.Errorf("tmux runtime: destroy session %s: %w", id, err)
	}
	return nil
}

// paneSessionIDs lists the pid of every pane in the session. tmux launches each
// pane in its own session (setsid), so a pane's pid is also its session id —
// the handle killSessionsByPID uses to reap the pane's descendants. Best-effort:
// any error (including a missing session) or unparseable line yields no ids,
// and pids <= 1 are skipped so we never signal init or the "current session".
func (r *Runtime) paneSessionIDs(ctx context.Context, socket, id string) []int {
	out, err := r.runOn(ctx, socket, listPanePIDsArgs(id)...)
	if err != nil {
		return nil
	}
	var ids []int
	for _, line := range strings.Split(string(out), "\n") {
		pid, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || pid <= 1 {
			continue
		}
		ids = append(ids, pid)
	}
	return ids
}

// IsAlive reports whether the handle's session still exists via `tmux
// has-session`. Exit 0 means alive. A non-zero exit with output indicating the
// session or server is missing is a definitive false, nil. Any other non-zero
// exit is a probe error (not proof of death) so callers (the reaper feeding
// the LCM) treat it as a failed probe and never kill a session on a transient
// error.
func (r *Runtime) IsAlive(ctx context.Context, handle ports.RuntimeHandle) (bool, error) {
	id, err := handleID(handle)
	if err != nil {
		return false, err
	}
	return r.hasSessionOn(ctx, r.socketFor(ctx, id), id)
}

// IsRunningCommand reports whether the pane still has a launched child process.
// AO starts panes through a shell wrapper that execs a keep-alive shell after
// the agent exits, so tmux's session liveness alone cannot prove the agent is
// still running.
func (r *Runtime) IsRunningCommand(ctx context.Context, handle ports.RuntimeHandle, command string) (bool, error) {
	id, err := handleID(handle)
	if err != nil {
		return false, err
	}
	out, err := r.runFor(ctx, id, paneProcessArgs(id)...)
	if err != nil {
		return false, fmt.Errorf("tmux runtime: inspect pane process %s: %w", id, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return false, fmt.Errorf("tmux runtime: invalid pane pid %q", strings.TrimSpace(string(out)))
	}
	args := []string{"-P", strconv.Itoa(pid)}
	if strings.TrimSpace(command) != "" {
		args = append(args, "-f", regexp.QuoteMeta(command))
	}
	childOut, err := r.runner.Run(ctx, nil, "pgrep", args...)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(string(childOut)) == "" {
			return false, nil
		}
		return false, fmt.Errorf("tmux runtime: inspect child process %d: %w", pid, err)
	}
	return strings.TrimSpace(string(childOut)) != "", nil
}

// SendMessage sends literal text to the session (chunked via send-keys -l) then
// presses Enter to submit. An empty message presses Enter alone (the nudge
// contract on ports.AgentMessenger).
//
// ponytail: send-keys -l chunked is simpler than load-buffer/paste-buffer; the
// ceiling is very large messages may be slower, but chunk size defaults to 16 KB
// which is ample for agent prompts.
func (r *Runtime) SendMessage(ctx context.Context, handle ports.RuntimeHandle, message string) error {
	id, err := handleID(handle)
	if err != nil {
		return err
	}
	// Resolve the hosting socket once: every chunk and the trailing Enter must
	// land in the same pane, and re-resolving per chunk would also re-probe.
	socket := r.socketFor(ctx, id)
	enterCtx := ctx
	if message != "" {
		for _, chunk := range chunks(message, r.chunkSize) {
			if _, err := r.runOn(ctx, socket, sendKeysLiteralArgs(id, chunk)...); err != nil {
				return fmt.Errorf("tmux runtime: send message %s: %w", id, err)
			}
		}
		// Give the target TUI a moment to accept the pasted text before the
		// trailing Enter, mirroring conpty's ptyInputEnterDelay. Without it a
		// large multiline paste can absorb the Enter and leave the prompt
		// unsubmitted (issue #2342). Empty-message nudges skip this — there is
		// no paste ahead of a catch-up Enter.
		//
		// From here on the chunks are already in the pane, so the pause and
		// the Enter are detached from the caller's cancellation (bounded by
		// their own timeout instead): abandoning mid-pause would strand an
		// unsubmitted draft that a retried send would then double-paste.
		var cancel context.CancelFunc
		enterCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), r.enterDelay+5*time.Second)
		defer cancel()
		if r.enterDelay > 0 {
			select {
			case <-enterCtx.Done():
				return enterCtx.Err()
			case <-time.After(r.enterDelay):
			}
		}
	}
	if _, err := r.runOn(enterCtx, socket, sendEnterArgs(id)...); err != nil {
		return fmt.Errorf("tmux runtime: send enter %s: %w", id, err)
	}
	return nil
}

// Interrupt sends Ctrl-C to the foreground process without destroying the tmux
// session, keeping the terminal available for inspection and reuse.
func (r *Runtime) Interrupt(ctx context.Context, handle ports.RuntimeHandle) error {
	id, err := handleID(handle)
	if err != nil {
		return err
	}
	if _, err := r.runFor(ctx, id, sendInterruptArgs(id)...); err != nil {
		return fmt.Errorf("tmux runtime: interrupt session %s: %w", id, err)
	}
	return nil
}

// GetOutput returns the last `lines` lines of the session pane's captured
// output.
func (r *Runtime) GetOutput(ctx context.Context, handle ports.RuntimeHandle, lines int) (string, error) {
	id, err := handleID(handle)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		return "", errors.New("tmux runtime: lines must be positive")
	}
	out, err := r.runFor(ctx, id, capturePaneArgs(id, lines)...)
	if err != nil {
		return "", fmt.Errorf("tmux runtime: capture output %s: %w", id, err)
	}
	return tailLines(trimTrailingBlankLines(string(out)), lines), nil
}

// Attach opens a fresh attach Stream by spawning `tmux attach-session` on a
// local PTY, sized rows x cols from birth when known. ctx cancellation closes
// the PTY.
func (r *Runtime) Attach(ctx context.Context, handle ports.RuntimeHandle, rows, cols uint16) (ports.Stream, error) {
	argv, err := r.attachCommand(ctx, handle)
	if err != nil {
		return nil, err
	}
	return ptyexec.Spawn(ctx, argv, attachEnv(os.Environ()), rows, cols)
}

// attachCommand returns the argv to attach a terminal to the session.
// tmux needs no per-session env block.
//
// -u forces tmux's client-side CLIENT_UTF8 flag on. Without it, tmux infers
// UTF-8 capability from LC_ALL/LC_CTYPE/LANG in the attaching process's env
// (see tmux's main()); AO's daemon is typically started without an
// interactive shell's locale, so that inference silently fails. A non-UTF8
// client makes tmux's tty_check_codeset (tty.c) replace any character it
// can't map through the legacy ACS table with underscores matching the
// glyph's display width. Box-drawing glyphs are in that ACS table so they
// still looked fine; agent CLI status icons outside it (e.g. Claude Code's
// spinner "✻" U+273B, its "⎿" U+23BF continuation marker) were silently
// rewritten to "_", which is the underscore corruption reported in #2484.
// Confirmed byte-for-byte: attaching with a stripped, locale-less env
// reproduces "_ _ _" for those glyphs; adding -u fixes it, with no observable
// difference for the still-correct box-drawing case. AO already treats the
// PTY byte stream as UTF-8 end to end, so forcing the flag is always
// correct here regardless of the daemon's own environment.
func (r *Runtime) attachCommand(ctx context.Context, handle ports.RuntimeHandle) ([]string, error) {
	id, err := handleID(handle)
	if err != nil {
		return nil, err
	}
	return r.tmuxArgv(r.socketFor(ctx, id), "-u", "attach-session", "-t", id), nil
}

func attachEnv(base []string) []string {
	env := append([]string(nil), base...)
	for i, kv := range env {
		if strings.HasPrefix(kv, "TERM=") {
			env[i] = "TERM=xterm-256color"
			return env
		}
	}
	return append(env, "TERM=xterm-256color")
}

// tmuxArgv builds the full argv for a tmux invocation on socket. It is the one
// place the socket flag is applied, so no command builder in commands.go and no
// future call site has to remember it -- and none can silently fall back to
// tmux's default /tmp socket (issue #160).
func (r *Runtime) tmuxArgv(socket string, args ...string) []string {
	argv := make([]string, 0, len(args)+3)
	argv = append(argv, r.binary)
	if socket != "" {
		argv = append(argv, "-S", socket)
	}
	return append(argv, args...)
}

// runOn wraps runner.Run with a per-call timeout context, targeting an explicit
// socket. Callers that act on an existing session should use runFor instead so
// the transitional legacy socket is honored.
func (r *Runtime) runOn(ctx context.Context, socket string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	argv := r.tmuxArgv(socket, args...)
	out, err := r.runner.Run(cmdCtx, nil, argv[0], argv[1:]...)
	if cmdCtx.Err() != nil {
		return out, cmdCtx.Err()
	}
	if err != nil {
		return out, commandError{err: err, output: strings.TrimSpace(string(out))}
	}
	return out, nil
}

// runFor runs a session-targeted tmux command on whichever socket actually
// hosts the session.
func (r *Runtime) runFor(ctx context.Context, id string, args ...string) ([]byte, error) {
	return r.runOn(ctx, r.socketFor(ctx, id), args...)
}

// socketFor resolves the socket hosting session id, preferring AO's own and
// falling back to tmux's default only for sessions that predate the move off
// /tmp (issue #160).
//
// Changing the socket path does not migrate a tmux server already running on
// the old one: without this fallback, upgrading under a live fleet would make
// every running session look dead to IsAlive, and the reaper would act on
// them. The fallback is self-retiring -- once the legacy socket file is gone
// (its server drained or /tmp swept) the stat below short-circuits and no
// extra probe is ever issued, which is also the state in which this function
// and legacySocketPath can be deleted.
func (r *Runtime) socketFor(ctx context.Context, id string) string {
	if r.socket == "" || r.legacySocket == "" {
		return r.socket
	}
	if _, err := os.Stat(r.legacySocket); err != nil {
		return r.socket
	}
	alive, err := r.hasSessionOn(ctx, r.socket, id)
	if err != nil {
		// A transient probe failure is not evidence the session lives
		// elsewhere. Falling back here could send a kill-session to a
		// same-named session on the legacy socket, so stay on the primary and
		// let the caller surface the error.
		return r.socket
	}
	if alive {
		return r.socket
	}
	if legacyAlive, legacyErr := r.hasSessionOn(ctx, r.legacySocket, id); legacyErr == nil && legacyAlive {
		return r.legacySocket
	}
	return r.socket
}

// hasSessionOn probes one socket for a session. Exit 0 means alive; a non-zero
// exit whose output says the session or server is missing is a definitive
// false. Any other failure is a probe error, never proof of death -- callers
// (IsAlive feeding the reaper) must not kill a session on a transient error.
func (r *Runtime) hasSessionOn(ctx context.Context, socket, id string) (bool, error) {
	out, err := r.runOn(ctx, socket, hasSessionArgs(id)...)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && sessionMissingOutput(string(out)) {
			return false, nil
		}
		return false, fmt.Errorf("tmux runtime: probe session %s: %w", id, err)
	}
	return true, nil
}

// -- session name helpers --

func tmuxSessionName(id domain.SessionID) (string, error) {
	raw := string(id)
	if raw == "" {
		return "", errors.New("tmux runtime: session id is required")
	}
	return SessionName(raw), nil
}

// SessionName returns the tmux session name the runtime registers for a given
// session id, applying the same sanitisation Create does. Callers that print an
// attach hint must use this rather than the raw id.
func SessionName(id string) string {
	if sessionIDPattern.MatchString(id) && len(id) <= 48 {
		return id
	}
	return sanitizedSessionName(id)
}

func sanitizedSessionName(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "session"
	}
	if len(base) > 32 {
		base = strings.TrimRight(base[:32], "-")
	}
	sum := sha256.Sum256([]byte(raw))
	return base + "-" + hex.EncodeToString(sum[:4])
}

func handleID(handle ports.RuntimeHandle) (string, error) {
	id := handle.ID
	if id == "" {
		return "", errors.New("tmux runtime: session id is required")
	}
	if !sessionIDPattern.MatchString(id) {
		return "", fmt.Errorf("tmux runtime: invalid handle id %q", id)
	}
	return id, nil
}

// -- output detection helpers --

// sessionMissingOutput reports whether a non-zero `tmux has-session` or
// `tmux kill-session` exit is definitively "session does not exist" rather
// than a transient probe failure.
//
// Both callers pass an explicit exact target (`-t =<id>`), so tmux's generic
// cmd-find "no current target" message here means the named session did not
// resolve — i.e. it is gone — not that a fallback current target was needed.
func sessionMissingOutput(out string) bool {
	s := strings.ToLower(out)
	return strings.Contains(s, "can't find session") ||
		strings.Contains(s, "no server running") ||
		strings.Contains(s, "no current target") ||
		strings.Contains(s, "error connecting") ||
		strings.Contains(s, "session not found")
}

// killSessionMissingOutput reports whether a non-zero `tmux kill-session`
// failed because the session was already gone.
func killSessionMissingOutput(out string) bool {
	return sessionMissingOutput(out)
}

// -- text helpers --

func chunks(s string, maxBytes int) []string {
	if s == "" {
		return []string{""}
	}
	if maxBytes <= 0 || len(s) <= maxBytes {
		return []string{s}
	}
	parts := []string{}
	for s != "" {
		if len(s) <= maxBytes {
			parts = append(parts, s)
			break
		}
		end := maxBytes
		for end > 0 && !utf8.ValidString(s[:end]) {
			end--
		}
		if end == 0 {
			_, size := utf8.DecodeRuneInString(s)
			end = size
		}
		parts = append(parts, s[:end])
		s = s[end:]
	}
	return parts
}

func tailLines(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "")
}

func trimTrailingBlankLines(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && strings.TrimRight(lines[len(lines)-1], "\r\n") == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "")
}

// -- env / quoting helpers --

func validateEnvKeys(env map[string]string) error {
	for key := range env {
		if !validEnvKey(key) {
			return fmt.Errorf("tmux runtime: invalid env key %q", key)
		}
	}
	return nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildLaunchCommand builds the shell command string passed to `sh -c`. It
// exports env vars, then runs argv, then execs a keep-alive interactive shell
// so the tmux session survives the agent exiting.
//
// PATH from cfg.Env is exported last, after all other keys, so an explicit
// override takes effect.
func buildLaunchCommand(cfg ports.RuntimeConfig) string {
	path := cfg.Env["PATH"]
	if path == "" {
		path = getenv("PATH")
	}

	var b strings.Builder
	b.WriteString("cd ")
	b.WriteString(shellQuote(cfg.WorkspacePath))
	b.WriteString(" || exit; ")
	for _, key := range sortedKeys(cfg.Env) {
		if key == "PATH" {
			continue
		}
		b.WriteString("export ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(shellQuote(cfg.Env[key]))
		b.WriteString("; ")
	}
	if path != "" {
		b.WriteString("export PATH=")
		b.WriteString(shellQuote(path))
		b.WriteString("; ")
	}
	// Quote each argv word so spaces inside a word are preserved.
	parts := make([]string, len(cfg.Argv))
	for i, a := range cfg.Argv {
		parts[i] = shellQuote(a)
	}
	b.WriteString(strings.Join(parts, " "))
	// Keep the tmux session alive after the agent exits so the operator can
	// inspect the terminal. The shell variable expansion picks up $SHELL from
	// the process env if set, otherwise falls back to /bin/sh.
	b.WriteString(`; exec "${SHELL:-/bin/sh}" -i`)
	return b.String()
}

func sameDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	if errA == nil {
		a = absA
	}
	absB, errB := filepath.Abs(b)
	if errB == nil {
		b = absB
	}
	if realA, err := filepath.EvalSymlinks(a); err == nil {
		a = realA
	}
	if realB, err := filepath.EvalSymlinks(b); err == nil {
		b = realB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// -- error type --

type commandError struct {
	err    error
	output string
}

func (e commandError) Error() string {
	if e.output == "" {
		return e.err.Error()
	}
	return e.err.Error() + ": " + e.output
}

func (e commandError) Unwrap() error { return e.err }
