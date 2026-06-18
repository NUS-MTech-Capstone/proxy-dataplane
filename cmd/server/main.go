package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/config"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/connect"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/httpapi"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/session_manager"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/sts"
)

func main() {
	observability.ConfigureLogger("proxy-dataplane")
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pod := model.ProxyPod{
		PodID:         cfg.PodID,
		InstanceID:    cfg.InstanceID,
		PodIP:         cfg.PodIP,
		WebsocketURL:  cfg.PublicURL,
		OperationsURL: cfg.OperationsURL,
		Status:        "READY",
		MaxSessions:   cfg.MaxSessions,
	}
	var credentials connect.CredentialProvider
	switch strings.ToLower(cfg.SSHKeyProvider) {
	case "", "env":
		credentials = connect.EnvCredentials{PrivateKeyPEM: cfg.SSHPrivateKeyPEM}
	case "aws":
		var err error
		credentials, err = connect.NewSecretsManagerCredentials(ctx, cfg.AWSRegion)
		if err != nil {
			slog.Error("initialize AWS Secrets Manager credentials failed", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unsupported SSH_KEY_PROVIDER", "provider", cfg.SSHKeyProvider)
		os.Exit(1)
	}
	svc := service.New(
		session_manager.NewSessionManager(cfg.SessionManagerURL),
		sts.NewSTS(cfg.STSURL),
		connect.NewSSH(credentials, cfg.SSHConnectTimeout),
		pod,
		cfg.HeartbeatInterval,
		cfg.SSHRejoinGrace,
		cfg.ScrollbackBytes,
		cfg.AutomationTimeout,
		cfg.AutomationOutputBytes,
	)
	if err := svc.Start(ctx); err != nil {
		slog.Error("register proxy dataplane failed", "error", err)
		os.Exit(1)
	}
	addr := ":" + cfg.Port
	slog.Info("server_listening", "addr", addr, "podId", cfg.PodID, "instanceId", cfg.InstanceID)
	if err := http.ListenAndServe(addr, httpapi.New(svc)); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
