package session_manager

import (
	"context"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
)

func (c *Client) RegisterPod(ctx context.Context, pod model.ProxyPod) error {
	return c.post(ctx, "/proxy-pods/register", pod, nil)
}

func (c *Client) HeartbeatPod(ctx context.Context, pod model.ProxyPod) error {
	return c.post(ctx, "/proxy-pods/heartbeat", pod, nil)
}
