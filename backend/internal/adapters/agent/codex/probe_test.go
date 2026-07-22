package codex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var codexExecFlagSurface = []string{
	"--no-update", "--add-dir", "--color", "--config",
	"--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust",
	"--disable", "--enable", "--ephemeral", "--help", "--ignore-rules",
	"--ignore-user-config", "--image", "--json", "--local-provider", "--model",
	"--output-last-message", "--output-schema", "--oss", "--profile", "--sandbox",
	"--skip-git-repo-check", "--strict-config", "--version", "--cd",
}

func TestValidateModelProbeUsesSupportedHermeticExecFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	bin := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
expect=""
for arg in "$@"; do
  if [ -n "$expect" ]; then
    case " $expect " in *" $arg "*) expect=""; continue ;; *) exit 2 ;; esac
  fi
  case "$arg" in
    --sandbox) expect="read-only workspace-write danger-full-access" ;;
    --color) expect="always never auto" ;;
    --*) case " $AO_SUPPORTED_FLAGS " in *" $arg "*) ;; *) echo "unexpected $arg" >&2; exit 2 ;; esac ;;
  esac
done
printf '%s\n' "$@" > "$AO_ARGS_FILE"
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_SUPPORTED_FLAGS", strings.Join(codexExecFlagSurface, " "))
	t.Setenv("AO_ARGS_FILE", argsFile)

	got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(context.Background(), "gpt-native")
	if err != nil {
		t.Fatalf("ValidateModel: %v", err)
	}
	if got.Status != ports.ModelValidationReachable {
		t.Fatalf("status = %q, want reachable (%s)", got.Status, got.Message)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(string(data))
	if containsString(args, "--ask-for-approval") {
		t.Fatalf("probe passes TUI-only --ask-for-approval: %#v", args)
	}
	for _, want := range [][]string{{"exec"}, {"--model", "gpt-native"}, {"--sandbox", "read-only"}, {"--skip-git-repo-check"}, {"--ephemeral"}, {"--ignore-user-config"}} {
		if !containsSubsequence(args, want) {
			t.Errorf("probe args %#v missing %#v", args, want)
		}
	}
}

func TestValidateModelClassifiesProviderHTTPVerdicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	tests := []struct {
		name   string
		output string
		want   ports.ModelValidationStatus
	}{
		{name: "400 model rejection", output: `ERROR: {"status":400,"error":{"message":"unsupported model"}}`, want: ports.ModelValidationUnreachable},
		{name: "404 model rejection", output: `ERROR: {"status":404,"error":{"message":"model_not_found"}}`, want: ports.ModelValidationUnreachable},
		{name: "422 model rejection", output: `422 model unavailable`, want: ports.ModelValidationUnreachable},
		{name: "401 auth", output: `ERROR: {"status":401,"error":{"message":"unauthorized"}}`, want: ports.ModelValidationProbeUnavailable},
		{name: "429 capacity", output: `ERROR: {"status":429,"error":{"message":"rate limit"}}`, want: ports.ModelValidationProbeUnavailable},
		{name: "500 provider", output: `ERROR: {"status":500,"error":{"message":"internal"}}`, want: ports.ModelValidationProbeUnavailable},
		{name: "missing status", output: `connection reset by peer`, want: ports.ModelValidationProbeUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := writeFakeScript(t, "#!/bin/sh\nprintf '%s\\n' '"+tc.output+"' >&2\nexit 1\n")
			got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(context.Background(), "gpt-native")
			if err != nil {
				t.Fatalf("ValidateModel: %v", err)
			}
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q (%s)", got.Status, tc.want, got.Message)
			}
		})
	}
}

func TestValidateModelUsageAndSignalFailuresAreProbeUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	for _, script := range []string{
		"#!/bin/sh\necho bad-flag >&2\nexit 2\n",
		"#!/bin/sh\nkill -9 $$\n",
	} {
		got, err := (&Plugin{resolvedBinary: writeFakeScript(t, script)}).ValidateModel(context.Background(), "gpt-native")
		if err != nil {
			t.Fatalf("ValidateModel: %v", err)
		}
		if got.Status != ports.ModelValidationProbeUnavailable {
			t.Fatalf("status = %q, want probe-unavailable", got.Status)
		}
	}
}

func TestValidateModelOwnsIndependentFortyFiveSecondBudget(t *testing.T) {
	if probeTimeout != 45*time.Second {
		t.Fatalf("probe timeout = %s, want 45s", probeTimeout)
	}
}

func TestFormatProbeOutputTruncatesUnicodeOnRuneBoundary(t *testing.T) {
	got := formatProbeOutput([]byte(strings.Repeat("界", 501)))
	if !utf8.ValidString(got) {
		t.Fatalf("formatProbeOutput returned invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("formatProbeOutput did not truncate: %q", got)
	}
	wantRunes := len([]rune(": ")) + 500 + len([]rune("...[truncated]"))
	if utf8.RuneCountInString(got) != wantRunes {
		t.Fatalf("rune count = %d, want %d", utf8.RuneCountInString(got), wantRunes)
	}
}

func writeFakeScript(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func containsString(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}
