package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/client"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/config"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/httpapi"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
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
	svc := service.New(client.NewSessionManager(cfg.SessionManagerURL), client.NewSTS(cfg.STSURL), pod, cfg.HeartbeatInterval)
	if err := svc.Start(ctx); err != nil {
		log.Fatalf("register proxy dataplane failed: %v", err)
	}
	addr := ":" + cfg.Port
	log.Printf("proxy-dataplane listening on %s podId=%s instanceId=%s", addr, cfg.PodID, cfg.InstanceID)
	log.Fatal(http.ListenAndServe(addr, httpapi.New(svc)))
}
