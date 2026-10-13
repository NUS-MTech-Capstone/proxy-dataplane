package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/model"
	"github.com/NUS-MTech-Capstone/access-proxy/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type signedIdentityFixture struct {
	verifier *ALBJWTVerifier
	key      *ecdsa.PrivateKey
}

func newSignedIdentityFixture(t *testing.T) signedIdentityFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fixture-key" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
	}))
	t.Cleanup(keys.Close)
	verifier, err := NewALBJWTVerifier(ALBJWTVerifierConfig{
		Region: "test", ALBARN: "fixture-alb", Issuer: "https://fixture-issuer", ClientID: "fixture-client",
		PublicKeyBaseURL: keys.URL, HTTPClient: keys.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return signedIdentityFixture{verifier, key}
}

func (f signedIdentityFixture) headers(t *testing.T, r *http.Request, mutate func(map[string]any, map[string]any)) {
	t.Helper()
	header := map[string]any{"alg": "ES256", "kid": "fixture-key", "signer": "fixture-alb", "iss": "https://fixture-issuer", "client": "fixture-client", "exp": time.Now().Add(time.Minute).Unix()}
	claims := map[string]any{"sub": "user-1", "email": "fixture@example.invalid"}
	if mutate != nil {
		mutate(header, claims)
	}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	digest := sha256.Sum256([]byte(unsigned))
	x, y, err := ecdsa.Sign(rand.Reader, f.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := append(x.FillBytes(make([]byte, 32)), y.FillBytes(make([]byte, 32))...)
	r.Header.Set("x-amzn-oidc-data", unsigned+"."+base64.RawURLEncoding.EncodeToString(signature))
	r.Header.Set("x-amzn-oidc-identity", "user-1")
}

func TestVerifiedALBIdentity(t *testing.T) {
	f := newSignedIdentityFixture(t)
	for _, name := range []string{"valid", "missing", "forged", "tampered", "missing_identity", "key_unavailable", "expired", "algorithm", "signer", "issuer", "client", "subject", "identity", "key_path"} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/sessions/sess-1/ws?token=valid-capability&apiVersion=1", nil)
			f.headers(t, r, func(h, c map[string]any) {
				switch name {
				case "expired":
					h["exp"] = time.Now().Add(-time.Minute).Unix()
				case "algorithm":
					h["alg"] = "none"
				case "signer":
					h["signer"] = "other-alb"
				case "issuer":
					h["iss"] = "other-issuer"
				case "client":
					h["client"] = "other-client"
				case "subject":
					delete(c, "sub")
				case "key_unavailable":
					h["kid"] = "missing-key"
				case "key_path":
					h["kid"] = "../fixture-key"
				}
			})
			switch name {
			case "missing":
				r.Header.Del("x-amzn-oidc-data")
			case "forged":
				r.Header.Set("x-amzn-oidc-data", "header.eyJzdWIiOiJ1c2VyLTEifQ.signature")
			case "tampered":
				parts := strings.Split(r.Header.Get("x-amzn-oidc-data"), ".")
				signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
				signature[0] ^= 0xff
				r.Header.Set("x-amzn-oidc-data", parts[0]+"."+parts[1]+"."+base64.RawURLEncoding.EncodeToString(signature))
			case "missing_identity":
				r.Header.Del("x-amzn-oidc-identity")
			case "identity":
				r.Header.Set("x-amzn-oidc-identity", "other-user")
			}
			user, err := cognitoUserFromALBHeaders(r, f.verifier)
			if name == "valid" {
				if err != nil || user.Subject != "user-1" {
					t.Fatalf("verified identity = %v, %v", user.Subject, err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid identity accepted")
			}
			// Nil service proves rejection precedes token/session/SSH work, even
			// when the caller provides an otherwise valid capability.
			rec := httptest.NewRecorder()
			New(nil, f.verifier).ServeHTTP(rec, r)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
}

func TestIdentityConfigurationFailsClosed(t *testing.T) {
	if _, err := NewALBJWTVerifier(ALBJWTVerifierConfig{}); err == nil {
		t.Fatal("missing trust configuration accepted")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := cognitoUserFromALBHeaders(r, nil); err == nil {
		t.Fatal("missing verifier accepted")
	}
}

func TestALBPublicKeyFetchHonorsCancellation(t *testing.T) {
	f := newSignedIdentityFixture(t)
	r := httptest.NewRequest("GET", "/", nil)
	f.headers(t, r, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.verifier.Verify(ctx, r.Header.Get("x-amzn-oidc-data")); err == nil {
		t.Fatal("cancelled key fetch accepted")
	}
}

func TestEachIdentityTrustSettingIsRequired(t *testing.T) {
	for _, field := range []string{"region", "alb", "issuer", "client"} {
		t.Run(field, func(t *testing.T) {
			cfg := ALBJWTVerifierConfig{Region: "test", ALBARN: "alb", Issuer: "issuer", ClientID: "client"}
			switch field {
			case "region":
				cfg.Region = ""
			case "alb":
				cfg.ALBARN = ""
			case "issuer":
				cfg.Issuer = ""
			case "client":
				cfg.ClientID = ""
			}
			if _, err := NewALBJWTVerifier(cfg); err == nil {
				t.Fatal("missing trust setting accepted")
			}
		})
	}
}

func TestVerifiedIdentityCannotOverrideCapabilityOrOwnerInstance(t *testing.T) {
	identity := newSignedIdentityFixture(t)
	for _, mismatch := range []string{"capability-user", "owner-instance"} {
		t.Run(mismatch, func(t *testing.T) {
			claims := model.SessionClaims{SessionID: "sess-1", UserID: "user-1", HostID: "host-1", CRID: "cr-1", ConnectionType: "ssh", Action: "start", Mode: "owner"}
			session := model.Session{SessionID: "sess-1", UserID: "user-1", HostID: "host-1", CRID: "cr-1", ConnectionType: "ssh", OwnerPodID: "pod-1", OwnerInstanceID: "instance-1", Status: model.SessionStatusActive}
			if mismatch == "capability-user" {
				claims.UserID = "other-user"
			} else {
				session.OwnerInstanceID = "stale-instance"
			}
			svc := service.New(automationSM{session: session}, automationSTS{sessionClaims: claims}, automationExecutor{}, model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1"}, time.Hour, time.Minute, 1024)
			request := httptest.NewRequest("GET", "/sessions/sess-1/ws?token=fixture-capability&apiVersion=1", nil)
			identity.headers(t, request, nil)
			rec := httptest.NewRecorder()
			New(svc, identity.verifier).ServeHTTP(rec, request)
			if rec.Code != http.StatusConflict {
				t.Fatalf("mismatch accepted or incorrectly classified: %d", rec.Code)
			}
			assertContractResponse(t, request.Method, request.URL.String(), rec)
		})
	}
}
