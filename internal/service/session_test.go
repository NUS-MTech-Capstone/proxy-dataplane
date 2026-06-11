package service

import (
	"context"
	"sync"
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

type recordingSessionManager struct {
	mu                sync.Mutex
	registered        []model.ProxyPod
	podHeartbeats     []model.ProxyPod
	sessionHeartbeats []string
	closedSessions    []string
}

func (r *recordingSessionManager) RegisterPod(_ context.Context, pod model.ProxyPod) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered = append(r.registered, pod)
	return nil
}

func (r *recordingSessionManager) HeartbeatPod(_ context.Context, pod model.ProxyPod) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.podHeartbeats = append(r.podHeartbeats, pod)
	return nil
}

func (*recordingSessionManager) GetSession(context.Context, string) (model.Session, error) {
	return model.Session{}, nil
}

func (r *recordingSessionManager) HeartbeatSession(_ context.Context, sessionID, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionHeartbeats = append(r.sessionHeartbeats, sessionID)
	return nil
}

func (r *recordingSessionManager) CloseSession(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closedSessions = append(r.closedSessions, sessionID)
	return nil
}

func TestStartRegistersPodAndSendsHeartbeat(t *testing.T) {
	sm := &recordingSessionManager{}
	svc := New(
		sm,
		fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1", MaxSessions: 50},
		5*time.Millisecond,
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		return len(sm.podHeartbeats) > 0
	})

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.registered) != 1 {
		t.Fatalf("registrations = %d, want 1", len(sm.registered))
	}
	if sm.registered[0].Status != "READY" || sm.registered[0].ActiveSessions != 0 {
		t.Fatalf("registered pod = %#v, want READY with zero active sessions", sm.registered[0])
	}
}

func TestActiveSessionHeartbeatsAndCloses(t *testing.T) {
	sm := &recordingSessionManager{}
	svc := New(
		sm,
		fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		5*time.Millisecond,
	)

	if err := svc.StartSession(t.Context(), "sess-1"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		return len(sm.sessionHeartbeats) > 0
	})
	svc.CloseSession(t.Context(), "sess-1")

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.closedSessions) != 1 || sm.closedSessions[0] != "sess-1" {
		t.Fatalf("closed sessions = %#v, want sess-1", sm.closedSessions)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
