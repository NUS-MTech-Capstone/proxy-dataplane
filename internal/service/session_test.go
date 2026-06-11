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
func (fakeSessionManager) CloseSession(context.Context, string, string, string) error { return nil }

type fakeTokenValidator struct {
	sessionID string
	userID    string
	mode      string
	token     string
}

func (f *fakeTokenValidator) ValidateToken(_ context.Context, sessionID, userID, mode, token string) error {
	f.sessionID = sessionID
	f.userID = userID
	f.mode = mode
	f.token = token
	return nil
}

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, connection.Command) (connection.Result, error) {
	return connection.Result{}, nil
}

func TestValidateSessionRejectsAuthenticatedUserMismatch(t *testing.T) {
	validator := &fakeTokenValidator{}
	svc := New(
		fakeSessionManager{session: model.Session{
			SessionID:       "sess-1",
			UserID:          "owner-123",
			OwnerPodID:      "pod-1",
			OwnerInstanceID: "instance-1",
			Status:          model.SessionStatusActive,
		}},
		validator,
		nil,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Second,
	)

	err := svc.ValidateSession(t.Context(), "sess-1", "token", "other-user")
	if err != ErrUserMismatch {
		t.Fatalf("ValidateSession() error = %v, want %v", err, ErrUserMismatch)
	}
	if validator.sessionID != "sess-1" || validator.userID != "other-user" ||
		validator.mode != "owner" || validator.token != "token" {
		t.Fatalf("token validation = %#v, want session/user/owner/token binding", validator)
	}
}

func TestAuthorizeObserverAllowsDifferentAuthenticatedUser(t *testing.T) {
	validator := &fakeTokenValidator{}
	svc := New(
		fakeSessionManager{session: model.Session{
			SessionID:       "sess-1",
			UserID:          "owner-123",
			OwnerPodID:      "pod-1",
			OwnerInstanceID: "instance-1",
			Status:          model.SessionStatusActive,
		}},
		validator,
		nil,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Second,
	)

	if _, err := svc.AuthorizeSession(t.Context(), "sess-1", "token", "observer-456", "observer"); err != nil {
		t.Fatalf("AuthorizeSession() error = %v", err)
	}
	if validator.mode != "observer" || validator.userID != "observer-456" {
		t.Fatalf("token validation = %#v, want observer mode and authenticated user", validator)
	}
}

func TestPublishSendsOutputToObservers(t *testing.T) {
	svc := New(
		fakeSessionManager{},
		&fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{},
		time.Second,
	)
	outputs, unsubscribe := svc.Subscribe("sess-1")
	defer unsubscribe()

	svc.Publish("sess-1", []byte("command output"))

	select {
	case output := <-outputs:
		if got, want := string(output), "command output"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not receive published output")
	}
}

func TestStartSessionAllowsOnlyOneWriter(t *testing.T) {
	svc := New(
		fakeSessionManager{},
		&fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour,
	)

	if _, err := svc.StartSession(t.Context(), "sess-1"); err != nil {
		t.Fatal(err)
	}
	defer svc.AbortSession("sess-1")

	if _, err := svc.StartSession(t.Context(), "sess-1"); err != ErrSessionAlreadyActive {
		t.Fatalf("StartSession() error = %v, want %v", err, ErrSessionAlreadyActive)
	}
}

type recordingSessionManager struct {
	mu                sync.Mutex
	registered        []model.ProxyPod
	podHeartbeats     []model.ProxyPod
	sessionHeartbeats []string
	closedSessions    []string
	heartbeatErr      error
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
	return r.heartbeatErr
}

func (r *recordingSessionManager) CloseSession(_ context.Context, sessionID, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closedSessions = append(r.closedSessions, sessionID)
	return nil
}

func TestStartRegistersPodAndSendsHeartbeat(t *testing.T) {
	sm := &recordingSessionManager{}
	svc := New(
		sm,
		&fakeTokenValidator{},
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
		&fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		5*time.Millisecond,
	)

	if _, err := svc.StartSession(t.Context(), "sess-1"); err != nil {
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

func TestDisconnectLeavesSessionRejoinable(t *testing.T) {
	sm := &recordingSessionManager{}
	svc := New(
		sm,
		&fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour,
	)

	if _, err := svc.StartSession(t.Context(), "sess-1"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	svc.DisconnectSession("sess-1")

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.closedSessions) != 0 {
		t.Fatalf("closed sessions = %#v, want none after disconnect", sm.closedSessions)
	}
}

type terminalHeartbeatError struct{}

func (terminalHeartbeatError) Error() string  { return "heartbeat rejected" }
func (terminalHeartbeatError) Terminal() bool { return true }

func TestTerminalHeartbeatFailureCancelsSession(t *testing.T) {
	sm := &recordingSessionManager{heartbeatErr: terminalHeartbeatError{}}
	svc := New(
		sm,
		&fakeTokenValidator{},
		fakeExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour,
	)

	sessionCtx, err := svc.StartSession(t.Context(), "sess-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	select {
	case <-sessionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("session context was not canceled after terminal heartbeat failure")
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
