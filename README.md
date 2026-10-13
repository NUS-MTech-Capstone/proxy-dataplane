# Proxy Dataplane

Owns persistent SSH PTY sessions and attaches owner or observer WebSockets to
the assigned session.

## Run

Start Session Manager and STS first, then:

```bash
PORT=8083 \
SESSION_MANAGER_URL=http://localhost:8081 \
SESSION_AUDIT_URL=http://localhost:8085 \
STS_URL=http://localhost:8082 \
SSH_KEY_PROVIDER=env \
SSH_PRIVATE_KEY_PEM="$(cat ~/.ssh/id_ed25519)" \
SSH_CONNECT_TIMEOUT=10s \
SSH_REJOIN_GRACE=5m \
PROXY_DATAPLANE_PUBLIC_URL=ws://localhost:8083 \
WEBSOCKET_ALLOWED_ORIGINS=http://localhost:4200 \
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

## Design Patterns

Proxy Dataplane uses a Strategy pattern for SSH key lookup through
`connect.CredentialProvider`. `EnvCredentials` supports local runs, while
`SecretsManagerCredentials` supports AWS deployments. Startup uses the
`connect.NewCredentialProvider` Factory to select the configured strategy from
`SSH_KEY_PROVIDER`.

## Endpoints

```http
GET /healthz
GET /sessions/{sessionId}/ws
POST /automation/runs/{runId}/operations
```

The automation endpoint is reachable through the assigned pod's internal
`operationsUrl`. It validates a dedicated STS capability plus Session Manager
run ownership, then maps an operation ID to:

```text
sudo -- /usr/local/sbin/ddm-ops <operationId>
```

It never accepts command text. Read-only operations require diagnose or verify
capabilities. Remediation operations from the code-reviewed v1 catalog require
a remediation capability and an exact approved plan-hash match.

`target_probe` is a read-only operation. It can run only with diagnose or
verify automation capability and maps to the same fixed `ddm-ops` helper path
as the other catalog operations.

For Phase 2 AI-agent workflows, Proxy Dataplane remains the owner of live SSH
PTY sessions and the only component on the terminal data path. The automation
endpoint is separate from WebSocket terminal handling and accepts only
allowlisted operation IDs backed by STS capability validation.

The signed STS token determines whether the connection starts, rejoins, or
observes a session. URL query parameters do not select authorization mode.
Observers are read-only.

The SSH shell and Session Manager heartbeat remain active for
`SSH_REJOIN_GRACE` after owner disconnect, allowing rejoin without losing shell
state. A bounded `SSH_SCROLLBACK_BYTES` buffer defaults to 256 KiB.

Proxy Dataplane also captures retained command/output audit records from the
PTY owner path and appends them asynchronously to Audit Service. Command
capture is best-effort because the SSH session is a raw PTY: printable owner
input is buffered until Enter, basic edit controls such as Backspace, Ctrl-U,
Ctrl-W, and Ctrl-C are interpreted, and observer input is never audited because
observers cannot write. PTY stdout/stderr chunks are stored as output records
linked to the most recent submitted command when possible. Matched interactive
terminal echo of owner input is treated as command echo and is not retained as
output.
Session lifecycle events such as `session started`, `owner detached`,
`owner rejoined`, `observer joined`, and `observer detached` are retained as
session audit records alongside commands and output.

Audit appends are batched with a bounded queue and timeout. Append failures or
Audit Service outages emit metadata-only logs/metrics and do not break live
terminal I/O. Command text and output bytes must not be logged to CloudWatch or
normal application logs. When Audit Service reports a truncated audit record,
the dataplane stops sending further audit output for that session while the SSH
session continues.

Terminal upgrades require both a cryptographically verified ALB identity and an
STS session capability. The verifier checks the ES256 signature against the
regional ALB public key, expiry, signer ALB ARN, Cognito issuer and client ID.
The signed `sub` must equal `x-amzn-oidc-identity`, the STS user and session
ownership metadata. Missing or invalid identity returns 401 before session or
SSH work; an STS token alone is insufficient.

Startup requires `AWS_REGION`, `ALB_ARN`, `COGNITO_CLIENT_ID`, and either
`COGNITO_ISSUER` or `COGNITO_USER_POOL_ID` (the issuer is derived from the pool
and region). Deploy these settings before promoting the strict identity image.
Check active sessions and allow them to finish before the Recreate rollout.
Health and specification routes remain exempt. Automation operations retain
their existing workload capability authentication.

Browser WebSocket upgrades must send an `Origin` that exactly matches one of
the comma-separated `WEBSOCKET_ALLOWED_ORIGINS`. A rejected or malformed
origin receives `403` before token validation or session lookup. Requests
without `Origin` remain available to non-browser clients. When the setting is
empty, the allowed HTTP(S) origin is derived from
`PROXY_DATAPLANE_PUBLIC_URL` (`wss` becomes `https`); localhost deployments
also allow the standard Angular development origins. Production should set the
value explicitly.

HTTP responses before an upgrade use `Cache-Control: no-store`, disable MIME
sniffing and framing, and apply `Referrer-Policy: no-referrer`. The dataplane
does not emit CORS allow headers.

The WebSocket endpoint accepts the session token either as:

```text
Authorization: Bearer <token>
```

or as `?token=<token>` for browser WebSocket clients. Browser upgrades also
use `apiVersion=1`; HTTP clients send `x-api-version: 1`.

The session token must belong to the authenticated Cognito user. Its signed
claims must match Session Manager metadata and include the target SSH address,
username, credential secret ID, and pinned host public key.

Local HTTP tests inject a verifier backed by signed P-256 fixtures and a local
key endpoint. There is no runtime identity bypass or unsigned-header mode.
Interactive testing uses the Cognito-authenticated ALB route.

## Terminal Protocol

WebSocket messages are binary. The first byte is the packet type:

```text
0x00 input
0x01 output
0x02 error
0x07 session event
0x10 resize: uint16 columns + uint16 rows, big endian
0x11 heartbeat: empty payload
0x12 detach WebSocket
0x18 terminate session
```

The dataplane uses `golang.org/x/crypto/ssh`, requests an `xterm-256color` PTY,
and starts a persistent shell. Only one owner may write. Rejoin attaches to the
same shell, while observers receive scrollback and future output.

Session event packets may contain plain UTF-8 lifecycle text such as
`observer attached`, or a structured JSON attachment snapshot:

```json
{"type":"attachments","observers":["user-subject-1","user-subject-2"]}
```

The observer list is live runtime state for the active dataplane process; it is
not persisted in Session Manager. Observer resize packets are accepted as a
no-op so read-only viewers do not receive writer-only errors during terminal
initialization. Empty heartbeat packets are accepted for owner and observer
attachments, do not touch the SSH shell or audit stream, and exist only to keep
the authenticated WebSocket transport alive.

## API Specifications

The HTTP OpenAPI contract is checked in at `api/openapi.yaml` and is served
unauthenticated at:

```http
GET /openapi.yaml
```

The terminal WebSocket AsyncAPI contract is checked in at `api/asyncapi.yaml`
and is served unauthenticated at:

```http
GET /asyncapi.yaml
```

Business endpoints require `x-api-version: 1`. `GET /healthz`,
`GET /openapi.yaml`, and `GET /asyncapi.yaml` are exempt. Browser WebSocket
upgrades may send `apiVersion=1` in the query string because browser
`WebSocket` clients cannot set custom upgrade headers; non-browser clients
should send `x-api-version: 1`.
Successful probe request logs are suppressed; failed probe responses still emit
request logs.

## API contract verification

`api/openapi.yaml` is the checked-in HTTP contract, served byte-for-byte at
`GET /openapi.yaml`. CI validates it with `openapi-spec-validator==0.9.0`.
Handler tests validate representative success and error responses against its
schemas, including missing and unsupported API versions. Business HTTP calls
require `x-api-version: 1`; the specification declares route exemptions and
any transport-specific alternatives. Run the repository test suite when
changing handlers or response models, and update the contract in the same PR.

The Image workflow runs only after successful CI for a push to `main`. It builds
the tested source SHA, retains that SHA as a traceability tag, and promotes the
build's `sha256` digest through a Kubernetes PR. Promotion requires the digest;
Argo CD deploys that immutable reference after the GitOps PR is merged.
