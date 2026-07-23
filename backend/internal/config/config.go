// Package config loads the daemon's runtime configuration. The HTTP daemon is
// a loopback-only sidecar: it binds 127.0.0.1, takes no public traffic, and
// reads everything it needs from the environment with sane defaults so it can
// boot with zero configuration in development.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// LoopbackHost is the only host the daemon ever binds. There is deliberately
	// no AO_HOST env var: the daemon has no auth/CORS/TLS and a stray
	// AO_HOST=0.0.0.0 would turn it into a public no-auth service. If a
	// non-default loopback (e.g. ::1, 127.0.0.2) is ever needed, add it back with
	// an IsLoopback() validator — not a raw env read.
	LoopbackHost = "127.0.0.1"
	// DefaultPort is the single port for REST, terminal mux, health, and control.
	DefaultPort = 3001
	// DefaultRequestTimeout bounds a single REST request. Long-lived terminal mux
	// connections are mounted outside this timeout.
	DefaultRequestTimeout = 60 * time.Second
	// DefaultShutdownTimeout is the hard cap on graceful shutdown. After this
	// the process exits even if connections are still draining.
	DefaultShutdownTimeout = 10 * time.Second
	// DefaultAgentHealthInterval is how often configured harnesses are checked
	// for local installation and authentication readiness.
	DefaultAgentHealthInterval = 5 * time.Minute
	// DefaultModelRevalidationInterval is deliberately daily because model
	// validation may issue real provider requests.
	DefaultModelRevalidationInterval = 24 * time.Hour
	// DefaultAgent is the compatibility value used when AO_AGENT is unset. The
	// daemon validates it at startup, but worker/orchestrator spawns resolve from
	// explicit requests or project role config instead of falling back to it.
	DefaultAgent = "claude-code"
	// DefaultTelemetryPostHogHost is the default PostHog ingestion host when
	// remote telemetry is enabled and AO_TELEMETRY_POSTHOG_HOST is unset.
	DefaultTelemetryPostHogHost = "https://us.i.posthog.com"
	// DefaultMetricsInterval is how often the daemon samples metrics.
	DefaultMetricsInterval = 30 * time.Second
	// DefaultMetricsLowQuotaPercent fires low_quota when a known quota window
	// reports remaining usage at or below this percent. Zero disables the alert.
	DefaultMetricsLowQuotaPercent = 10
)

// TelemetryRemote selects the remote telemetry exporter.
type TelemetryRemote string

const (
	// TelemetryRemoteOff disables remote telemetry export.
	TelemetryRemoteOff TelemetryRemote = "off"
	// TelemetryRemotePostHog exports allowlisted events to PostHog.
	TelemetryRemotePostHog TelemetryRemote = "posthog"
)

// TelemetryConfig controls local and remote telemetry behavior.
type TelemetryConfig struct {
	Events      bool
	Metrics     bool
	Remote      TelemetryRemote
	PostHogKey  string
	PostHogHost string
}

// MetricsConfig controls the daemon metrics observer. Interval <=0 disables
// the observer and /api/v1/metrics reports not implemented.
type MetricsConfig struct {
	Interval        time.Duration
	LowQuotaPercent float64
}

// DefaultAllowedOrigins are the browser origins the daemon's CORS boundary
// trusts, beyond loopback-served content (which the middleware always trusts —
// local pages can reach the no-auth daemon directly anyway). The daemon has no
// auth, so every entry must be an origin web content cannot present:
// app://renderer is the packaged Electron renderer, served from a custom
// scheme only the desktop app registers — no website can bear it. The opaque
// "null" origin (file:// pages, sandboxed iframes on any website) must never
// be added.
var DefaultAllowedOrigins = []string{
	"app://renderer",
}

// Config is the fully-resolved daemon configuration. It is immutable once
// built by Load.
type Config struct {
	// Host is the bind address. Always loopback — see LoopbackHost.
	Host string
	// Port is the TCP port to bind. The daemon fails fast if it is taken.
	Port int
	// RequestTimeout bounds REST request handling.
	RequestTimeout time.Duration
	// ShutdownTimeout is the hard graceful-shutdown deadline.
	ShutdownTimeout time.Duration
	// RunFilePath is where the PID + port handshake file (running.json) is
	// written so the Electron supervisor can discover and reap the daemon.
	RunFilePath string
	// DataDir is the directory holding durable SQLite state: DB and WAL files.
	// It is created on first use by the storage layer.
	DataDir string
	// Agent is the compatibility agent adapter id selected by AO_AGENT;
	// startSession fails fast if no adapter with this id is registered.
	Agent string
	// AgentHealthInterval controls scheduled install/auth health checks. Zero
	// disables the scheduled monitor without affecting explicit health reads.
	AgentHealthInterval time.Duration
	// ModelRevalidationInterval controls scheduled model-pin reachability
	// checks. Zero disables scheduled revalidation.
	ModelRevalidationInterval time.Duration
	// PrimeProjectID is a legacy migration hint for the old env-gated Prime.
	PrimeProjectID string
	// PrimeDisplayName is a legacy migration hint for the old env-gated Prime.
	PrimeDisplayName string
	// AllowedOrigins are the browser origins granted CORS read access (see
	// DefaultAllowedOrigins). Overridden by AO_ALLOWED_ORIGINS.
	AllowedOrigins []string
	// MobileAdvertisedHost, when set (AO_MOBILE_ADVERTISED_HOST), is the host —
	// IP or DNS name — advertised to pairing phones in the Connect Mobile
	// status/QR instead of the autopicked interface address. It does not change
	// what the LAN listener binds; it only changes what is advertised.
	MobileAdvertisedHost string
	// Telemetry controls local/remote telemetry sinks.
	Telemetry TelemetryConfig
	// Metrics controls the usage and quota metrics observer.
	Metrics MetricsConfig
}

// Addr returns the host:port the HTTP server binds. It uses net.JoinHostPort so
// the result is correct for IPv6 literals as well as IPv4 / hostnames.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Load resolves configuration from the environment, applying defaults. It
// returns an error only for values that are present but malformed (e.g. a
// non-numeric AO_PORT); missing values fall back to defaults.
//
// Recognised variables:
//
//	AO_PORT              bind port           (default 3001)
//	AO_REQUEST_TIMEOUT   per-request timeout (Go duration > 0, default 60s)
//	AO_SHUTDOWN_TIMEOUT  shutdown deadline   (Go duration > 0, default 10s)
//	AO_RUN_FILE          running.json path   (default ~/.ao/running.json)
//	AO_DATA_DIR          durable state dir   (default ~/.ao/data)
//	AO_AGENT             compatibility agent id (default claude-code)
//	AO_AGENT_HEALTH_INTERVAL agent install/auth probe period (Go duration >= 0, 0 disables, default 5m)
//	AO_MODEL_REVALIDATION_INTERVAL model-pin probe period (Go duration >= 0, 0 disables, default 24h)
//	AO_PRIME_PROJECT_ID  legacy Prime migration hint, reported by the API but no longer enables Prime
//	AO_PRIME_DISPLAY_NAME legacy Prime migration hint, <= 20 runes
//	AO_ALLOWED_ORIGINS   CORS origins, comma-separated (default DefaultAllowedOrigins)
//	AO_MOBILE_ADVERTISED_HOST  host advertised in the Connect Mobile pairing status/QR (default: interface autopick)
//	AO_TELEMETRY_EVENTS  local event capture off|on (default off)
//	AO_TELEMETRY_METRICS local metric capture off|on (default off)
//	AO_TELEMETRY_REMOTE  remote exporter off|posthog (default off)
//	AO_TELEMETRY_POSTHOG_KEY   PostHog project key
//	AO_TELEMETRY_POSTHOG_HOST  PostHog host (default DefaultTelemetryPostHogHost)
//	AO_METRICS_INTERVAL           metrics sampling interval (Go duration, default 30s; 0 disables)
//	AO_METRICS_LOW_QUOTA_PERCENT  low-quota threshold percent (default 10; 0 disables)
//
// The bind host is not configurable: the daemon is loopback-only by design.
func Load() (Config, error) {
	cfg := Config{
		Host:                      LoopbackHost,
		Port:                      DefaultPort,
		RequestTimeout:            DefaultRequestTimeout,
		ShutdownTimeout:           DefaultShutdownTimeout,
		Agent:                     DefaultAgent,
		AgentHealthInterval:       DefaultAgentHealthInterval,
		ModelRevalidationInterval: DefaultModelRevalidationInterval,
		AllowedOrigins:            DefaultAllowedOrigins,
		Telemetry: TelemetryConfig{
			Remote:      TelemetryRemoteOff,
			PostHogHost: DefaultTelemetryPostHogHost,
		},
		Metrics: MetricsConfig{
			Interval:        DefaultMetricsInterval,
			LowQuotaPercent: DefaultMetricsLowQuotaPercent,
		},
	}

	if raw := os.Getenv("AO_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid AO_PORT %q: %w", raw, err)
		}
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid AO_PORT %d: out of range 1-65535", port)
		}
		cfg.Port = port
	}

	if raw := os.Getenv("AO_REQUEST_TIMEOUT"); raw != "" {
		d, err := parsePositiveDuration("AO_REQUEST_TIMEOUT", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.RequestTimeout = d
	}

	if raw := os.Getenv("AO_SHUTDOWN_TIMEOUT"); raw != "" {
		d, err := parsePositiveDuration("AO_SHUTDOWN_TIMEOUT", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.ShutdownTimeout = d
	}

	if raw := os.Getenv("AO_AGENT"); raw != "" {
		cfg.Agent = raw
	}
	if raw := os.Getenv("AO_AGENT_HEALTH_INTERVAL"); raw != "" {
		duration, err := parseNonNegativeDuration("AO_AGENT_HEALTH_INTERVAL", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.AgentHealthInterval = duration
	}
	if raw := os.Getenv("AO_MODEL_REVALIDATION_INTERVAL"); raw != "" {
		duration, err := parseNonNegativeDuration("AO_MODEL_REVALIDATION_INTERVAL", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.ModelRevalidationInterval = duration
	}

	cfg.PrimeProjectID = strings.TrimSpace(os.Getenv("AO_PRIME_PROJECT_ID"))
	if raw := strings.TrimSpace(os.Getenv("AO_PRIME_DISPLAY_NAME")); raw != "" {
		if l := len([]rune(raw)); l > 20 {
			return Config{}, fmt.Errorf("invalid AO_PRIME_DISPLAY_NAME: must be <= 20 characters, got %d", l)
		}
		cfg.PrimeDisplayName = raw
	}

	if raw := strings.TrimSpace(os.Getenv("AO_MOBILE_ADVERTISED_HOST")); raw != "" {
		cfg.MobileAdvertisedHost = raw
	}

	if raw, ok := os.LookupEnv("AO_ALLOWED_ORIGINS"); ok && raw != "" {
		// Explicit override replaces the defaults entirely so a deployment can
		// also narrow the list. The "null" origin is rejected, never silently
		// dropped: an operator allowing it would open the no-auth daemon to
		// every sandboxed iframe on the web.
		origins := make([]string, 0, 4)
		for _, origin := range strings.Split(raw, ",") {
			origin = strings.TrimSpace(origin)
			if origin == "" {
				continue
			}
			if origin == "null" || origin == "*" {
				return Config{}, fmt.Errorf("invalid AO_ALLOWED_ORIGINS entry %q: wildcard and null origins are not allowed", origin)
			}
			origins = append(origins, origin)
		}
		cfg.AllowedOrigins = origins
	}

	if raw := os.Getenv("AO_TELEMETRY_EVENTS"); raw != "" {
		v, err := parseToggleEnv("AO_TELEMETRY_EVENTS", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Telemetry.Events = v
	}
	if raw := os.Getenv("AO_TELEMETRY_METRICS"); raw != "" {
		v, err := parseToggleEnv("AO_TELEMETRY_METRICS", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Telemetry.Metrics = v
	}
	if raw := os.Getenv("AO_TELEMETRY_REMOTE"); raw != "" {
		remote, err := parseTelemetryRemote(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid AO_TELEMETRY_REMOTE %q: %w", raw, err)
		}
		cfg.Telemetry.Remote = remote
	}
	if raw := os.Getenv("AO_TELEMETRY_POSTHOG_KEY"); raw != "" {
		cfg.Telemetry.PostHogKey = raw
	}
	if raw := os.Getenv("AO_TELEMETRY_POSTHOG_HOST"); raw != "" {
		cfg.Telemetry.PostHogHost = raw
	}
	if raw := os.Getenv("AO_METRICS_INTERVAL"); raw != "" {
		d, err := parseNonNegativeDuration("AO_METRICS_INTERVAL", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Metrics.Interval = d
	}
	if raw := os.Getenv("AO_METRICS_LOW_QUOTA_PERCENT"); raw != "" {
		v, err := parseNonNegativeFloat("AO_METRICS_LOW_QUOTA_PERCENT", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.Metrics.LowQuotaPercent = v
	}

	runFile, err := resolveRunFilePath()
	if err != nil {
		return Config{}, err
	}
	cfg.RunFilePath = runFile

	dataDir, err := resolveDataDir()
	if err != nil {
		return Config{}, err
	}
	cfg.DataDir = dataDir

	return cfg, nil
}

func parseToggleEnv(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be off|on", name)
	}
}

func parseTelemetryRemote(raw string) (TelemetryRemote, error) {
	switch TelemetryRemote(strings.ToLower(strings.TrimSpace(raw))) {
	case TelemetryRemoteOff:
		return TelemetryRemoteOff, nil
	case TelemetryRemotePostHog:
		return TelemetryRemotePostHog, nil
	default:
		return "", fmt.Errorf("must be off|posthog")
	}
}

func parseNonNegativeFloat(name, raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid %s %q: must be >= 0", name, raw)
	}
	return v, nil
}

// parsePositiveDuration rejects zero and negative durations: a zero
// RequestTimeout would expire every request instantly, and a non-positive
// ShutdownTimeout would defeat graceful shutdown.
func parsePositiveDuration(name, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be > 0", name, raw)
	}
	return d, nil
}

// parseNonNegativeDuration accepts zero as a documented disable sentinel but
// rejects malformed and negative durations.
func parseNonNegativeDuration(name, raw string) (time.Duration, error) {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("invalid %s %q: must be >= 0", name, raw)
	}
	return duration, nil
}

// resolveRunFilePath picks where running.json lives. An explicit AO_RUN_FILE
// wins; otherwise it sits under the canonical AO home directory so the CLI and
// Electron supervisor share one handshake location.
func resolveRunFilePath() (string, error) {
	if p, ok := os.LookupEnv("AO_RUN_FILE"); ok && p != "" {
		return p, nil
	}
	stateDir, err := defaultStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "running.json"), nil
}

// resolveDataDir picks where durable state (the SQLite DB) lives. An explicit
// AO_DATA_DIR wins; otherwise it defaults under the same canonical AO home
// directory as the run-file.
func resolveDataDir() (string, error) {
	if p, ok := os.LookupEnv("AO_DATA_DIR"); ok && p != "" {
		return p, nil
	}
	stateDir, err := defaultStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "data"), nil
}

func defaultStateDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve state dir: %w", err)
	}
	return filepath.Join(homeDir, ".ao"), nil
}
