package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
)

type Server struct {
	svc                     *service.Service
	identityVerifier        IdentityVerifier
	mux                     *http.ServeMux
	webSocketAllowedOrigins map[string]struct{}
}

func New(svc *service.Service, verifier IdentityVerifier, webSocketAllowedOrigins ...string) *Server {
	s := &Server{
		svc:                     svc,
		identityVerifier:        verifier,
		mux:                     http.NewServeMux(),
		webSocketAllowedOrigins: make(map[string]struct{}, len(webSocketAllowedOrigins)),
	}
	for _, origin := range webSocketAllowedOrigins {
		if normalized, ok := normalizeWebSocketOrigin(origin); ok {
			s.webSocketAllowedOrigins[normalized] = struct{}{}
		}
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	securityHeadersMiddleware(observability.RequestIDMiddleware(observability.LogRequests(apiVersionMiddleware(s.mux)))).ServeHTTP(w, r)
}

func (s *Server) webSocketOriginAllowed(origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return true
	}
	normalized, ok := normalizeWebSocketOrigin(origin)
	if !ok {
		return false
	}
	_, ok = s.webSocketAllowedOrigins[normalized]
	return ok
}

func normalizeWebSocketOrigin(origin string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(parsed.Host), true
}
