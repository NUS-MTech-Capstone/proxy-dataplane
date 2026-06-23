package service

import (
	"context"
	"errors"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

var ErrOwnerMismatch = errors.New("session is owned by another proxy dataplane instance")
var ErrUserMismatch = errors.New("authenticated user does not own the session")
var ErrSessionNotActive = errors.New("session is not active")
var ErrRuntimeExists = errors.New("session runtime already exists")
var ErrRuntimeNotFound = errors.New("session runtime is not available on this dataplane instance")
var ErrWriterAlreadyAttached = errors.New("session already has an active writer")

type Access struct {
	Session model.Session
	Claims  model.SessionClaims
}

// ResolveSessionUserID returns the Cognito subject from ALB headers when present,
// otherwise from a validated STS session token. Some ALB WebSocket upgrades reach
// the dataplane without x-amzn-oidc-data even though the browser session is valid
// on /ddm API routes.
func (s *Service) ResolveSessionUserID(
	ctx context.Context,
	sessionID, token string,
	cognitoSubject string,
) (string, error) {
	if cognitoSubject != "" {
		return cognitoSubject, nil
	}
	claims, err := s.sts.ValidateToken(ctx, sessionID, "", token)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

func (s *Service) AuthorizeSession(ctx context.Context, sessionID, token, userID string) (Access, error) {
	claims, err := s.sts.ValidateToken(ctx, sessionID, userID, token)
	if err != nil {
		return Access{}, err
	}
	session, err := s.sm.GetSession(ctx, sessionID)
	if err != nil {
		return Access{}, err
	}
	if session.OwnerPodID != s.pod.PodID || session.OwnerInstanceID != s.pod.InstanceID {
		return Access{}, ErrOwnerMismatch
	}
	if session.Status != model.SessionStatusActive {
		return Access{}, ErrSessionNotActive
	}
	if claims.SessionID != session.SessionID || claims.HostID != session.HostID ||
		claims.CRID != session.CRID || claims.ConnectionType != session.ConnectionType {
		return Access{}, errors.New("token claims do not match session metadata")
	}
	if claims.UserID != userID {
		return Access{}, errors.New("token user does not match Cognito identity")
	}
	if claims.Mode == "owner" && session.UserID != userID {
		return Access{}, ErrUserMismatch
	}
	if claims.Action == "observe" && claims.Mode != "observer" ||
		claims.Action != "observe" && claims.Mode != "owner" {
		return Access{}, errors.New("invalid token action and mode")
	}
	return Access{Session: session, Claims: claims}, nil
}

func (s *Service) Attach(ctx context.Context, sessionID, token, userID string) (*Attachment, error) {
	access, err := s.AuthorizeSession(ctx, sessionID, token, userID)
	if err != nil {
		return nil, err
	}
	switch access.Claims.Action {
	case "start":
		return s.startRuntime(ctx, access)
	case "rejoin", "observe":
		runtime, ok := s.runtime(sessionID)
		if !ok {
			return nil, ErrRuntimeNotFound
		}
		return runtime.Attach(access.Claims.Mode, access.Claims.UserID)
	default:
		return nil, errors.New("unsupported session action")
	}
}

func (s *Service) startRuntime(ctx context.Context, access Access) (*Attachment, error) {
	if access.Claims.ConnectionType != "ssh" {
		return nil, errors.New("only ssh connectionType is supported")
	}
	if _, exists := s.runtime(access.Session.SessionID); exists {
		return nil, ErrRuntimeExists
	}
	shell, err := s.connector.Open(ctx, access.Claims)
	if err != nil {
		return nil, err
	}
	runtime := newRuntime(s, access.Session, shell)
	s.mu.Lock()
	if _, exists := s.runtimes[access.Session.SessionID]; exists {
		s.mu.Unlock()
		_ = shell.Close()
		return nil, ErrRuntimeExists
	}
	s.runtimes[access.Session.SessionID] = runtime
	s.mu.Unlock()
	runtime.Start()
	attachment, err := runtime.Attach("owner", access.Claims.UserID)
	if err != nil {
		runtime.Close(err)
		return nil, err
	}
	return attachment, nil
}
