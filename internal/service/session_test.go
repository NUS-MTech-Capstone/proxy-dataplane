package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
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
	automationRun     model.AutomationRun
	registered        []model.ProxyPod
	podHeartbeats     []model.ProxyPod
	sessionHeartbeats []string
	closedSessions    []string
	auditRecords      []model.AppendSessionAuditRecord
	auditErr          error
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
func (f *fakeSessionManager) AppendSessionAuditRecords(_ context.Context, _ string, req model.AppendSessionAuditRecordsRequest) ([]model.SessionAuditRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auditRecords = append(f.auditRecords, req.Records...)
	if f.auditErr != nil {
		return nil, f.auditErr
	}
	records := make([]model.SessionAuditRecord, 0, len(req.Records))
	for i, record := range req.Records {
		records = append(records, model.SessionAuditRecord{
			RecordType:       record.RecordType,
			CommandID:        record.CommandID,
			CommandText:      record.CommandText,
			OutputDataBase64: record.OutputDataBase64,
			Sequence:         int64(i + 1),
			Truncated:        record.Truncated,
			ByteCount:        record.ByteCount,
		})
	}
	return records, nil
}
func (f *fakeSessionManager) GetAutomationRun(context.Context, string) (model.AutomationRun, error) {
	return f.automationRun, nil
}

type fakeTokenValidator struct {
	claims           map[string]model.SessionClaims
	automationClaims map[string]model.AutomationClaims
}

func (f fakeTokenValidator) ValidateAutomationToken(_ context.Context, _, token string) (model.AutomationClaims, error) {
	claims, ok := f.automationClaims[token]
	if !ok {
		return model.AutomationClaims{}, errors.New("invalid automation token")
	}
	return claims, nil
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
	commands  []string
}

func (f *fakeConnector) Execute(_ context.Context, _ model.AutomationClaims, command string, _ int) (model.OperationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	return model.OperationResult{Stdout: "ok"}, nil
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

func TestExecuteOperationRequiresAssignedInstanceAndPlanHash(t *testing.T) {
	run := model.AutomationRun{
		RunID: "run-1", IncidentID: "inc-1", HostID: "host-1",
		OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
		Status: "ACTIVE", ExpiresAt: time.Now().Add(time.Hour),
	}
	sm := &fakeSessionManager{automationRun: run}
	validator := fakeTokenValidator{automationClaims: map[string]model.AutomationClaims{
		"token": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "remediate",
			AllowedOperations: []string{"restart_service"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"wrong-host": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-2", Phase: "remediate",
			AllowedOperations: []string{"restart_service"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"wrong-phase": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "diagnose",
			AllowedOperations: []string{"restart_service"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"unapproved": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "remediate",
			AllowedOperations: []string{"health_check"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"reload": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "remediate",
			AllowedOperations: []string{"reload_service"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"target-probe-diagnose": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "diagnose",
			AllowedOperations: []string{"target_probe"},
			OwnerPodID:        "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"target-probe-verify": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "verify",
			AllowedOperations: []string{"target_probe"},
			OwnerPodID:        "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
		"target-probe-remediate": {
			RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "remediate",
			AllowedOperations: []string{"target_probe"}, PlanHash: "hash",
			OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
			HelperPath: "/usr/local/sbin/ddm-ops",
		},
	}}
	connector := &fakeConnector{}
	svc := New(
		sm, validator, connector,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Second, time.Minute, 1024, time.Second, 1024,
	)
	result, err := svc.ExecuteOperation(t.Context(), "run-1", "token", model.OperationRequest{
		OperationID: "restart_service", PlanHash: "hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OperationID != "restart_service" {
		t.Fatalf("result = %#v", result)
	}
	connector.mu.Lock()
	command := connector.commands[0]
	connector.mu.Unlock()
	if command != "sudo -- /usr/local/sbin/ddm-ops restart_service" {
		t.Fatalf("command = %q", command)
	}
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "reload", model.OperationRequest{
		OperationID: "reload_service", PlanHash: "hash",
	}); err != nil {
		t.Fatal(err)
	}
	connector.mu.Lock()
	command = connector.commands[len(connector.commands)-1]
	connector.mu.Unlock()
	if command != "sudo -- /usr/local/sbin/ddm-ops reload_service" {
		t.Fatalf("command = %q", command)
	}
	for _, token := range []string{"target-probe-diagnose", "target-probe-verify"} {
		if _, err := svc.ExecuteOperation(t.Context(), "run-1", token, model.OperationRequest{
			OperationID: "target_probe",
		}); err != nil {
			t.Fatalf("target_probe rejected for token %q: %v", token, err)
		}
	}
	connector.mu.Lock()
	command = connector.commands[len(connector.commands)-1]
	connector.mu.Unlock()
	if command != "sudo -- /usr/local/sbin/ddm-ops target_probe" {
		t.Fatalf("command = %q", command)
	}
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "target-probe-remediate", model.OperationRequest{
		OperationID: "target_probe", PlanHash: "hash",
	}); err == nil {
		t.Fatal("target_probe was accepted as a remediation operation")
	}
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "token", model.OperationRequest{
		OperationID: "restart_service", PlanHash: "changed",
	}); err == nil {
		t.Fatal("changed plan hash was accepted")
	}
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "token", model.OperationRequest{
		OperationID: "restart_service", PlanHash: "hash",
		Parameters: map[string]string{"service": "other"},
	}); err == nil {
		t.Fatal("parameters outside the trusted inventory profile were accepted")
	}
	for _, token := range []string{"wrong-host", "wrong-phase", "unapproved"} {
		if _, err := svc.ExecuteOperation(t.Context(), "run-1", token, model.OperationRequest{
			OperationID: "restart_service", PlanHash: "hash",
		}); err == nil {
			t.Fatalf("operation accepted token %q", token)
		}
	}
	sm.automationRun.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "token", model.OperationRequest{
		OperationID: "restart_service", PlanHash: "hash",
	}); err == nil {
		t.Fatal("operation accepted an expired automation run")
	}
	sm.automationRun = run
	sm.automationRun.OwnerInstanceID = "stale-instance"
	if _, err := svc.ExecuteOperation(t.Context(), "run-1", "token", model.OperationRequest{
		OperationID: "restart_service", PlanHash: "hash",
	}); err == nil {
		t.Fatal("operation accepted a stale dataplane instance")
	}
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

func TestResolveSessionUserIDPrefersCognitoSubject(t *testing.T) {
	svc := newTestService(&fakeSessionManager{}, &fakeConnector{}, time.Second)
	userID, err := svc.ResolveSessionUserID(t.Context(), "sess-1", "start", "alb-user")
	if err != nil {
		t.Fatalf("ResolveSessionUserID() error = %v", err)
	}
	if userID != "alb-user" {
		t.Fatalf("userID = %q, want alb-user", userID)
	}
}

func TestResolveSessionUserIDFallsBackToSessionToken(t *testing.T) {
	svc := newTestService(&fakeSessionManager{}, &fakeConnector{}, time.Second)
	userID, err := svc.ResolveSessionUserID(t.Context(), "sess-1", "start", "")
	if err != nil {
		t.Fatalf("ResolveSessionUserID() error = %v", err)
	}
	if userID != "owner-1" {
		t.Fatalf("userID = %q, want owner-1", userID)
	}
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
	if err := observer.Handle(protocol.Packet{Type: protocol.Resize, Data: []byte{0, 100, 0, 30}}); err != nil {
		t.Fatalf("observer resize error = %v, want nil", err)
	}
	gotScrollback, gotSnapshot := outputAndEventSnapshotContains(t, rejoined, "persistent output", []string{"observer-1"})
	if !gotScrollback {
		t.Fatal("rejoined owner did not receive scrollback")
	}
	if !gotSnapshot {
		t.Fatal("rejoined owner did not receive observer attachment snapshot")
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

func TestAuditRecorderCapturesOwnerCommandsAndOutput(t *testing.T) {
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

	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("pwdd\x7f\n")}); err != nil {
		t.Fatal(err)
	}
	if _, err := connector.shell.stdoutW.Write([]byte("/home/ubuntu\r\n")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		hasCommand := false
		hasOutput := false
		for _, record := range sm.auditRecords {
			if record.RecordType == model.AuditRecordCommand && record.CommandText == "pwd" {
				hasCommand = true
			}
			if record.RecordType == model.AuditRecordOutput {
				data, _ := base64.StdEncoding.DecodeString(record.OutputDataBase64)
				hasOutput = strings.Contains(string(data), "/home/ubuntu")
			}
		}
		return hasCommand && hasOutput
	})
}

func TestAuditRecorderStripsTerminalEchoFromRetainedOutput(t *testing.T) {
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

	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("ls -lrta")}); err != nil {
		t.Fatal(err)
	}
	if _, err := connector.shell.stdoutW.Write([]byte("ls -lrta")); err != nil {
		t.Fatal(err)
	}
	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("\r")}); err != nil {
		t.Fatal(err)
	}
	if _, err := connector.shell.stdoutW.Write([]byte("\r\ntotal 0\r\n")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		hasCommand := false
		hasResultOutput := false
		for _, record := range sm.auditRecords {
			if record.RecordType == model.AuditRecordCommand && record.CommandText == "ls -lrta" {
				hasCommand = true
			}
			if record.RecordType != model.AuditRecordOutput {
				continue
			}
			data, _ := base64.StdEncoding.DecodeString(record.OutputDataBase64)
			if strings.Contains(string(data), "ls -lrta") {
				t.Fatalf("terminal command echo was retained as output: %#v", sm.auditRecords)
			}
			if record.CommandID != "" && strings.Contains(string(data), "total 0") {
				hasResultOutput = true
			}
		}
		return hasCommand && hasResultOutput
	})
}

func TestAuditRecorderSplitsCRLFAndPastedCommands(t *testing.T) {
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

	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("ls -lrta\r\necho \"test\"\r\n")}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		seen := make(map[string]bool)
		for _, record := range sm.auditRecords {
			if record.RecordType == model.AuditRecordCommand {
				seen[record.CommandText] = true
			}
		}
		return seen["ls -lrta"] && seen[`echo "test"`]
	})
}

func TestAuditRecorderHandlesControlKeysAndObserverDoesNotCaptureInput(t *testing.T) {
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
	observer, err := svc.Attach(t.Context(), "sess-1", "observe", "observer-1")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()

	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("echo secret\x17safe\n")}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Handle(protocol.Packet{Type: protocol.Input, Data: []byte{0x03}}); err != nil {
		t.Fatal(err)
	}
	if err := observer.Handle(protocol.Packet{Type: protocol.Input, Data: []byte("whoami\n")}); err == nil {
		t.Fatal("observer input was accepted")
	}

	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		hasCommand := false
		hasControl := false
		for _, record := range sm.auditRecords {
			if record.RecordType == model.AuditRecordCommand && record.CommandText == "echo safe" {
				hasCommand = true
			}
			if record.RecordType == model.AuditRecordControl && record.CommandID != "" && record.CommandText == "Ctrl-C" {
				hasControl = true
			}
			if record.CommandText == "whoami" {
				t.Fatalf("observer command was audited: %#v", sm.auditRecords)
			}
		}
		return hasCommand && hasControl
	})
}

func TestAuditRecorderCapturesSessionLifecycleEvents(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	svc := newTestService(sm, &fakeConnector{}, time.Second)
	owner, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if runtime, ok := svc.runtime("sess-1"); ok {
			runtime.Close(nil)
		}
	}()
	observer, err := svc.Attach(t.Context(), "sess-1", "observe", "observer-1")
	if err != nil {
		t.Fatal(err)
	}
	observer.Close()
	owner.Close()
	rejoined, err := svc.Attach(t.Context(), "sess-1", "rejoin", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	rejoined.Close()

	waitFor(t, func() bool {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		seen := make(map[string]bool)
		for _, record := range sm.auditRecords {
			if record.RecordType == model.AuditRecordSessionEvent {
				seen[record.CommandText] = true
			}
		}
		return seen["session started"] &&
			seen["observer joined"] &&
			seen["observer detached"] &&
			seen["owner detached"] &&
			seen["owner rejoined"]
	})
}

func TestObserverAttachmentSnapshotUpdatesOnDetach(t *testing.T) {
	sm := &fakeSessionManager{session: testSession()}
	svc := newTestService(sm, &fakeConnector{}, time.Second)
	owner, err := svc.Attach(t.Context(), "sess-1", "start", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if runtime, ok := svc.runtime("sess-1"); ok {
			runtime.Close(nil)
		}
	}()
	observer, err := svc.Attach(t.Context(), "sess-1", "observe", "observer-1")
	if err != nil {
		t.Fatal(err)
	}
	if !eventSnapshotContains(t, owner, []string{"observer-1"}) {
		t.Fatal("owner did not receive observer attachment snapshot")
	}
	observer.Close()
	if !eventSnapshotContains(t, owner, nil) {
		t.Fatal("owner did not receive observer detach snapshot")
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

func eventSnapshotContains(t *testing.T, attachment *Attachment, expected []string) bool {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case packet := <-attachment.Output():
			if packet.Type != protocol.SessionEvent {
				continue
			}
			var event struct {
				Type      string   `json:"type"`
				Observers []string `json:"observers"`
			}
			if err := json.Unmarshal(packet.Data, &event); err != nil || event.Type != "attachments" {
				continue
			}
			if slices.Equal(event.Observers, expected) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func outputAndEventSnapshotContains(t *testing.T, attachment *Attachment, expectedOutput string, expectedObservers []string) (bool, bool) {
	t.Helper()
	deadline := time.After(time.Second)
	gotOutput := false
	gotSnapshot := false
	for {
		select {
		case packet := <-attachment.Output():
			if packet.Type == protocol.Output && string(packet.Data) == expectedOutput+"\r\n" {
				gotOutput = true
			}
			if packet.Type == protocol.SessionEvent {
				var event struct {
					Type      string   `json:"type"`
					Observers []string `json:"observers"`
				}
				if err := json.Unmarshal(packet.Data, &event); err == nil && event.Type == "attachments" && slices.Equal(event.Observers, expectedObservers) {
					gotSnapshot = true
				}
			}
			if gotOutput && gotSnapshot {
				return true, true
			}
		case <-deadline:
			return gotOutput, gotSnapshot
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
