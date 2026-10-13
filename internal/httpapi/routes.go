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
	// ws checks the origin before requiring cryptographically verified ALB identity.
	s.mux.HandleFunc("GET /sessions/", s.ws)
	s.mux.HandleFunc("POST /automation/runs/", s.operation)
}
