package session_audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func NewSessionAudit(baseURL string) *Client {
	return &Client{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) AppendSessionAuditRecords(
	ctx context.Context,
	sessionID string,
	req model.AppendSessionAuditRecordsRequest,
) ([]model.SessionAuditRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sessions/"+sessionID+"/audit/records", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	observability.SetRequestHeaders(ctx, httpReq)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, ResponseError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var response model.AppendSessionAuditRecordsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	return response.Records, nil
}

type ResponseError struct {
	StatusCode int
	Body       string
}

func (e ResponseError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("audit-service returned status %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("audit-service returned status %d", e.StatusCode)
}

func (e ResponseError) Terminal() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}
