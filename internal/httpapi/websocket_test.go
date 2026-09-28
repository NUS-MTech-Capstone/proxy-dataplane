package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
)

func TestWebSocketEndpointRejectsBadPathAndMissingToken(t *testing.T) {
	server := New(nil)
	tests := []struct {
		path      string
		useHeader bool
		status    int
	}{
		{path: "/sessions/sess-1/not-ws", useHeader: true, status: http.StatusNotFound},
		{path: "/sessions/sess-1/ws?apiVersion=1", status: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		if tt.useHeader {
			req.Header.Set(apiVersionHeader, supportedAPIVersion)
		}
		server.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Fatalf("%s status = %d, want %d", tt.path, rec.Code, tt.status)
		}
	}
}

func TestWebSocketOriginPolicy(t *testing.T) {
	server := New(nil, "https://api.ddm.keiyam.me", "http://localhost:4200/")
	tests := []struct {
		name   string
		origin string
		status int
	}{
		{name: "configured production origin", origin: "https://api.ddm.keiyam.me", status: http.StatusUnauthorized},
		{name: "normalized configured origin", origin: "http://localhost:4200", status: http.StatusUnauthorized},
		{name: "origin-less non-browser client", status: http.StatusUnauthorized},
		{name: "foreign origin", origin: "https://evil.example", status: http.StatusForbidden},
		{name: "malformed origin", origin: "://bad-origin", status: http.StatusForbidden},
		{name: "origin with path", origin: "https://api.ddm.keiyam.me/path", status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws?apiVersion=1", nil)
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			server.ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
		})
	}
}

func TestWebSocketRejectsOriginBeforeAuthentication(t *testing.T) {
	server := New(nil, "https://api.ddm.keiyam.me")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws?token=secret&apiVersion=1", nil)
	request.Header.Set("Origin", "null")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestParseSessionIDAndBearerToken(t *testing.T) {
	if id, ok := parseSessionID("/sessions/sess-1/ws"); !ok || id != "sess-1" {
		t.Fatalf("parseSessionID() = %q/%v", id, ok)
	}
	if _, ok := parseSessionID("/sessions/sess-1/other"); ok {
		t.Fatal("parseSessionID() accepted non-ws path")
	}
	if got := bearerToken("Bearer token-1"); got != "token-1" {
		t.Fatalf("bearerToken() = %q", got)
	}
	if got := bearerToken("Basic token-1"); got != "" {
		t.Fatalf("bearerToken() = %q, want empty", got)
	}
}

func TestWebSocketEndpointRejectsInvalidTokenDuringIdentityResolution(t *testing.T) {
	svc := service.New(
		automationSM{},
		automationSTS{err: errors.New("invalid token")},
		automationExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour, time.Minute, 1024,
	)
	server := New(svc)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws?token=bad&apiVersion=1", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWebSocketEndpointMapsAttachFailureToConflict(t *testing.T) {
	svc := service.New(
		automationSM{session: model.Session{
			SessionID: "sess-1", UserID: "user-1", HostID: "host-1", CRID: "cr-1",
			ConnectionType: "ssh", OwnerPodID: "other-pod", OwnerInstanceID: "instance-1", Status: model.SessionStatusActive,
		}},
		automationSTS{sessionClaims: model.SessionClaims{
			SessionID: "sess-1", UserID: "user-1", HostID: "host-1", CRID: "cr-1",
			ConnectionType: "ssh", Action: "start", Mode: "owner",
		}},
		automationExecutor{},
		model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"},
		time.Hour, time.Minute, 1024,
	)
	server := New(svc)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws?token=token&apiVersion=1", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}
