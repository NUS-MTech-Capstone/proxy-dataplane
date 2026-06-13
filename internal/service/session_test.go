package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/protocol"
)

type fakeSessionManager struct {
	mu                sync.Mutex
	session           model.Session
	registered        []model.ProxyPod
	podHeartbeats     []model.ProxyPod
	sessionHeartbeats []string
	closedSessions    []string
	heartbeatErr      error
}

func (f *fakeSessionManager) RegisterPod(_ context.Context, pod model.ProxyPod) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, pod)
	return nil
}
func (f *fakeSessionManager) HeartbeatPod(_ context.Context, pod model.ProxyPod) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.podHeartbeats = append(f.podHeartbeats, pod)
	return nil
}
func (f *fakeSessionManager) GetSession(context.Context, string) (model.Session, error) {
	return f.session, nil
}
func (f *fakeSessionManager) HeartbeatSession(_ context.Context, sessionID, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionHeartbeats = append(f.sessionHeartbeats, sessionID)
	return f.heartbeatErr
}
func (f *fakeSessionManager) CloseSession(_ context.Context, sessionID, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedSessions = append(f.closedSessions, sessionID)
	return nil
}

type fakeTokenValidator struct {
	claims map[string]model.SessionClaims
}

func (f fakeTokenValidator) ValidateToken(_ context.Context, _, _, token string) (model.SessionClaims, error) {
	claims, ok := f.claims[token]
	if !ok {
		return model.SessionClaims{}, errors.New("invalid token")
	}
	return claims, nil
}

type fakeConnector struct {
	mu        sync.Mutex
	openCount int
	shell     *fakeShell
}

func (f *fakeConnector) Open(context.Context, model.SessionClaims) (connection.Shell, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCount++
	if f.shell == nil {
		f.shell = newFakeShell()
	}
	return f.shell, nil
}

type fakeShell struct {
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter
	wait    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	input   []byte
	cols    uint16
	rows    uint16
}

func newFakeShell() *fakeShell {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	return &fakeShell{
		stdoutR: stdoutR, stdoutW: stdoutW, stderrR: stderrR, stderrW: stderrW, wait: make(chan struct{}),
	}
}

func (s *fakeShell) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.input = append(s.input, data...)
	return len(data), nil
}
func (s *fakeShell) Stdout() io.Reader { return s.stdoutR }
func (s *fakeShell) Stderr() io.Reader { return s.stderrR }
func (s *fakeShell) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	return nil
}
func (s *fakeShell) Wait() error {
	<-s.wait
	return nil
}
func (s *fakeShell) Close() error {
	s.once.Do(func() {
		close(s.wait)
		_ = s.stdoutW.Close()
		_ = s.stderrW.Close()
		_ = s.stdoutR.Close()
		_ = s.stderrR.Close()
	})
	return nil
}

func testSession() model.Session {
	return model.Session{
		SessionID: "sess-1", UserID: "owner-1", HostID: "host-1", CRID: "cr-1",
		ConnectionType: "ssh", OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
		Status: model.SessionStatusActive,
	}
}

func testClaims(action, mode, userID string) model.SessionClaims {
	return model.SessionClaims{
		SessionID: "sess-1", UserID: userID, HostID: "host-1", CRID: "cr-1",
		ConnectionType: "ssh", Action: action, Mode: mode,
		Address: "host:22", Username: "ubuntu", CredentialSecretID: "secret",
		HostPublicKey: "ssh-ed25519 AAAA",
	}
}

func newTestService(sm *fakeSessionManager, connector *fakeConnector, grace time.Duration) *Service {
	validator := fakeTokenValidator{claims: map[string]model.SessionClaims{
		"start":   testClaims("start", "owner", "owner-1"),
		"rejoin":  testClaims("rejoin", "owner", "owner-1"),
		"observe": testClaims("observe", "observer", "observer-1"),
		"other":   testClaims("rejoin", "owner", "other-user"),
	}}
	return New(
		sm, validator, connector,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1", MaxSessions: 50},
		5*time.Millisecond, grace, 256*1024,
	)
}

func TestAuthorizeSessionRejectsAuthenticatedUserMismatch(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	svc := newTestService(sm, &fakeConnector{}, time.Second)
	if _, err := svc.AuthorizeSession(t.Context(), "sess-1", "other", "other-user"); !errors.Is(err, ErrUserMismatch) {
		t.Fatalf("AuthorizeSession() error = %v, want %v", err, ErrUserMismatch)
	}
}

func TestStartRejoinObserveAndScrollback(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	connector := &fakeConnector{}
	svc := newTestService(sm, connector, time.Second)

	owner, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if runtime, ok := svc.runtime("sess-1"); ok {
			runtime.Close(nil)
		}
	}()
	if _, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1"); !errors.Is(err, ErrRuntimeExists) {
		t.Fatalf("duplicate start error = %v, want %v", err, ErrRuntimeExists)
	}
	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("pwd\n")}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Handle(protocol.Packet{Type: protocol.Resize, Data: []byte{0, 120, 0, 40}}); err != nil {
		t.Fatal(err)
	}
	if _, err := connector.shell.stdoutW.Write([]byte("persistent output\r\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		connector.shell.mu.Lock()
		defer connector.shell.mu.Unlock()
		return string(connector.shell.input) == "pwd\n" && connector.shell.cols == 120 && connector.shell.rows == 40
	})

	owner.Close()
	rejoined, err := svc.Attach(t.Context(), "sess-1", "rejoin", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	defer rejoined.Close()
	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("stale\n")}); err == nil {
		t.Fatal("detached owner input was accepted")
	}
	observer, err := svc.Attach(t.Context(), "sess-1", "observe", "observer-1")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("id\n")}); err == nil {
		t.Fatal("observer input was accepted")
	}
	if !outputContains(t, rejoined, "persistent output") {
		t.Fatal("rejoined owner did not receive scrollback")
	}
	if !outputContains(t, observer, "persistent output") {
		t.Fatal("observer did not receive scrollback")
	}
	if err := rejoined.Handle(protocol.Packet{Type: protocol.ClientEndSession}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, ok := svc.runtime("sess-1")
		return !ok
	})
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if connector.openCount != 1 {
		t.Fatalf("SSH opens = %d, want 1", connector.openCount)
	}
}

func TestRejoinGraceClosesRuntime(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	svc := newTestService(sm, &fakeConnector{}, 10*time.Millisecond)
	owner, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	owner.Close()
	waitFor(t, func() bool {
		_, ok := svc.runtime("sess-1")
		return !ok
	})
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(sm.closedSessions) != 1 {
		t.Fatalf("closed sessions = %#v, want one", sm.closedSessions)
	}
}

type terminalHeartbeatError struct{}

func (terminalHeartbeatError) Error() string  { return "heartbeat rejected" }
func (terminalHeartbeatError) Terminal() bool { return true }

func TestTerminalHeartbeatFailureClosesRuntime(t *testing.T) {
	sm := &fakeSessionManager{session: testSession(), heartbeatErr: terminalHeartbeatError{}}
	svc := newTestService(sm, &fakeConnector{}, time.Second)
	if _, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, ok := svc.runtime("sess-1")
		return !ok
	})
}

func TestStartRegistersPodAndSendsHeartbeat(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	svc := newTestService(sm, &fakeConnector{}, time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		return len(sm.podHeartbeats) > 0
	})
}

func outputContains(t *testing.T, attachment *Attachment, expected string) bool {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case packet := <-attachment.Output():
			if packet.Type == protocol.Output && string(packet.Data) == expected+"\r\n" {
				return true
			}
		case <-deadline:
			return false
		}
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
