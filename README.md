# proxy-dataplane

## Key responsibilities

1. Reads from kafka topic
2. 
# Proxy Dataplane

Owns the live WebSocket connection for an assigned session. The MVP runtime is a
mock terminal echo session; real SSH/DDM Agent bridging can replace the echo
loop later.

## Run

Start Session Manager and STS first, then:

```bash
PORT=8083 \
SESSION_MANAGER_URL=http://localhost:8081 \
STS_URL=http://localhost:8082 \
PROXY_DATAPLANE_PUBLIC_URL=ws://localhost:8083 \
MAX_SESSIONS=50 \
go run ./cmd/server
```

On startup the process registers itself with Session Manager and sends pod
heartbeats every five seconds.

## Endpoints

```http
GET /healthz
GET /sessions/{sessionId}/ws
```

The WebSocket endpoint expects AWS ALB Cognito authentication. The ALB validates
the Cognito token and forwards the claims JWT in `x-amzn-oidc-data`. The
dataplane parses the `sub` claim and requires it to match the session's
`userId`. `GET /healthz` remains unauthenticated.

The WebSocket endpoint accepts the session token either as:

```text
Authorization: Bearer <token>
```

or as `?token=<token>` for simple local testing.

For direct local testing without an ALB, also provide a simulated
`x-amzn-oidc-data` header containing a three-part JWT with a matching `sub`
claim. The dataplane intentionally does not verify this JWT because production
traffic must only reach it through the authenticated ALB.
