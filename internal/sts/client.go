package sts

import (
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func NewSTS(baseURL string) *Client {
	return &Client{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}
