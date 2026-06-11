package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

var ErrOwnerMismatch = ownerMismatchError{}
var ErrUserMismatch = userMismatchError{}
var ErrSessionAlreadyActive = errors.New("session already has an active writer")
var ErrSessionNotActive = errors.New("session is not active")

type ownerMismatchError struct{}

func (ownerMismatchError) Error() string {
	return "session is owned by another proxy dataplane instance"
}

type userMismatchError struct{}

func (userMismatchError) Error() string {
	return "authenticated user does not own the session"
}

func (s *Service) ValidateSession(ctx context.Context, sessionID, token, userID string) error {
	_, err := s.AuthorizeSession(ctx, sessionID, token, userID)
	return err
}

func (s *Service) AuthorizeSession(ctx context.Context, sessionID, token, userID string) (model.Session, error) {
	if err := s.sts.ValidateToken(ctx, sessionID, userID, "owner", token); err != nil {
		return model.Session{}, err
	}
	session, err := s.sm.GetSession(ctx, sessionID)
	if err != nil {
		return model.Session{}, err
	}
	if session.OwnerPodID != s.pod.PodID || session.OwnerInstanceID != s.pod.InstanceID {
		return model.Session{}, ErrOwnerMismatch
	}
	if session.UserID != userID {
		return model.Session{}, ErrUserMismatch
	}
	if session.Status != model.SessionStatusActive {
		return model.Session{}, ErrSessionNotActive
	}
	return session, nil
}

func (s *Service) StartSession(ctx context.Context, sessionID string) (context.Context, error) {
	s.mu.Lock()
	if _, exists := s.active[sessionID]; exists {
		s.mu.Unlock()
		return nil, ErrSessionAlreadyActive
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s.active[sessionID] = cancel
	s.mu.Unlock()
	go s.sessionHeartbeatLoop(sessionCtx, cancel, sessionID)
	return sessionCtx, nil
}

func (s *Service) ExecuteCommand(ctx context.Context, session model.Session, token, command string) (connection.Result, error) {
	log.Printf("CommandExecuting sessionId=%s hostId=%s", session.SessionID, session.HostID)
	result, err := s.executor.Execute(ctx, connection.Command{
		SessionID: session.SessionID,
		UserID:    session.UserID,
		HostID:    session.HostID,
		CRID:      session.CRID,
		Token:     token,
		Value:     command,
	})
	if err != nil {
		log.Printf("CommandFailed sessionId=%s hostId=%s error=%v", session.SessionID, session.HostID, err)
		return connection.Result{}, err
	}
	log.Printf("CommandExecuted sessionId=%s hostId=%s exitCode=%d", session.SessionID, session.HostID, result.ExitCode)
	return result, nil
}

func (s *Service) AbortSession(sessionID string) {
	s.removeActive(sessionID)
}

func (s *Service) CloseSession(ctx context.Context, sessionID string) {
	s.removeActive(sessionID)
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.sm.CloseSession(closeCtx, sessionID, s.pod.PodID, s.pod.InstanceID); err != nil {
		log.Printf("close session failed sessionId=%s error=%v", sessionID, err)
	}
}

func (s *Service) removeActive(sessionID string) {
	s.mu.Lock()
	cancel := s.active[sessionID]
	delete(s.active, sessionID)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) sessionHeartbeatLoop(ctx context.Context, cancel context.CancelFunc, sessionID string) {
	heartbeat := func() bool {
		if err := s.sm.HeartbeatSession(ctx, sessionID, s.pod.PodID, s.pod.InstanceID); err != nil && ctx.Err() == nil {
			log.Printf("session heartbeat failed sessionId=%s error=%v", sessionID, err)
			var terminal interface{ Terminal() bool }
			if errors.As(err, &terminal) && terminal.Terminal() {
				cancel()
				return false
			}
		}
		return true
	}
	if !heartbeat() {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !heartbeat() {
				return
			}
		}
	}
}
