package session_manager

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRegisterPodSendsJSONAndRequestID(t *testing.T) {
	client := NewSessionManager("http://session-manager.example")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/proxy-pods/register" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		if req.Header.Get("X-Request-ID") != "req-1" {
			t.Fatalf("X-Request-ID = %q", req.Header.Get("X-Request-ID"))
		}
		var pod model.ProxyPod
		if err := json.NewDecoder(req.Body).Decode(&pod); err != nil {
			t.Fatal(err)
		}
		if pod.PodID != "pod-1" || pod.InstanceID != "instance-1" || pod.WebsocketURL == "" {
			t.Fatalf("pod = %#v", pod)
		}
		return dpJSONResponse(http.StatusCreated, ``), nil
	})

	ctx := observability.ContextWithRequestID(t.Context(), "req-1")
	err := client.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-1", InstanceID: "instance-1", WebsocketURL: "wss://proxy", MaxSessions: 50,
	})
	if err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
}

func TestGetSessionHandlesSuccessStatusAndMalformedJSON(t *testing.T) {
	tests := []struct {
		name      string
		response  *http.Response
		wantErr   string
		wantOwner string
	}{
		{
			name:      "success",
			response:  dpJSONResponse(http.StatusOK, `{"sessionId":"sess-1","ownerPodId":"pod-1","ownerInstanceId":"instance-1","status":"ACTIVE"}`),
			wantOwner: "instance-1",
		},
		{
			name:     "status",
			response: dpJSONResponse(http.StatusNotFound, `{"error":"missing"}`),
			wantErr:  "status 404",
		},
		{
			name:     "malformed json",
			response: dpJSONResponse(http.StatusOK, `{`),
			wantErr:  "unexpected EOF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewSessionManager("http://session-manager.example")
			client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/sessions/sess-1" {
					t.Fatalf("request = %s %s", req.Method, req.URL.Path)
				}
				return tt.response, nil
			})
			session, err := client.GetSession(t.Context(), "sess-1")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("GetSession() error = %v", err)
				}
				if session.OwnerInstanceID != tt.wantOwner {
					t.Fatalf("session = %#v", session)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("GetSession() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPostClassifiesTerminalResponseErrorAndTransportError(t *testing.T) {
	client := NewSessionManager("http://session-manager.example")
	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return dpJSONResponse(http.StatusConflict, `{"error":"closed"}`), nil
	})
	err := client.CloseSession(t.Context(), "sess-1", "pod-1", "instance-1")
	var responseErr ResponseError
	if !errors.As(err, &responseErr) || !responseErr.Terminal() {
		t.Fatalf("CloseSession() error = %v, want terminal ResponseError", err)
	}

	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})
	err = client.HeartbeatSession(t.Context(), "sess-1", "pod-1", "instance-1")
	if err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("HeartbeatSession() error = %v, want transport error", err)
	}
}

func TestHeartbeatPodAndGetAutomationRun(t *testing.T) {
	client := NewSessionManager("http://session-manager.example")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/proxy-pods/heartbeat":
			return dpJSONResponse(http.StatusOK, ``), nil
		case "/automation/runs/run-1":
			return dpJSONResponse(http.StatusOK, `{"runId":"run-1","incidentId":"inc-1","hostId":"host-1","status":"ACTIVE"}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})
	if err := client.HeartbeatPod(t.Context(), model.ProxyPod{PodID: "pod-1"}); err != nil {
		t.Fatalf("HeartbeatPod() error = %v", err)
	}
	run, err := client.GetAutomationRun(t.Context(), "run-1")
	if err != nil {
		t.Fatalf("GetAutomationRun() error = %v", err)
	}
	if run.RunID != "run-1" {
		t.Fatalf("run = %#v", run)
	}
}

func dpJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
