package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/config"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/connect"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/httpapi"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/session_manager"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/sts"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pod := model.ProxyPod{
		PodID:        cfg.PodID,
		InstanceID:   cfg.InstanceID,
		PodIP:        cfg.PodIP,
		WebsocketURL: cfg.PublicURL,
		Status:       "READY",
		MaxSessions:  cfg.MaxSessions,
	}
	var credentials connect.CredentialProvider
	switch strings.ToLower(cfg.SSHKeyProvider) {
	case "", "env":
		credentials = connect.EnvCredentials{PrivateKeyPEM: cfg.SSHPrivateKeyPEM}
	case "aws":
		var err error
		credentials, err = connect.NewSecretsManagerCredentials(ctx, cfg.AWSRegion)
		if err != nil {
			log.Fatalf("initialize AWS Secrets Manager credentials failed: %v", err)
		}
	default:
		log.Fatalf("unsupported SSH_KEY_PROVIDER %q", cfg.SSHKeyProvider)
	}
	svc := service.New(
		session_manager.NewSessionManager(cfg.SessionManagerURL),
		sts.NewSTS(cfg.STSURL),
		connect.NewSSH(credentials, cfg.SSHConnectTimeout),
		pod,
		cfg.HeartbeatInterval,
		cfg.SSHRejoinGrace,
		cfg.ScrollbackBytes,
	)
	if err := svc.Start(ctx); err != nil {
		log.Fatalf("register proxy dataplane failed: %v", err)
	}
	addr := ":" + cfg.Port
	log.Printf("proxy-dataplane listening on %s podId=%s instanceId=%s", addr, cfg.PodID, cfg.InstanceID)
	log.Fatal(http.ListenAndServe(addr, httpapi.New(svc)))
}
