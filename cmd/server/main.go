package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/config"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/connect"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/httpapi"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/session_audit"
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
	credentials, err := connect.NewCredentialProvider(ctx, cfg.SSHKeyProvider, cfg.SSHPrivateKeyPEM, cfg.AWSRegion)
	if err != nil {
		slog.Error("initialize SSH credential provider failed", "provider", cfg.SSHKeyProvider, "error", err)
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
		session_audit.NewSessionAudit(cfg.SessionAuditURL),
		cfg.AutomationTimeout,
		cfg.AutomationOutputBytes,
	)
	if err := svc.Start(ctx); err != nil {
		slog.Error("register proxy dataplane failed", "error", err)
		os.Exit(1)
	}
	addr := ":" + cfg.Port
	slog.Info("server_listening", "addr", addr, "podId", cfg.PodID, "instanceId", cfg.InstanceID)
	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(svc),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
