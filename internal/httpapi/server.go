package httpapi

import (
	"net/http"

	"github.com/NUS-MTech-Capstone/access-proxy/internal/observability"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
)

type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	observability.RequestIDMiddleware(observability.LogRequests(apiVersionMiddleware(s.mux))).ServeHTTP(w, r)
}
