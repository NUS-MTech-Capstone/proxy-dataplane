package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndDerivedOperationsURL(t *testing.T) {
	t.Setenv("PORT", "9093")
	t.Setenv("POD_IP", "10.0.0.10")
	t.Setenv("POD_ID", "pod-1")
	t.Setenv("OWNER_INSTANCE_ID", "instance-1")

	cfg := Load()
	if cfg.Port != "9093" || cfg.OperationsURL != "http://10.0.0.10:9093" ||
		cfg.PodID != "pod-1" || cfg.InstanceID != "instance-1" {
		t.Fatalf("Load() = %#v", cfg)
	}
	if cfg.SessionManagerURL != "http://localhost:8081" || cfg.STSURL != "http://localhost:8082" ||
		cfg.SSHKeyProvider != "env" || cfg.PublicURL != "ws://localhost:8083" {
		t.Fatalf("defaults = %#v", cfg)
	}
}

func TestLoadParsesOverridesAndFallbacks(t *testing.T) {
	t.Setenv("SSH_CONNECT_TIMEOUT", "2s")
	t.Setenv("SSH_REJOIN_GRACE", "10s")
	t.Setenv("SSH_SCROLLBACK_BYTES", "bad")
	t.Setenv("MAX_SESSIONS", "bad")
	t.Setenv("AUTOMATION_OPERATION_TIMEOUT", "0s")
	t.Setenv("AUTOMATION_MAX_OUTPUT_BYTES", "-1")
	t.Setenv("PROXY_DATAPLANE_OPERATIONS_URL", "http://ops")
	t.Setenv("OWNER_INSTANCE_ID", "instance-1")

	cfg := Load()
	if cfg.SSHConnectTimeout != 2*time.Second || cfg.SSHRejoinGrace != 10*time.Second {
		t.Fatalf("durations = %#v", cfg)
	}
	if cfg.ScrollbackBytes != 262144 || cfg.MaxSessions != 50 ||
		cfg.AutomationTimeout != 30*time.Second || cfg.AutomationOutputBytes != 262144 ||
		cfg.OperationsURL != "http://ops" {
		t.Fatalf("fallbacks = %#v", cfg)
	}
}

func TestRandomIDAndEnvHelpers(t *testing.T) {
	if got := env("DDM_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("env() = %q", got)
	}
	id := randomID()
	if !strings.HasPrefix(id, "boot-") {
		t.Fatalf("randomID() = %q", id)
	}
}
