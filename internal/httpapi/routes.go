package httpapi

import (
	"net/http"

	"github.com/NUS-MTech-Capstone/access-proxy/api"
)

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /openapi.yaml", serveYAML(api.OpenAPI))
	s.mux.HandleFunc("GET /asyncapi.yaml", serveYAML(api.AsyncAPI))
	// Optional ALB identity headers are parsed in ws when present; STS token auth
	// is the fallback because WebSocket upgrades often miss x-amzn-oidc-data.
	s.mux.Handle("GET /sessions/", optionalCognitoMiddleware(http.HandlerFunc(s.ws)))
	s.mux.HandleFunc("POST /automation/runs/", s.operation)
}
