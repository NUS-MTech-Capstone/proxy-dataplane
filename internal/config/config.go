package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	SessionManagerURL string
	STSURL            string
	AgentURLTemplate  string
	CommandTimeout    time.Duration
	PublicURL         string
	PodID             string
	PodIP             string
	InstanceID        string
	MaxSessions       int
	HeartbeatInterval time.Duration
}

func Load() Config {
	maxSessions, err := strconv.Atoi(env("MAX_SESSIONS", "50"))
	if err != nil {
		maxSessions = 50
	}
	return Config{
		Port:              env("PORT", "8083"),
		SessionManagerURL: env("SESSION_MANAGER_URL", "http://localhost:8081"),
		STSURL:            env("STS_URL", "http://localhost:8082"),
		AgentURLTemplate:  env("DDM_AGENT_URL_TEMPLATE", "http://localhost:8084"),
		CommandTimeout:    durationEnv("COMMAND_TIMEOUT", 30*time.Second),
		PublicURL:         env("PROXY_DATAPLANE_PUBLIC_URL", "ws://localhost:8083"),
		PodID:             env("POD_ID", hostname()),
		PodIP:             env("POD_IP", "127.0.0.1"),
		InstanceID:        env("OWNER_INSTANCE_ID", randomID()),
		MaxSessions:       maxSessions,
		HeartbeatInterval: 5 * time.Second,
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
