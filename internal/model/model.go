package model

import "time"

type Session struct {
	SessionID       string    `json:"sessionId"`
	UserID          string    `json:"userId"`
	HostID          string    `json:"hostId"`
	CRID            string    `json:"crId"`
	OwnerPodID      string    `json:"ownerPodId"`
	OwnerInstanceID string    `json:"ownerInstanceId"`
	WebsocketURL    string    `json:"websocketUrl"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
	LeaseExpiresAt  time.Time `json:"leaseExpiresAt"`
}

type ProxyPod struct {
	PodID           string    `json:"podId"`
	InstanceID      string    `json:"instanceId"`
	PodIP           string    `json:"podIp"`
	WebsocketURL    string    `json:"websocketUrl"`
	Status          string    `json:"status"`
	ActiveSessions  int       `json:"activeSessions"`
	MaxSessions     int       `json:"maxSessions"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt,omitempty"`
}
