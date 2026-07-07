package httpapi

import "net/http"

const (
	apiVersionHeader    = "x-api-version"
	apiVersionQuery     = "apiVersion"
	supportedAPIVersion = "1"
)

func apiVersionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIVersionExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if hasSupportedAPIVersion(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusBadRequest, `missing or unsupported x-api-version, expected "1"`)
	})
}

func hasSupportedAPIVersion(r *http.Request) bool {
	if r.Header.Get(apiVersionHeader) == supportedAPIVersion {
		return true
	}
	_, isWebSocketPath := parseSessionID(r.URL.Path)
	return isWebSocketPath && r.URL.Query().Get(apiVersionQuery) == supportedAPIVersion
}

func isAPIVersionExempt(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/openapi.yaml", "/asyncapi.yaml":
		return true
	default:
		return false
	}
}
