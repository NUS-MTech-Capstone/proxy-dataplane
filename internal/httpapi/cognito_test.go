package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCognitoMiddlewareAddsUserToContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-amzn-oidc-data", testJWT(`{"sub":"user-123","email":"user@example.com"}`))
	rec := httptest.NewRecorder()

	handler := cognitoMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := cognitoUserFromContext(r.Context())
		if !ok {
			t.Fatal("Cognito user missing from context")
		}
		if user.Subject != "user-123" {
			t.Fatalf("Subject = %q, want user-123", user.Subject)
		}
		if user.Email != "user@example.com" {
			t.Fatalf("Email = %q, want user@example.com", user.Email)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestCognitoMiddlewareRejectsMissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	cognitoMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCognitoMiddlewareRejectsMissingSubject(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-amzn-oidc-data", testJWT(`{"email":"user@example.com"}`))
	rec := httptest.NewRecorder()

	cognitoMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not be called")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func testJWT(claims string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	return "header." + payload + ".signature"
}
