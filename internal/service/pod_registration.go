package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

func (s *Service) Start(ctx context.Context) error {
	s.rootCtx = ctx
	if err := s.sm.RegisterPod(ctx, s.podWithActive()); err != nil {
		return err
	}
	go s.heartbeatLoop(ctx)
	go func() {
		<-ctx.Done()
		s.mu.RLock()
		runtimes := make([]*Runtime, 0, len(s.runtimes))
		for _, runtime := range s.runtimes {
			runtimes = append(runtimes, runtime)
		}
		s.mu.RUnlock()
		for _, runtime := range runtimes {
			runtime.Close(ctx.Err())
		}
	}()
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
				slog.WarnContext(ctx, "ProxyPodHeartbeatFailed",
					"podId", s.pod.PodID,
					"instanceId", s.pod.InstanceID,
					"errorKind", observability.ErrorKind(err),
					"error", err,
				)
				observability.EmitCount("proxy-dataplane", "HeartbeatMisses", map[string]string{"Component": "proxy_pod", "Reason": observability.ErrorKind(err)})
			}
		}
	}
}

func (s *Service) podWithActive() model.ProxyPod {
	s.mu.RLock()
	active := len(s.runtimes)
	s.mu.RUnlock()
	pod := s.pod
	pod.Status = "READY"
	pod.ActiveSessions = active
	return pod
}
