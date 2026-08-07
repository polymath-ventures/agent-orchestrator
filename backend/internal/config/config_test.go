package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear every recognised var so we observe pure defaults regardless of the
	// surrounding environment.
	for _, k := range []string{"AO_PORT", "AO_REQUEST_TIMEOUT", "AO_SHUTDOWN_TIMEOUT", "AO_RUN_FILE", "AO_DATA_DIR", "AO_AGENT", "AO_AGENT_HEALTH_INTERVAL", "AO_MODEL_REVALIDATION_INTERVAL", "AO_ALLOWED_ORIGINS", "AO_MOBILE_ADVERTISED_HOST", "AO_TELEMETRY_EVENTS", "AO_TELEMETRY_METRICS", "AO_TELEMETRY_REMOTE", "AO_TELEMETRY_POSTHOG_KEY", "AO_TELEMETRY_POSTHOG_HOST", "AO_TELEMETRY_DISABLED_EVENTS", "AO_TELEMETRY_APP_VERSION", "AO_METRICS_INTERVAL", "AO_METRICS_LOW_QUOTA_PERCENT", "AO_QUOTA_PROBE_INTERVAL", "AO_WORKER_TASK_PROMPT"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != LoopbackHost {
		t.Errorf("Host = %q, want %q", cfg.Host, LoopbackHost)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("RequestTimeout = %s, want %s", cfg.RequestTimeout, DefaultRequestTimeout)
	}
	if cfg.ShutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, DefaultShutdownTimeout)
	}
	if cfg.RunFilePath == "" {
		t.Error("RunFilePath is empty, want a resolved default path")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	wantRunFilePath := filepath.Join(homeDir, ".ao", "running.json")
	if cfg.RunFilePath != wantRunFilePath {
		t.Errorf("RunFilePath = %q, want %q", cfg.RunFilePath, wantRunFilePath)
	}
	if cfg.DataDir == "" {
		t.Error("DataDir is empty, want a resolved default path")
	}
	wantDataDir := filepath.Join(homeDir, ".ao", "data")
	if cfg.DataDir != wantDataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, wantDataDir)
	}
	if cfg.Telemetry.Remote != TelemetryRemoteOff || cfg.Telemetry.PostHogHost != DefaultTelemetryPostHogHost {
		t.Fatalf("Telemetry defaults = %+v", cfg.Telemetry)
	}
	if cfg.Metrics.Interval != DefaultMetricsInterval || cfg.Metrics.LowQuotaPercent != DefaultMetricsLowQuotaPercent {
		t.Fatalf("Metrics defaults = %+v", cfg.Metrics)
	}
	if cfg.Metrics.QuotaProbeInterval != DefaultQuotaProbeInterval {
		t.Fatalf("QuotaProbeInterval default = %s, want %s", cfg.Metrics.QuotaProbeInterval, DefaultQuotaProbeInterval)
	}
	if cfg.AgentHealthInterval != DefaultAgentHealthInterval {
		t.Errorf("AgentHealthInterval = %s, want %s", cfg.AgentHealthInterval, DefaultAgentHealthInterval)
	}
	if cfg.ModelRevalidationInterval != DefaultModelRevalidationInterval {
		t.Errorf("ModelRevalidationInterval = %s, want %s", cfg.ModelRevalidationInterval, DefaultModelRevalidationInterval)
	}
	if cfg.MobileAdvertisedHost != "" {
		t.Errorf("MobileAdvertisedHost = %q, want empty default", cfg.MobileAdvertisedHost)
	}
	if cfg.ProjectDefaults.WorkerTaskPrompt != "" {
		t.Errorf("ProjectDefaults.WorkerTaskPrompt = %q, want empty default", cfg.ProjectDefaults.WorkerTaskPrompt)
	}
}

func TestLoadWorkerTaskPromptDefault(t *testing.T) {
	t.Setenv("AO_WORKER_TASK_PROMPT", "/address-issue {issue}\n")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ProjectDefaults.WorkerTaskPrompt; got != "/address-issue {issue}\n" {
		t.Fatalf("WorkerTaskPrompt = %q, want bytes preserved", got)
	}
}

func TestLoadPreservesWhitespaceWorkerTaskPromptDefaultForSpawnValidation(t *testing.T) {
	t.Setenv("AO_WORKER_TASK_PROMPT", " \n\t")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ProjectDefaults.WorkerTaskPrompt; got != " \n\t" {
		t.Fatalf("WorkerTaskPrompt = %q, want active whitespace preserved for spawn-time failure", got)
	}
}

func TestLoadAgentHealthInterval(t *testing.T) {
	t.Setenv("AO_AGENT_HEALTH_INTERVAL", "90s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentHealthInterval != 90*time.Second {
		t.Errorf("AgentHealthInterval = %s, want 90s", cfg.AgentHealthInterval)
	}

	t.Setenv("AO_AGENT_HEALTH_INTERVAL", "0")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load zero: %v", err)
	}
	if cfg.AgentHealthInterval != 0 {
		t.Errorf("AgentHealthInterval = %s, want 0 (disabled)", cfg.AgentHealthInterval)
	}
}

func TestLoadModelRevalidationInterval(t *testing.T) {
	t.Setenv("AO_MODEL_REVALIDATION_INTERVAL", "12h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ModelRevalidationInterval != 12*time.Hour {
		t.Errorf("ModelRevalidationInterval = %s, want 12h", cfg.ModelRevalidationInterval)
	}

	t.Setenv("AO_MODEL_REVALIDATION_INTERVAL", "0")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load zero: %v", err)
	}
	if cfg.ModelRevalidationInterval != 0 {
		t.Errorf("ModelRevalidationInterval = %s, want 0 (disabled)", cfg.ModelRevalidationInterval)
	}
}

func TestLoadAbsolutizesRelativeOverrides(t *testing.T) {
	// A relative override must be resolved to absolute at Load time. The daemon
	// chdir's into its data dir at startup, so a relative path left as-is would
	// be re-resolved against the new cwd and double-nest state.
	t.Setenv("AO_RUN_FILE", "rel-running.json")
	t.Setenv("AO_DATA_DIR", "rel-data")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !filepath.IsAbs(cfg.RunFilePath) {
		t.Errorf("RunFilePath = %q, want absolute", cfg.RunFilePath)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Errorf("DataDir = %q, want absolute", cfg.DataDir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cwd, "rel-data"); cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if want := filepath.Join(cwd, "rel-running.json"); cfg.RunFilePath != want {
		t.Errorf("RunFilePath = %q, want %q", cfg.RunFilePath, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	overrideDir := t.TempDir()
	runFilePath := filepath.Join(overrideDir, "ao-test-running.json")
	dataDir := filepath.Join(overrideDir, "ao-test-data")

	t.Setenv("AO_PORT", "4002")
	t.Setenv("AO_REQUEST_TIMEOUT", "5s")
	t.Setenv("AO_SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("AO_RUN_FILE", runFilePath)
	t.Setenv("AO_DATA_DIR", dataDir)
	t.Setenv("AO_TELEMETRY_EVENTS", "on")
	t.Setenv("AO_TELEMETRY_METRICS", "off")
	t.Setenv("AO_TELEMETRY_REMOTE", "posthog")
	t.Setenv("AO_TELEMETRY_POSTHOG_KEY", "phc_test")
	t.Setenv("AO_TELEMETRY_POSTHOG_HOST", "https://eu.i.posthog.com")
	t.Setenv("AO_METRICS_INTERVAL", "2m")
	t.Setenv("AO_METRICS_LOW_QUOTA_PERCENT", "7.5")
	t.Setenv("AO_QUOTA_PROBE_INTERVAL", "30m")
	t.Setenv("AO_MOBILE_ADVERTISED_HOST", "  ao-server.example.ts.net  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr() != "127.0.0.1:4002" {
		t.Errorf("Addr() = %q, want 127.0.0.1:4002", cfg.Addr())
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %s, want 5s", cfg.RequestTimeout)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 3s", cfg.ShutdownTimeout)
	}
	if cfg.RunFilePath != runFilePath {
		t.Errorf("RunFilePath = %q, want %q", cfg.RunFilePath, runFilePath)
	}
	if cfg.DataDir != dataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dataDir)
	}
	if !cfg.Telemetry.Events || cfg.Telemetry.Metrics {
		t.Fatalf("Telemetry toggles = %+v", cfg.Telemetry)
	}
	if cfg.Telemetry.Remote != TelemetryRemotePostHog || cfg.Telemetry.PostHogKey != "phc_test" || cfg.Telemetry.PostHogHost != "https://eu.i.posthog.com" {
		t.Fatalf("Telemetry remote = %+v", cfg.Telemetry)
	}
	if cfg.Metrics.QuotaProbeInterval != 30*time.Minute {
		t.Fatalf("QuotaProbeInterval = %s, want 30m", cfg.Metrics.QuotaProbeInterval)
	}
	if cfg.Metrics.Interval != 2*time.Minute || cfg.Metrics.LowQuotaPercent != 7.5 {
		t.Fatalf("Metrics config = %+v", cfg.Metrics)
	}
	if cfg.MobileAdvertisedHost != "ao-server.example.ts.net" {
		t.Errorf("MobileAdvertisedHost = %q, want trimmed override", cfg.MobileAdvertisedHost)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"non-numeric port", map[string]string{"AO_PORT": "abc"}},
		{"port out of range", map[string]string{"AO_PORT": "70000"}},
		{"bad request timeout", map[string]string{"AO_REQUEST_TIMEOUT": "soon"}},
		{"bad shutdown timeout", map[string]string{"AO_SHUTDOWN_TIMEOUT": "later"}},
		{"zero request timeout", map[string]string{"AO_REQUEST_TIMEOUT": "0s"}},
		{"negative request timeout", map[string]string{"AO_REQUEST_TIMEOUT": "-1s"}},
		{"zero shutdown timeout", map[string]string{"AO_SHUTDOWN_TIMEOUT": "0s"}},
		{"negative shutdown timeout", map[string]string{"AO_SHUTDOWN_TIMEOUT": "-5s"}},
		{"bad agent health interval", map[string]string{"AO_AGENT_HEALTH_INTERVAL": "soon"}},
		{"negative agent health interval", map[string]string{"AO_AGENT_HEALTH_INTERVAL": "-1m"}},
		{"bad model revalidation interval", map[string]string{"AO_MODEL_REVALIDATION_INTERVAL": "daily"}},
		{"negative model revalidation interval", map[string]string{"AO_MODEL_REVALIDATION_INTERVAL": "-1m"}},
		{"null origin", map[string]string{"AO_ALLOWED_ORIGINS": "app://renderer,null"}},
		{"wildcard origin", map[string]string{"AO_ALLOWED_ORIGINS": "*"}},
		{"bad telemetry events", map[string]string{"AO_TELEMETRY_EVENTS": "maybe"}},
		{"bad telemetry metrics", map[string]string{"AO_TELEMETRY_METRICS": "maybe"}},
		{"bad telemetry remote", map[string]string{"AO_TELEMETRY_REMOTE": "otlp"}},
		{"bad metrics interval", map[string]string{"AO_METRICS_INTERVAL": "later"}},
		{"negative metrics interval", map[string]string{"AO_METRICS_INTERVAL": "-1s"}},
		{"bad quota probe interval", map[string]string{"AO_QUOTA_PROBE_INTERVAL": "hourly"}},
		{"negative quota probe interval", map[string]string{"AO_QUOTA_PROBE_INTERVAL": "-1s"}},
		{"bad low quota percent", map[string]string{"AO_METRICS_LOW_QUOTA_PERCENT": "low"}},
		{"negative low quota percent", map[string]string{"AO_METRICS_LOW_QUOTA_PERCENT": "-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("Load() = nil error, want error")
			}
		})
	}
}

func TestLoadAllowedOrigins(t *testing.T) {
	t.Run("default includes the packaged renderer origin", func(t *testing.T) {
		t.Setenv("AO_ALLOWED_ORIGINS", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		found := false
		for _, origin := range cfg.AllowedOrigins {
			if origin == "app://renderer" {
				found = true
			}
		}
		if !found {
			t.Errorf("AllowedOrigins = %v, want app://renderer included", cfg.AllowedOrigins)
		}
	})

	t.Run("override replaces defaults and trims entries", func(t *testing.T) {
		t.Setenv("AO_ALLOWED_ORIGINS", " app://renderer , http://localhost:9999 ,")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := []string{"app://renderer", "http://localhost:9999"}
		if len(cfg.AllowedOrigins) != len(want) {
			t.Fatalf("AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
		}
		for i, origin := range want {
			if cfg.AllowedOrigins[i] != origin {
				t.Errorf("AllowedOrigins[%d] = %q, want %q", i, cfg.AllowedOrigins[i], origin)
			}
		}
	})
}

// The daemon reads Owner to decide whether the frontend-death watchdog is
// installed at all, so a break in this plumbing silently disables the watchdog
// for the desktop app — or, worse, leaves it installed on a persistent daemon.
func TestLoadOwner(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want string
	}{
		{"app", OwnerApp},
		{"persistent", "persistent"},
		{"", ""},
		// Not trimmed: an exact-match gate must fail closed on a malformed value.
		{"  app  ", "  app  "},
	} {
		t.Run("AO_OWNER="+tc.env, func(t *testing.T) {
			t.Setenv("AO_OWNER", tc.env)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Owner != tc.want {
				t.Fatalf("Owner = %q, want %q", cfg.Owner, tc.want)
			}
		})
	}
}

// The kill switch and the supervisor-supplied version are user-visible
// boundaries: the daemon reads them from the environment the desktop app hands
// it, so a parsing regression here silently disables the switch.
func TestLoadTelemetryDisabledEventsAndAppVersion(t *testing.T) {
	t.Setenv("AO_TELEMETRY_DISABLED_EVENTS", " ao.v2.app.active , ao.renderer.* ,, ")
	t.Setenv("AO_TELEMETRY_APP_VERSION", "  0.11.2  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"ao.v2.app.active", "ao.renderer.*"}
	if len(cfg.Telemetry.DisabledEvents) != len(want) {
		t.Fatalf("DisabledEvents = %#v, want %#v", cfg.Telemetry.DisabledEvents, want)
	}
	for i, name := range want {
		if cfg.Telemetry.DisabledEvents[i] != name {
			t.Fatalf("DisabledEvents[%d] = %q, want %q", i, cfg.Telemetry.DisabledEvents[i], name)
		}
	}
	if cfg.Telemetry.AppVersion != "0.11.2" {
		t.Fatalf("AppVersion = %q, want trimmed 0.11.2", cfg.Telemetry.AppVersion)
	}
}

// An unparseable or blank list must never stop the daemon booting: the switch
// has to be usable in a hurry, so a bad entry is inert rather than fatal.
func TestLoadTelemetryDisabledEventsBlankIsInert(t *testing.T) {
	t.Setenv("AO_TELEMETRY_DISABLED_EVENTS", " , , ")
	t.Setenv("AO_TELEMETRY_APP_VERSION", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Telemetry.DisabledEvents) != 0 {
		t.Fatalf("DisabledEvents = %#v, want empty", cfg.Telemetry.DisabledEvents)
	}
	if cfg.Telemetry.AppVersion != "" {
		t.Fatalf("AppVersion = %q, want empty", cfg.Telemetry.AppVersion)
	}
}
