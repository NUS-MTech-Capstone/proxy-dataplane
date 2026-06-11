package service

import (
	"context"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

type fakeSessionManager struct {
	session model.Session
}

func (fakeSessionManager) RegisterPod(context.Context, model.ProxyPod) error  { return nil }
func (fakeSessionManager) HeartbeatPod(context.Context, model.ProxyPod) error { return nil }
func (f fakeSessionManager) GetSession(context.Context, string) (model.Session, error) {
	return f.session, nil
}
func (fakeSessionManager) HeartbeatSession(context.Context, string, string, string) error {
	return nil
}
func (fakeSessionManager) CloseSession(context.Context, string) error { return nil }

type fakeTokenValidator struct{}

func (fakeTokenValidator) ValidateToken(context.Context, string, string) error { return nil }

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, connection.Command) (connection.Result, error) {
	return connection.Result{}, nil
}

func TestValidateSessionRejectsAuthenticatedUserMismatch(t *testing.T) {
	svc := New(
		fakeSessionManager{session: model.Session{
			SessionID:       "sess-1",
			UserID:          "owner-123",
			OwnerPodID:      "pod-1",
			OwnerInstanceID: "instance-1",
			Status:          model.SessionStatusActive,
		}},
		fakeTokenValidator{},
		nil,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Second,
	)

	err := svc.ValidateSession(t.Context(), "sess-1", "token", "other-user")
	if err != ErrUserMismatch {
		t.Fatalf("ValidateSession() error = %v, want %v", err, ErrUserMismatch)
	}
}

func TestStartSessionAllowsOnlyOneWriter(t *testing.T) {
	svc := New(
		fakeSessionManager{},
		fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour,
	)

	if err := svc.StartSession(t.Context(), "sess-1"); err != nil {
		t.Fatal(err)
	}
	defer svc.AbortSession("sess-1")

	if err := svc.StartSession(t.Context(), "sess-1"); err != ErrSessionAlreadyActive {
		t.Fatalf("StartSession() error = %v, want %v", err, ErrSessionAlreadyActive)
	}
}
