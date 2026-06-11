package service

import (
	"context"
	"log"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

func (s *Service) Start(ctx context.Context) error {
	if err := s.sm.RegisterPod(ctx, s.podWithActive()); err != nil {
		return err
	}
	go s.heartbeatLoop(ctx)
	return nil
}
func (s *Service) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sm.HeartbeatPod(ctx, s.podWithActive()); err != nil {
				log.Printf("proxy pod heartbeat failed podId=%s error=%v", s.pod.PodID, err)
			}
		}
	}
}

func (s *Service) podWithActive() model.ProxyPod {
	s.mu.RLock()
	active := len(s.active)
	s.mu.RUnlock()
	pod := s.pod
	pod.Status = "READY"
	pod.ActiveSessions = active
	return pod
}
