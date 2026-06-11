# Proxy Dataplane

Owns the live WebSocket connection for an assigned session. The MVP runtime is a
line-oriented terminal that executes commands through the DDM Agent installed
on the target server.

## Run

Start Session Manager and STS first, then:

```bash
PORT=8083 \
SESSION_MANAGER_URL=http://localhost:8081 \
STS_URL=http://localhost:8082 \
DDM_AGENT_URL_TEMPLATE=http://localhost:8084 \
COMMAND_TIMEOUT=30s \
PROXY_DATAPLANE_PUBLIC_URL=ws://localhost:8083 \
MAX_SESSIONS=50 \
go run ./cmd/server
```

On startup the process registers itself with Session Manager and sends pod
heartbeats every five seconds.

`DDM_AGENT_URL_TEMPLATE` may contain `{hostId}` when session host IDs are
DNS-resolvable:

```bash
DDM_AGENT_URL_TEMPLATE=http://{hostId}:8084
```

If `hostId` is an opaque inventory ID, add a trusted inventory resolver rather
than accepting a target address from the WebSocket client.

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

## Command Runtime

The WebSocket accepts terminal keystrokes, buffers one line, and calls:

```http
POST {DDM_AGENT_URL}/execute
Authorization: Bearer <short-lived-session-token>
Content-Type: application/json
X-DDM-Session-ID: sess-123
X-DDM-User-ID: user-123
X-DDM-Host-ID: host-123
X-DDM-CR-ID: cr-123

{"command":"whoami"}
```

The DDM Agent validates the forwarded session token directly with STS before
executing the command. Use TLS and private network reachability between the
dataplane, agent, and STS outside local development.

Only one writer may be connected to a session. The dataplane heartbeats the
session while connected and closes the Session Manager record on disconnect.

This MVP command endpoint is not a persistent PTY: shell state such as `cd`,
environment changes, full-screen programs, and terminal resize are not
preserved. The connector boundary is intended to support a streaming DDM Agent
PTY transport later. Direct SSH is not enabled until target inventory and
credential-source contracts are defined.
