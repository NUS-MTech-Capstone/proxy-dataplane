package sts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
)

func (c *Client) ValidateAutomationToken(ctx context.Context, runID, token string) (model.AutomationClaims, error) {
	body, err := json.Marshal(map[string]string{"runId": runID, "token": token})
	if err != nil {
		return model.AutomationClaims{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tokens/automation/validate", bytes.NewReader(body))
	if err != nil {
		return model.AutomationClaims{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	observability.SetRequestHeaders(ctx, req)
	resp, err := c.client.Do(req)
	if err != nil {
		return model.AutomationClaims{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.AutomationClaims{}, fmt.Errorf("sts returned status %d", resp.StatusCode)
	}
	var parsed struct {
		Allowed bool                    `json:"allowed"`
		Reason  string                  `json:"reason,omitempty"`
		Claims  *model.AutomationClaims `json:"claims,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return model.AutomationClaims{}, err
	}
	if !parsed.Allowed || parsed.Claims == nil {
		return model.AutomationClaims{}, fmt.Errorf("automation token rejected: %s", parsed.Reason)
	}
	return *parsed.Claims, nil
}
