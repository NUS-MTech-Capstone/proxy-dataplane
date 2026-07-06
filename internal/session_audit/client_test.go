package session_audit

import (
	"encoding/json"
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

func TestAppendSessionAuditRecordsSendsOwnerAndRecords(t *testing.T) {
	client := NewSessionAudit("http://audit-service.example")
	client.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/sessions/sess-1/audit/records" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		if req.Header.Get("X-Request-ID") != "req-1" {
			t.Fatalf("X-Request-ID = %q", req.Header.Get("X-Request-ID"))
		}
		var body model.AppendSessionAuditRecordsRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.OwnerPodID != "pod-1" || body.OwnerInstanceID != "instance-1" || len(body.Records) != 1 {
			t.Fatalf("body = %#v", body)
		}
		return auditJSONResponse(http.StatusCreated, `{"records":[{"sequence":1,"recordType":"command","commandText":"pwd"}]}`), nil
	})

	ctx := observability.ContextWithRequestID(t.Context(), "req-1")
	records, err := client.AppendSessionAuditRecords(ctx, "sess-1", model.AppendSessionAuditRecordsRequest{
		OwnerPodID: "pod-1", OwnerInstanceID: "instance-1",
		Records: []model.AppendSessionAuditRecord{{RecordType: model.AuditRecordCommand, CommandText: "pwd"}},
	})
	if err != nil {
		t.Fatalf("AppendSessionAuditRecords() error = %v", err)
	}
	if len(records) != 1 || records[0].Sequence != 1 {
		t.Fatalf("records = %#v", records)
	}
}

func TestAppendSessionAuditRecordsClassifiesStatus(t *testing.T) {
	client := NewSessionAudit("http://audit-service.example")
	client.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return auditJSONResponse(http.StatusBadRequest, `{"error":"owner mismatch"}`), nil
	})
	err := func() error {
		_, err := client.AppendSessionAuditRecords(t.Context(), "sess-1", model.AppendSessionAuditRecordsRequest{})
		return err
	}()
	var responseErr ResponseError
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("AppendSessionAuditRecords() error = %v", err)
	}
	if !strings.Contains(err.Error(), "audit-service") {
		t.Fatalf("AppendSessionAuditRecords() error = %v, want audit-service source", err)
	}
	if !asResponseError(err, &responseErr) || !responseErr.Terminal() {
		t.Fatalf("AppendSessionAuditRecords() error = %v, want terminal ResponseError", err)
	}
}

func auditJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func asResponseError(err error, target *ResponseError) bool {
	if value, ok := err.(ResponseError); ok {
		*target = value
		return true
	}
	return false
}
