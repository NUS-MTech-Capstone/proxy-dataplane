# Proxy Dataplane

Owns persistent SSH PTY sessions and attaches owner or observer WebSockets to
the assigned session.

## Run

Start Session Manager and STS first, then:

```bash
PORT=8083 \
SESSION_MANAGER_URL=http://localhost:8081 \
STS_URL=http://localhost:8082 \
SSH_KEY_PROVIDER=env \
SSH_PRIVATE_KEY_PEM="$(cat ~/.ssh/id_ed25519)" \
SSH_CONNECT_TIMEOUT=10s \
SSH_REJOIN_GRACE=5m \
PROXY_DATAPLANE_PUBLIC_URL=ws://localhost:8083 \
MAX_SESSIONS=50 \
go run ./cmd/server
```

On startup the process registers itself with Session Manager and sends pod
heartbeats every five seconds.

For deployment, use AWS Secrets Manager:

```bash
SSH_KEY_PROVIDER=aws
AWS_REGION=ap-southeast-1
```

The secret named by the signed `credentialSecretId` claim must contain the raw,
unencrypted PEM/OpenSSH private key. Keys are parsed in memory and are never
written to disk.

## Endpoints

```http
GET /healthz
GET /sessions/{sessionId}/ws
```

The signed STS token determines whether the connection starts, rejoins, or
observes a session. URL query parameters do not select authorization mode.
Observers are read-only.

The SSH shell and Session Manager heartbeat remain active for
`SSH_REJOIN_GRACE` after owner disconnect, allowing rejoin without losing shell
state. A bounded `SSH_SCROLLBACK_BYTES` buffer defaults to 256 KiB.

The WebSocket endpoint expects AWS ALB Cognito authentication. The ALB validates
the Cognito token and forwards the claims JWT in `x-amzn-oidc-data`. The
dataplane parses the `sub` claim and requires it to match the session's
`userId`. `GET /healthz` remains unauthenticated.

The WebSocket endpoint accepts the session token either as:

```text
Authorization: Bearer <token>
```

or as `?token=<token>` for simple local testing.

The session token must belong to the authenticated Cognito user. Its signed
claims must match Session Manager metadata and include the target SSH address,
username, credential secret ID, and pinned host public key.

For direct local testing without an ALB, also provide a simulated
`x-amzn-oidc-data` header containing a three-part JWT with a matching `sub`
claim. The dataplane intentionally does not verify this JWT because production
traffic must only reach it through the authenticated ALB.

## Terminal Protocol

WebSocket messages are binary. The first byte is the packet type:

```text
0x00 input
0x01 output
0x02 error
0x07 session event
0x10 resize: uint16 columns + uint16 rows, big endian
0x12 detach WebSocket
0x18 terminate session
```

The dataplane uses `golang.org/x/crypto/ssh`, requests an `xterm-256color` PTY,
and starts a persistent shell. Only one owner may write. Rejoin attaches to the
same shell, while observers receive scrollback and future output.
