package connect

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/connection"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAgentExecute(t *testing.T) {
	agent := NewAgent("http://agent.example:8084", time.Second)
	agent.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/execute" {
			t.Fatalf("request = %s %s, want POST /execute", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-DDM-Host-ID"); got != "host-123" {
			t.Fatalf("X-DDM-Host-ID = %q, want host-123", got)
		}
		if got := r.Header.Get("X-DDM-Session-ID"); got != "sess-1" {
			t.Fatalf("X-DDM-Session-ID = %q, want sess-1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			t.Fatalf("Authorization = %q, want bearer session token", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["command"] != "whoami" {
			t.Fatalf("command = %q, want whoami", body["command"])
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"stdout":"kei\n","stderr":"","exitCode":0}`)),
		}, nil
	})

	result, err := agent.Execute(t.Context(), connection.Command{
		SessionID: "sess-1",
		UserID:    "user-1",
		HostID:    "host-123",
		CRID:      "cr-1",
		Token:     "session-token",
		Value:     "whoami",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "kei\n" || result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentRejectsUnsafeTemplatedHostID(t *testing.T) {
	agent := NewAgent("http://{hostId}:8084", time.Second)
	if _, err := agent.endpoint("host@example.com"); err == nil {
		t.Fatal("endpoint() error = nil, want unsafe hostId rejection")
	}
}
