package sts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateTokenSendsRequestAndReturnsClaims(t *testing.T) {
	client := NewSTS("http://sts.example")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/tokens/validate" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		if req.Header.Get("X-Request-ID") != "req-1" {
			t.Fatalf("X-Request-ID = %q", req.Header.Get("X-Request-ID"))
		}
		var body map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["sessionId"] != "sess-1" || body["userId"] != "user-1" || body["token"] != "token-1" {
			t.Fatalf("body = %#v", body)
		}
		return stsJSONResponse(http.StatusOK, `{"allowed":true,"claims":{"sessionId":"sess-1","userId":"user-1","hostId":"host-1","crId":"cr-1","connectionType":"ssh","action":"start","mode":"owner"}}`), nil
	})

	ctx := observability.ContextWithRequestID(t.Context(), "req-1")
	claims, err := client.ValidateToken(ctx, "sess-1", "user-1", "token-1")
	if err != nil {
		t.Fatal(err)
	}
	if claims.SessionID != "sess-1" || claims.Mode != "owner" {
		t.Fatalf("claims = %#v", claims)
	}
}

func TestValidateTokenRejectsDeniedMissingClaimsAndBadResponses(t *testing.T) {
	tests := []struct {
		name      string
		response  *http.Response
		transport error
		wantErr   string
	}{
		{name: "denied", response: stsJSONResponse(http.StatusOK, `{"allowed":false,"reason":"expired"}`), wantErr: "expired"},
		{name: "missing claims", response: stsJSONResponse(http.StatusOK, `{"allowed":true}`), wantErr: "did not include claims"},
		{name: "status", response: stsJSONResponse(http.StatusUnauthorized, `{"error":"bad"}`), wantErr: "status 401"},
		{name: "malformed json", response: stsJSONResponse(http.StatusOK, `{`), wantErr: "unexpected EOF"},
		{name: "transport", transport: errors.New("dial failed"), wantErr: "dial failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewSTS("http://sts.example")
			client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tt.transport != nil {
					return nil, tt.transport
				}
				return tt.response, nil
			})
			_, err := client.ValidateToken(t.Context(), "sess-1", "user-1", "token-1")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateToken() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAutomationTokenReturnsClaimsAndRejectsMissingClaims(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "success", body: `{"allowed":true,"claims":{"runId":"run-1","incidentId":"inc-1","hostId":"host-1","phase":"diagnose","allowedOperations":["health_check"]}}`},
		{name: "denied", body: `{"allowed":false,"reason":"revoked"}`, wantErr: "revoked"},
		{name: "missing claims", body: `{"allowed":true}`, wantErr: "automation token rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewSTS("http://sts.example")
			client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/tokens/automation/validate" {
					t.Fatalf("path = %s", req.URL.Path)
				}
				return stsJSONResponse(http.StatusOK, tt.body), nil
			})
			claims, err := client.ValidateAutomationToken(t.Context(), "run-1", "token-1")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAutomationToken() error = %v", err)
				}
				if claims.HostID != "host-1" || len(claims.AllowedOperations) != 1 {
					t.Fatalf("claims = %#v", claims)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateAutomationToken() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func stsJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
