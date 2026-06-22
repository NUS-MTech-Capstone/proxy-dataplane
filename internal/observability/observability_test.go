package observability

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hijackRecorder struct {
	http.ResponseWriter
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	server, client := net.Pipe()
	_ = client
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}

func TestLogRequestsPreservesHijacker(t *testing.T) {
	inner := &hijackRecorder{ResponseWriter: httptest.NewRecorder()}
	handler := LogRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Hijacker")
		}
		if _, _, err := hijacker.Hijack(); err != nil {
			t.Fatalf("Hijack() error = %v", err)
		}
	}))

	handler.ServeHTTP(inner, httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws", nil))

	if !inner.hijacked {
		t.Fatal("expected underlying connection to be hijacked")
	}
}
