package service

import (
	"context"
	"log"
)

var ErrOwnerMismatch = ownerMismatchError{}

type ownerMismatchError struct{}

func (ownerMismatchError) Error() string {
	return "session is owned by another proxy dataplane instance"
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
