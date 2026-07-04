package sts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

type validateResponse struct {
	Allowed bool                 `json:"allowed"`
	Reason  string               `json:"reason,omitempty"`
	Claims  *model.SessionClaims `json:"claims,omitempty"`
}

func (c *Client) ValidateToken(ctx context.Context, sessionID, userID, token string) (model.SessionClaims, error) {
	body, err := json.Marshal(map[string]string{
		"sessionId": sessionID,
		"userId":    userID,
		"token":     token,
	})
	if err != nil {
		return model.SessionClaims{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tokens/validate", bytes.NewReader(body))
	if err != nil {
		return model.SessionClaims{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	observability.SetRequestHeaders(ctx, req)
	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		recordSTSDependency(ctx, "token_validate", time.Since(start), err, 0)
		return model.SessionClaims{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("sts returned status %d", resp.StatusCode)
		recordSTSDependency(ctx, "token_validate", time.Since(start), err, resp.StatusCode)
		return model.SessionClaims{}, err
	}
	recordSTSDependency(ctx, "token_validate", time.Since(start), nil, resp.StatusCode)
	var parsed validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return model.SessionClaims{}, err
	}
	if !parsed.Allowed {
		return model.SessionClaims{}, fmt.Errorf("token rejected: %s", parsed.Reason)
	}
	if parsed.Claims == nil {
		return model.SessionClaims{}, fmt.Errorf("sts response did not include claims")
	}
	return *parsed.Claims, nil
}

func recordSTSDependency(ctx context.Context, operation string, duration time.Duration, err error, status int) {
	metrics := []observability.Metric{{
		Name:  "STSRequestLatencyMs",
		Unit:  "Milliseconds",
		Value: float64(duration.Milliseconds()),
	}}
	if err != nil {
		metrics = append(metrics, observability.Metric{Name: "STSErrors", Unit: "Count", Value: 1})
		slog.WarnContext(ctx, "STSUnavailable",
			"requestId", observability.RequestID(ctx),
			"operation", operation,
			"durationMs", duration.Milliseconds(),
			"errorKind", observability.ErrorKind(err),
			"status", status,
			"error", err,
		)
	}
	observability.EmitMetric("proxy-dataplane", metrics, map[string]string{"Component": "sts"})
}
