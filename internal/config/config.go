package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                  string
	SessionManagerURL     string
	SessionAuditURL       string
	STSURL                string
	SSHKeyProvider        string
	SSHPrivateKeyPEM      string
	AWSRegion             string
	SSHConnectTimeout     time.Duration
	SSHRejoinGrace        time.Duration
	ScrollbackBytes       int
	PublicURL             string
	OperationsURL         string
	PodID                 string
	PodIP                 string
	InstanceID            string
	MaxSessions           int
	HeartbeatInterval     time.Duration
	AutomationTimeout     time.Duration
	AutomationOutputBytes int
}

func Load() Config {
	maxSessions, err := strconv.Atoi(env("MAX_SESSIONS", "50"))
	if err != nil {
		maxSessions = 50
	}
	scrollbackBytes, err := strconv.Atoi(env("SSH_SCROLLBACK_BYTES", "262144"))
	if err != nil || scrollbackBytes <= 0 {
		scrollbackBytes = 262144
	}
	operationsURL := os.Getenv("PROXY_DATAPLANE_OPERATIONS_URL")
	port := env("PORT", "8083")
	podIP := env("POD_IP", "127.0.0.1")
	if operationsURL == "" {
		operationsURL = "http://" + podIP + ":" + port
	}
	automationOutputBytes, err := strconv.Atoi(env("AUTOMATION_MAX_OUTPUT_BYTES", "262144"))
	if err != nil || automationOutputBytes <= 0 {
		automationOutputBytes = 262144
	}
	return Config{
		Port:                  port,
		SessionManagerURL:     env("SESSION_MANAGER_URL", "http://localhost:8081"),
		SessionAuditURL:       env("SESSION_AUDIT_URL", "http://localhost:8085"),
		STSURL:                env("STS_URL", "http://localhost:8082"),
		SSHKeyProvider:        env("SSH_KEY_PROVIDER", "env"),
		SSHPrivateKeyPEM:      os.Getenv("SSH_PRIVATE_KEY_PEM"),
		AWSRegion:             os.Getenv("AWS_REGION"),
		SSHConnectTimeout:     durationEnv("SSH_CONNECT_TIMEOUT", 10*time.Second),
		SSHRejoinGrace:        durationEnv("SSH_REJOIN_GRACE", 5*time.Minute),
		ScrollbackBytes:       scrollbackBytes,
		PublicURL:             env("PROXY_DATAPLANE_PUBLIC_URL", "ws://localhost:8083"),
		OperationsURL:         operationsURL,
		PodID:                 env("POD_ID", hostname()),
		PodIP:                 podIP,
		InstanceID:            env("OWNER_INSTANCE_ID", randomID()),
		MaxSessions:           maxSessions,
		HeartbeatInterval:     5 * time.Second,
		AutomationTimeout:     durationEnv("AUTOMATION_OPERATION_TIMEOUT", 30*time.Second),
		AutomationOutputBytes: automationOutputBytes,
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(env(key, fallback.String()))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "proxy-dataplane-local"
	}
	return name
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "boot-local"
	}
	return "boot-" + hex.EncodeToString(b)
}
