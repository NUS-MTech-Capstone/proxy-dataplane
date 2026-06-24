package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewareAndSetRequestHeaders(t *testing.T) {
	handler := RequestIDMiddleware(LogRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := RequestID(r.Context()); id != "req-1" {
			t.Fatalf("RequestID() = %q", id)
		}
		outbound := httptest.NewRequest(http.MethodGet, "http://example", nil)
		SetRequestHeaders(r.Context(), outbound)
		if outbound.Header.Get("X-Request-ID") != "req-1" {
			t.Fatalf("outbound X-Request-ID = %q", outbound.Header.Get("X-Request-ID"))
		}
		w.WriteHeader(http.StatusAccepted)
	})))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "req-1")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted || rec.Header().Get("X-Request-ID") != "req-1" {
		t.Fatalf("status/header = %d/%q", rec.Code, rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Fatal("RequestID() = empty")
		}
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
}
