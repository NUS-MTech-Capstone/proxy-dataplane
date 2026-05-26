package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/client"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

type Service struct {
	sm       *client.SessionManager
	sts      *client.STS
	pod      model.ProxyPod
	mu       sync.RWMutex
	active   map[string]struct{}
	interval time.Duration
}

func New(sm *client.SessionManager, sts *client.STS, pod model.ProxyPod, interval time.Duration) *Service {
	return &Service{sm: sm, sts: sts, pod: pod, active: make(map[string]struct{}), interval: interval}
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.sm.RegisterPod(ctx, s.podWithActive()); err != nil {
		return err
	}
	go s.heartbeatLoop(ctx)
	return nil
}

func (s *Service) ValidateSession(ctx context.Context, sessionID, token string) error {
	if err := s.sts.ValidateToken(ctx, sessionID, token); err != nil {
		return err
	}
	session, err := s.sm.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.OwnerPodID != s.pod.PodID || session.OwnerInstanceID != s.pod.InstanceID {
		return ErrOwnerMismatch
	}
	return nil
}

func (s *Service) StartSession(ctx context.Context, sessionID string) {
	s.mu.Lock()
	s.active[sessionID] = struct{}{}
	s.mu.Unlock()
	_ = s.sm.HeartbeatSession(ctx, sessionID, s.pod.PodID, s.pod.InstanceID)
}

func (s *Service) CloseSession(ctx context.Context, sessionID string) {
	s.mu.Lock()
	delete(s.active, sessionID)
	s.mu.Unlock()
	if err := s.sm.CloseSession(ctx, sessionID); err != nil {
		log.Printf("close session failed sessionId=%s error=%v", sessionID, err)
	}
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

var ErrOwnerMismatch = ownerMismatchError{}

type ownerMismatchError struct{}

func (ownerMismatchError) Error() string {
	return "session is owned by another proxy dataplane instance"
}
