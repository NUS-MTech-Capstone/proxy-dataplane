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
	if cfg.SessionManagerURL != "http://localhost:8081" || cfg.SessionAuditURL != "http://localhost:8085" || cfg.STSURL != "http://localhost:8082" ||
		cfg.SSHKeyProvider != "env" || cfg.PublicURL != "ws://localhost:8083" {
		t.Fatalf("defaults = %#v", cfg)
	}
	if got := strings.Join(cfg.WebSocketOrigins, ","); got != "http://localhost:8083,http://localhost:4200,http://127.0.0.1:4200" {
		t.Fatalf("WebSocketOrigins = %q", got)
	}
}

func TestLoadWebSocketOrigins(t *testing.T) {
	t.Setenv("PROXY_DATAPLANE_PUBLIC_URL", "wss://api.ddm.keiyam.me")
	cfg := Load()
	if got := strings.Join(cfg.WebSocketOrigins, ","); got != "https://api.ddm.keiyam.me" {
		t.Fatalf("derived WebSocketOrigins = %q", got)
	}

	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "https://one.example, https://two.example,https://one.example")
	cfg = Load()
	if got := strings.Join(cfg.WebSocketOrigins, ","); got != "https://one.example,https://two.example" {
		t.Fatalf("configured WebSocketOrigins = %q", got)
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

func TestIdentitySettingsAndIssuerDerivation(t *testing.T) {
	t.Setenv("AWS_REGION", "ap-southeast-1")
	t.Setenv("ALB_ARN", "configured-alb")
	t.Setenv("COGNITO_CLIENT_ID", "configured-client")
	t.Setenv("COGNITO_USER_POOL_ID", "ap-southeast-1_fixture")
	t.Setenv("COGNITO_ISSUER", "")
	cfg := Load()
	if cfg.CognitoIssuer != "https://cognito-idp.ap-southeast-1.amazonaws.com/ap-southeast-1_fixture" || cfg.ALBARN != "configured-alb" || cfg.CognitoClientID != "configured-client" {
		t.Fatal("identity settings not loaded")
	}
	t.Setenv("COGNITO_ISSUER", "https://explicit-issuer")
	if Load().CognitoIssuer != "https://explicit-issuer" {
		t.Fatal("explicit issuer was not preserved")
	}
}
