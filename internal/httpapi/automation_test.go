package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
)

type automationSM struct {
	run     model.AutomationRun
	session model.Session
	err     error
}

func (f automationSM) RegisterPod(context.Context, model.ProxyPod) error  { return nil }
func (f automationSM) HeartbeatPod(context.Context, model.ProxyPod) error { return nil }
func (f automationSM) GetSession(context.Context, string) (model.Session, error) {
	return f.session, f.err
}
func (f automationSM) HeartbeatSession(context.Context, string, string, string) error { return nil }
func (f automationSM) CloseSession(context.Context, string, string, string) error     { return nil }
func (f automationSM) GetAutomationRun(context.Context, string) (model.AutomationRun, error) {
	return f.run, nil
}

type automationSTS struct {
	claims        model.AutomationClaims
	sessionClaims model.SessionClaims
	err           error
}

func (f automationSTS) ValidateToken(context.Context, string, string, string) (model.SessionClaims, error) {
	return f.sessionClaims, f.err
}
func (f automationSTS) ValidateAutomationToken(context.Context, string, string) (model.AutomationClaims, error) {
	return f.claims, f.err
}

type automationExecutor struct {
	err error
}

func (f automationExecutor) Open(context.Context, model.SessionClaims) (connection.Shell, error) {
	return nil, errors.New("unexpected shell open")
}
func (f automationExecutor) Execute(_ context.Context, _ model.AutomationClaims, command string, maxOutputBytes int) (model.OperationResult, error) {
	if f.err != nil {
		return model.OperationResult{}, f.err
	}
	if command != "sudo -- /usr/local/sbin/ddm-ops health_check" || maxOutputBytes != 2048 {
		return model.OperationResult{}, errors.New("unexpected executor inputs")
	}
	return model.OperationResult{OperationID: "health_check", Stdout: "ok", ExitCode: 0}, nil
}

func TestOperationEndpointValidationAndSuccess(t *testing.T) {
	server := New(newAutomationService(automationExecutor{}))
	tests := []struct {
		name   string
		path   string
		body   string
		auth   string
		status int
	}{
		{name: "not found", path: "/automation/runs/run-1/other", body: `{}`, auth: "Bearer token", status: http.StatusNotFound},
		{name: "missing token", path: "/automation/runs/run-1/operations", body: `{}`, status: http.StatusUnauthorized},
		{name: "bad json", path: "/automation/runs/run-1/operations", body: `{`, auth: "Bearer token", status: http.StatusBadRequest},
		{name: "missing operation", path: "/automation/runs/run-1/operations", body: `{}`, auth: "Bearer token", status: http.StatusBadRequest},
		{name: "success", path: "/automation/runs/run-1/operations", body: `{"operationId":"health_check"}`, auth: "Bearer token", status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", tt.auth)
			server.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.status, rec.Body.String())
			}
		})
	}
}

func TestOperationEndpointMapsServiceFailureToConflict(t *testing.T) {
	server := New(newAutomationService(automationExecutor{err: errors.New("boom")}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/automation/runs/run-1/operations", strings.NewReader(`{"operationId":"health_check"}`))
	req.Header.Set("Authorization", "Bearer token")

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestParseAutomationRunID(t *testing.T) {
	if runID, ok := parseAutomationRunID("/automation/runs/run-1/operations"); !ok || runID != "run-1" {
		t.Fatalf("parseAutomationRunID() = %q/%v", runID, ok)
	}
	if _, ok := parseAutomationRunID("/automation/runs//operations"); ok {
		t.Fatal("parseAutomationRunID() accepted empty run id")
	}
}

func newAutomationService(executor automationExecutor) *service.Service {
	run := model.AutomationRun{
		RunID: "run-1", IncidentID: "inc-1", HostID: "host-1",
		OwnerPodID: "pod-1", OwnerInstanceID: "instance-1", Status: "ACTIVE", ExpiresAt: time.Now().Add(time.Hour),
	}
	claims := model.AutomationClaims{
		RunID: "run-1", IncidentID: "inc-1", HostID: "host-1", Phase: "diagnose",
		AllowedOperations: []string{"health_check"}, OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
		HelperPath: "/usr/local/sbin/ddm-ops",
	}
	return service.New(
		automationSM{run: run}, automationSTS{claims: claims}, executor,
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour, time.Minute, 1024, executor, time.Second, 2048,
	)
}
