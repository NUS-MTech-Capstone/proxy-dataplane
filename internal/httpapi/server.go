package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

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
	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /sessions/", s.ws)
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := parseSessionID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "session token is required")
		return
	}
	if err := s.svc.ValidateSession(r.Context(), sessionID, token); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		log.Printf("websocket upgrade failed sessionId=%s error=%v", sessionID, err)
		return
	}
	defer conn.Close()
	s.svc.StartSession(r.Context(), sessionID)
	defer s.svc.CloseSession(r.Context(), sessionID)
	log.Printf("SessionStarted sessionId=%s", sessionID)
	_ = conn.WriteText([]byte("connected to DenDenMushi mock session\r\n"))
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			if err != io.EOF {
				log.Printf("websocket read failed sessionId=%s error=%v", sessionID, err)
			}
			return
		}
		if err := conn.WriteText(append([]byte("echo: "), msg...)); err != nil {
			log.Printf("websocket write failed sessionId=%s error=%v", sessionID, err)
			return
		}
	}
}

func parseSessionID(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "sessions" || parts[2] != "ws" {
		return "", false
	}
	return parts[1], true
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
