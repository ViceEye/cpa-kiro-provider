# Kiro Provider for CLIProxyAPI

Repository: <https://github.com/ViceEye/cpa-kiro-provider>

`kiro-provider` is an independent AGPL-3.0 native provider plugin for
CLIProxyAPI. It imports existing Kiro IDE, kiro-cli, Amazon Q, and AWS SSO
credentials and exposes Kiro runtime models through the normal CPA API.

The production runtime consists only of CLIProxyAPI and
`kiro-provider-v0.5.6.so`. Kiro Gateway is the protocol reference and is not a
sidecar or runtime dependency.

## Current release

### v0.5.6 - 2026-08-17

Changes:

- Reapplies system and developer instructions after oversized conversation
  history is trimmed to Kiro's request limit.
- Moves those instructions to the current user message when all previous
  history must be removed.
- Keeps history trimming aligned to conversation turns and removes leading
  orphaned tool-result turns.
- Uses the Go standard library for trimming bounds and keeps test fixtures on
  the same runtime data shape as production code.

Fixes:

- Fixed long Codex conversations silently losing their system or developer
  instructions after payload trimming.
- Fixed oversized tool conversations potentially starting with a tool result
  whose matching assistant tool call had already been removed.

Verification completed for this release:

- `go vet ./...`
- `go test ./...`
- Linux amd64 `c-shared` build in Docker.
- Network-isolated CPA integration tests covering model discovery, quota,
  streaming and non-streaming chat completions, tools, refresh, and account
  failover.

## Development history

Early `0.x` versions were fast local integration builds rather than separately
published releases. The milestones below record verified capability groups;
they do not claim a one-to-one mapping between every local `.so` and a Git
commit.

| Version milestone | Date | Development result |
| --- | --- | --- |
| `v0.1.x-v0.3.x` | 2026-08-16 | Established the standalone Linux plugin ABI, Kiro credential import and refresh, model registration, OpenAI Chat Completions conversion, AWS Event Stream parsing, and multi-account execution. |
| `v0.4.x` | 2026-08-16 | Added dynamic model discovery, `profileArn` handling, quota reporting, management routes, and isolated CPA protocol fixtures. |
| `v0.5.0-v0.5.4` | 2026-08-16 to 2026-08-17 | Added first-time Kiro browser login, organization IAM Identity Center continuation, device authorization, region separation, persisted OAuth repair, and request compatibility fixes found during live CPA testing. |
| `v0.5.5` | 2026-08-17 | Added bounded history trimming so large Codex requests remain below Kiro's payload limit while preserving complete turns. |
| `v0.5.6` | 2026-08-17 | Preserved system/developer instructions during trimming and hardened orphaned tool-result removal. |

When Git history starts, use commits and tags as the source of truth for later
releases instead of extending this reconstructed pre-Git history.

## Features

- Kiro Desktop and AWS SSO OIDC refresh flows.
- First-time Kiro CLI-compatible browser PKCE login through `app.kiro.dev`,
  including the organization choice shown by Kiro's unified sign-in page.
- Optional AWS Builder ID and IAM Identity Center device-code login.
- Kiro IDE JSON, Enterprise `clientIdHash`, AWS SSO cache JSON, kiro-cli and
  Amazon Q SQLite import.
- Multi-account import through CPA-owned `AuthData` persistence.
- Reference mode (default) and independent copy mode.
- Namespaced `kiro/<model>` registrations.
- Per-account dynamic model discovery through Kiro `ListAvailableModels`, with
  static models retained only as an outage fallback.
- Automatic `profileArn` discovery through the AWS JSON
  `ListAvailableProfiles` operation for older OAuth credentials that contain
  valid tokens but no profile, with CPA auth persistence after discovery.
- Authenticated Kiro subscription and credit quota Management API.
- Official Kiro favicon in CPA plugin metadata.
- OpenAI Chat Completions input/output, including streaming, images, tools,
  tool results, and multi-turn history.
- AWS Binary Event Stream framing and CRC validation.
- CPA host HTTP bridge usage for every upstream request.
- HTTP status propagation for CPA refresh, cooldown, retry, and failover.

The management panel login action defaults to the same browser shape used by
Kiro CLI: the plugin generates a fresh state and PKCE pair and returns an
`https://app.kiro.dev/signin?...` URL. The verifier stays only in CPA's in-memory
plugin OAuth session. CPA polls the plugin and saves the completed credential.

For a remote CPA server, social Kiro login redirects the user's browser to
`http://localhost:3128`. If no local callback helper is listening, copy the
final URL from the browser address bar into CPA's OAuth callback field. CPA
stores the callback in its auth directory and the plugin exchanges the code.

Kiro's unified sign-in page returns an intermediate `login_option=awsidc`
callback for organization accounts rather than an OAuth code. The independent
management panel submits this URL to the plugin's
`POST /v0/management/plugins/kiro-provider/oauth/callback` route. The plugin
reads the returned `issuer_url` and `idc_region`, registers an AWS SSO OIDC
client, starts device authorization, and returns the organization verification
link and user code. CPA keeps polling the original plugin login session and
saves the credential after the user approves it. No CPA core change is needed.

`organization-browser` remains available as an experimental direct
authorization-code flow. The Kiro CLI-compatible two-stage portal/device flow
uses the default `kiro-browser` mode.

## Build

Docker Desktop or a Linux Docker engine is required. The build target is
Linux amd64 with glibc, matching the CPA Debian image.

```bash
cd plugins-src/kiro-provider
docker build --output type=local,dest=dist .
```

The artifact is written to:

```text
dist/linux/amd64/kiro-provider-v0.5.6.so
```

## Install

Copy the versioned library into the CPA plugin directory:

```bash
mkdir -p plugins/linux/amd64
cp dist/linux/amd64/kiro-provider-v0.5.6.so plugins/linux/amd64/
```

Enable the plugin in `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    kiro-provider:
      enabled: true
      priority: 100
      import_mode: reference
      login_mode: kiro-browser
      sso_region: eu-west-1
      api_region: us-east-1
      static_models: []
```

`sso_region` selects the AWS IAM Identity Center/OIDC endpoint. `api_region`
selects the Kiro runtime and Amazon Q service endpoints and is independent of
the SSO region; Kiro currently uses `us-east-1` for these API calls.

To use AWS SSO OIDC device authorization instead:

```yaml
plugins:
  configs:
    kiro-provider:
      login_mode: aws-device
      sso_region: eu-west-1
      sso_start_url: https://example.awsapps.com/start
```

For Docker Compose, mount the repository's `plugins` directory at
`/CLIProxyAPI/plugins`, as supported by the standard CPA compose file.

## Import credentials

Reference mode is recommended when CPA and Kiro share an account. The plugin
reloads the external source before refresh, reducing conflicts when Kiro
rotates a refresh token.

```bash
./CLIProxyAPI --kiro-import /credentials --kiro-import-mode reference
```

Copy mode gives CPA an independent stored copy:

```bash
./CLIProxyAPI --kiro-import /credentials/token.json --kiro-import-mode copy
```

Supported sources:

- Kiro IDE token JSON.
- AWS SSO cache JSON.
- Enterprise token JSON plus sibling `<clientIdHash>.json` registration.
- kiro-cli or Amazon Q SQLite databases containing `auth_kv`.
- A directory containing one or more of these formats.
- A direct JSON object using camelCase or snake_case token fields.

When CPA runs in Docker, mount external credentials read-only:

```yaml
services:
  cli-proxy-api:
    volumes:
      - /host/kiro-credentials:/credentials:ro
```

Never put real credentials in the plugin source tree, image, build context,
logs, or test fixtures.

## Use

List models through the normal CPA endpoint:

```bash
curl http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer $CPA_API_KEY"
```

Call a Kiro model through OpenAI Chat Completions:

```bash
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer $CPA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "kiro/claude-sonnet-4.5",
    "stream": true,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

The plugin intentionally targets OpenAI Chat Completions, including streaming,
for clients such as Codex.

The plugin dynamically calls Kiro `ListAvailableModels` for every account by
using the AWS JSON service endpoint at `https://q.{region}.amazonaws.com/`.
Before model execution, discovery, or quota lookup, a credential without a
`profileArn` is upgraded in place using `ListAvailableProfiles`; existing OAuth
logins therefore do not need to be repeated.
The bundled static list is used only when discovery fails. Set
`model_discovery_url` only to override the service endpoint for a private
gateway or integration fixture. `runtime_base_url`, `desktop_refresh_url`, and
`oidc_refresh_url` have the same override purpose.

## Quota management

The plugin exposes an authenticated Management API endpoint. It calls Kiro's
HTTP GET `getUsageLimits` endpoint with `origin=AI_EDITOR` and
`resourceType=AGENTIC_REQUEST` and the account's discovered `profileArn`:

```text
GET /v0/management/plugins/kiro-provider/quota
```

It calls Kiro `GetUsageLimits` for every enabled Kiro credential and returns
sanitized subscription, credit usage, overage, charge, and reset information.
Tokens, refresh credentials, profile ARNs, and upstream user IDs are never
included. Set `usage_url` only to override the default Kiro service endpoint.

Plugin metadata uses the official Kiro favicon at
`https://kiro.dev/favicon.ico`.

## Credential behavior

Each imported account becomes a separate `provider=kiro` CPA auth. CPA owns
selection, session affinity, retry, cooldown, and failover. The plugin maps:

- `401`/`403`: refresh and retry once, then propagate the status.
- `402`/`429`: propagate for CPA cooldown and account switching.
- network failures and `5xx`: retryable upstream failures.
- malformed client payloads: non-retryable `400`.
- corrupt or truncated Event Stream data: stream failure.

The plugin never logs authorization headers or credential payloads.

## Tests

All fixtures use fake values and tests perform no real network requests:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.26-bookworm \
  sh -c '/usr/local/go/bin/gofmt -w . && /usr/local/go/bin/go test -v ./...'
```

An online smoke test is optional and must use a read-only credential mount.

The repository also includes a network-isolated CPA integration fixture. Once
the `kiro-mock` and `cpa-kiro-provider-test` containers are running on the
`kiro-provider-test` Docker network, run:

```bash
docker restart kiro-mock cpa-kiro-provider-test
docker run --rm --network kiro-provider-test \
  -v "$PWD/integration:/tests:ro" \
  python:3.12-slim python /tests/run_integration.py
```

This verifies dynamic model discovery, the authenticated quota Management API,
OpenAI Chat Completions streaming and non-streaming output, tool calls,
credential refresh, and multi-account failover for 402, 403, 429, and 5xx
responses. All fixture credentials and responses are synthetic.

## Release checklist

1. Update `pluginVersion`, the Docker output filename, and README artifact
   references together.
2. Run `gofmt`, `go vet ./...`, and `go test ./...` in the pinned Go image.
3. Build the Linux amd64 `.so` and run the isolated CPA integration suite.
4. Confirm that test fixtures contain synthetic values only.
5. Tag the release; do not commit `dist/`, credentials, OAuth callbacks, or
   local CPA configuration.

## License

This plugin is licensed under AGPL-3.0-or-later. See `LICENSE` and `NOTICE`.
