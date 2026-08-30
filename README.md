# Kiro Provider for CLIProxyAPI

Repository: <https://github.com/ViceEye/cpa-kiro-provider>

`kiro-provider` is an independent AGPL-3.0 native provider plugin for
CLIProxyAPI. It imports existing Kiro IDE, kiro-cli, Amazon Q, and AWS SSO
credentials and exposes Kiro runtime models through the normal CPA API.

The production runtime consists only of CLIProxyAPI and
`kiro-provider-v0.8.0.so`. Kiro Gateway is the protocol reference and is not a
sidecar or runtime dependency.

## Current release

### v0.8.0 - 2026-08-30

Fixes:

- Keeps one CPA auth record per physical credential file. The plugin no longer
  returns a content-hash auth ID from `auth.parse`, command line imports, refresh
  responses, and model-discovery updates; the host now derives one file-based
  record ID that `host.auth.save` upserts instead of registering a second record
  for the same file. Multi-account files keep distinct per-account IDs.
- Refresh responses now carry over stored fields the plugin does not model
  (`disabled`, `priority`, `note`, ...), so a refreshed credential no longer
  loses its disabled flag or weight.
- Keeps the host record ID out of the stored credential JSON, so the
  credential's content identity survives host-driven refreshes.
- Preserves host record attributes (including the auth file path) across
  refresh and model-discovery updates.
- The console closes the relogin flow steps automatically after a successful
  relogin instead of leaving the login URL and callback box open.
- Adds an in-plugin OAuth provider selection page for Kiro and Cline.
- Adds Cline browser OAuth callback handling and stores Cline credentials with
  the plugin provider marker plus the Cline kind marker.

Verification completed for this release:

- `go vet ./...`
- `go test ./...`
- Linux amd64 `c-shared` build in Docker.
- Live CPA deployment: refreshed credential persisted to the original file and
  the auth manager kept exactly one record for it.

## Release changelog

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
| `v0.6.0` | 2026-08-17 | Added plugin-owned OAuth Resource Routes and callback handling for compatible environments. Production `app.kiro.dev` still requires a loopback redirect URI, so remote Builder ID and organization login use `aws-device`. |
| `v0.6.1` | 2026-08-17 | Accepted organization-specific AWS device verification URLs without broadening the trusted-host boundary. |
| `v0.6.2` | 2026-08-26 | Added Codex-to-Kiro Claude history normalization and fixed fragmented `toolUseEvent` output producing duplicate malformed OpenAI tool calls. |
| `v0.7.0` | 2026-08-27 | Fixed object-valued tool arguments concatenating into invalid JSON, dropped `required` entries with no matching property, removed the assistant-content filler sentence that models echoed back as output, and degraded oversized requests by dropping large images and truncating large tool results instead of failing the turn. |
| `v0.7.7` | 2026-08-29 | Unified auth record identity on the host's file-based ID so plugin saves upsert instead of duplicating credentials, kept host record IDs out of stored credential JSON, and made the console close the relogin flow after success. |

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

## Quick start

1. Build or download the Linux `.so` and place it under CPA's configured
   plugin directory.
2. Enable `plugins.enabled` and `plugins.configs.kiro-provider.enabled`.
3. Choose one login mode from the next section. Remote organization accounts
   should use `aws-device`.
4. Restart or recreate the CPA service and confirm that logs show
   `plugin registered plugin_id=kiro-provider`.
5. Open CPA's OAuth page, select **Kiro OAuth**, and complete sign-in.
6. Confirm that Kiro models appear in `GET /v1/models`, then call them through
   the normal OpenAI Chat Completions endpoint.
7. Install the optional customized `management.html` only when Kiro balance
   and quota cards are required in the web panel.

The plugin, OAuth login, model proxy, automatic token refresh, and quota API do
not require CPA core changes. Only the visual Kiro quota cards require the
customized management frontend.

## Plugin configuration

Configure Kiro either from **Management Center → Plugins → Kiro → Edit
configuration** or directly in CPA's `config.yaml` under
`plugins.configs.kiro-provider`. The panel and YAML edit the same plugin-scoped
settings; rebuilding the `.so` is not required when only these values change.

### Organization account template

For a remote CPA server using AWS IAM Identity Center, start with this complete
configuration:

```yaml
plugins:
  enabled: true
  dir: /CLIProxyAPI/plugins
  configs:
    kiro-provider:
      enabled: true
      priority: 100
      import_mode: reference
      login_mode: aws-device
      browser_redirect_uri: http://localhost:3128
      sso_start_url: https://your-company.awsapps.com/start
      sso_region: eu-west-1
      api_region: us-east-1
      static_models: []
```

Replace:

- `sso_start_url` with the organization's AWS access portal URL.
- `sso_region` with the IAM Identity Center region used by that organization.
- `dir` with the plugin path visible inside the CPA process or container.

Keep `api_region: us-east-1` unless the Kiro runtime for the account is known to
use another region. `sso_region` controls login and token refresh;
`api_region` controls Kiro models, profiles, chat, and quota calls.

`login_mode: aws-device` names the OAuth flow, not the account type. A
non-default organization `sso_start_url` makes the resulting credential a
**Kiro Organization** credential. `browser_redirect_uri` is ignored by this
device flow, but keeping the safe localhost default prevents an invalid public
redirect if the login mode is changed later.

After saving, confirm that CPA logs contain:

```text
plugin registered plugin_id=kiro-provider
```

Then open **OAuth → Kiro OAuth**, follow the AWS verification link, complete
organization SSO, and leave the panel open until CPA reports success.

### Configuration reference

| Setting | Default | Purpose |
| --- | --- | --- |
| `enabled` | `true` | Enables this plugin configuration. |
| `priority` | host default | CPA provider priority. |
| `import_mode` | `reference` | Controls imported files: `reference` reloads the source; `copy` stores an independent copy. OAuth logins are stored by CPA. |
| `login_mode` | `kiro-browser` | Selects `kiro-browser` or `aws-device`. |
| `sso_start_url` | `https://view.awsapps.com/start` | Default means Builder ID; an organization `*.awsapps.com/start` URL means IAM Identity Center. |
| `sso_region` | `us-east-1` | AWS SSO OIDC registration, device authorization, and refresh region. |
| `api_region` | `us-east-1` | Kiro runtime, model discovery, profile, and quota region. |
| `browser_redirect_uri` | `http://localhost:3128` | Used only by browser authorization-code flows; Kiro rejects arbitrary public CPA domains. |
| `model_prefix` | `kiro/` | Prefix registered on discovered model IDs. Use another non-empty prefix only when needed. |
| `static_models` | `[]` | Additional fallback model IDs when dynamic discovery is unavailable. |

`runtime_base_url`, `model_discovery_url`, `usage_url`, refresh URLs, and token
URLs are test/private-gateway overrides. Leave them unset for production Kiro.

## Login modes

Choose the flow based on the account and where CPA runs:

| `login_mode` | Use case | Redirect behavior | Remote CPA |
| --- | --- | --- | --- |
| `aws-device` | AWS Builder ID or organization IAM Identity Center | Opens an AWS verification URL and CPA polls for completion | Recommended |
| `kiro-browser` | Kiro personal/social browser OAuth compatible with kiro-cli | Kiro redirects to `http://localhost:3128` | Requires a local callback listener, relay, or manual callback submission |

Kiro's production sign-in page rejects arbitrary public redirect hosts with
`Invalid redirect URI. Must be localhost or a subdomain of app.kiro.dev`.
Do not configure a CPA domain such as `https://cpa.example.com/...` as
`browser_redirect_uri`.

### Organization login on a remote CPA server

Use AWS device authorization. This is the simplest flow because it has no
browser callback:

```yaml
plugins:
  configs:
    kiro-provider:
      enabled: true
      login_mode: aws-device
      sso_start_url: https://example.awsapps.com/start
      sso_region: eu-west-1
      api_region: us-east-1
```

Open CPA's OAuth page, start **Kiro OAuth**, open the returned AWS verification
URL, complete organization SSO, and leave the panel open while CPA polls and
saves the credential. Although the setting is named `aws-device`, the account
is stored as **Kiro Organization** whenever `sso_start_url` is an organization
IAM Identity Center URL instead of the default Builder ID URL.

### Builder ID device login

Use the Builder ID start URL. `sso_region` defaults to `us-east-1` when omitted:

```yaml
plugins:
  configs:
    kiro-provider:
      login_mode: aws-device
      sso_start_url: https://view.awsapps.com/start
      sso_region: us-east-1
      api_region: us-east-1
```

### Personal/social browser login

Use the Kiro CLI-compatible loopback redirect:

```yaml
plugins:
  configs:
    kiro-provider:
      login_mode: kiro-browser
      browser_redirect_uri: http://localhost:3128
      api_region: us-east-1
```

When CPA is remote, `localhost` belongs to the browser machine, not the CPA
server. Complete this flow with one of the following:

1. Use a local Kiro CLI/browser listener or another loopback relay, then paste
   the complete localhost callback URL into a management panel that submits it to
   `POST /v0/management/plugins/kiro-provider/oauth/callback`.
2. Sign in with Kiro CLI or Quotio and import the resulting credential.

Never paste OAuth callback URLs into logs or issues; they may contain a
short-lived authorization code.

## Repository layout

```text
cmd/kiro-provider/      Native C ABI entry point
internal/provider/      CPA/Kiro orchestration, OAuth, credentials and quota
internal/chat/          OpenAI Chat Completions to Kiro request conversion
internal/eventstream/   AWS Event Stream parser
internal/jsonx/         Shared JSON value helpers
internal/provider/testdata/  Synthetic credential fixtures
integration/            Network-isolated CPA integration suite
.github/workflows/      GitHub Actions CI
```

## Build

Docker Desktop or a Linux Docker engine is required. The build target is
Linux amd64 with glibc, matching the CPA Debian image.

```bash
git clone https://github.com/ViceEye/cpa-kiro-provider.git
cd cpa-kiro-provider
docker build --output type=local,dest=dist .
```

The artifact is written to:

```text
dist/linux/amd64/kiro-provider-v0.8.0.so
```

## Install

CPA searches both its plugin root and the matching `<GOOS>/<GOARCH>`
subdirectory. Copy the versioned library using either layout:

```bash
# Flat layout, such as the default /CLIProxyAPI/plugins mount:
cp dist/linux/amd64/kiro-provider-v0.8.0.so plugins/

# Or platform-specific layout:
mkdir -p plugins/linux/amd64
cp dist/linux/amd64/kiro-provider-v0.8.0.so plugins/linux/amd64/
```

Enable the plugin in `config.yaml`:

Use the complete template and field reference in
[Plugin configuration](#plugin-configuration). The organization example is
also suitable for Docker Compose when `dir` matches the container mount.

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

### Optional Kiro quota panel

The official CPA `management.html` can load the plugin and start Kiro OAuth,
but it does not render the plugin-specific quota endpoint. To see Kiro balance,
credits, reset time, and overage status in **Quota Management**, a customized
`management.html` is required.

The customization is limited to the Kiro quota adapter, types, translations,
icon, and tests. It does not modify the OAuth page or CPA core. Build it from
the sibling `Cli-Proxy-API-Management-Center` source tree.

```bash
cd ../Cli-Proxy-API-Management-Center
bun install --frozen-lockfile
bun run verify
```

The single-file build is `dist/index.html`. Deploy it as CPA's
`static/management.html` (or the file selected by `MANAGEMENT_STATIC_PATH`).
Prevent CPA's automatic updater from replacing the custom build:

```yaml
remote-management:
  disable-auto-update-panel: true
```

For Docker Compose, mount the file into the same static path used by the CPA
container:

```yaml
services:
  cli-proxy-api:
    environment:
      MANAGEMENT_STATIC_PATH: /CLIProxyAPI/static/management.html
    volumes:
      - ./management.html:/CLIProxyAPI/static/management.html:ro
```

Copy `dist/index.html` to the host as `management.html`, then recreate only the
CPA service. The plugin quota endpoint remains usable even when the official
panel is restored; only the Kiro quota cards disappear.

## Token lifetime, refresh, and re-login

An access token expiring after several hours does **not** mean that the saved
credential must be deleted. OAuth login stores the access token together with
its refresh token and, for AWS SSO accounts, the OIDC client ID and secret.

The plugin refreshes automatically:

- CPA schedules a refresh ten minutes before the recorded access-token expiry.
- Chat, streaming chat, model discovery, profile discovery, and quota lookup
  refresh before use when the token is missing or close to expiry.
- A Kiro API response of `401` or `403` triggers one refresh and one retry.
- Rotated access and refresh tokens are persisted back to the same CPA auth
  entry, so its file name and auth ID remain stable.

Normal eight-hour access-token expiry therefore needs no manual action and no
new OAuth login.

Re-login is required only when refresh can no longer succeed, for example when
the refresh token was revoked, organization access was removed, or the AWS OIDC
client registration is no longer valid. In that case:

1. Leave the old credential in place.
2. Start **Kiro OAuth** again and complete authorization.
3. Verify a model call and quota lookup with the new credential.
4. Disable or delete the old failed credential afterward.

Deleting the old credential before re-login is unnecessary and removes the
easy rollback path. The current CPA OAuth flow creates a new auth entry for a
new AWS client registration; it does not overwrite an unrelated expired entry.

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
  sh -c '/usr/local/go/bin/gofmt -w cmd/kiro-provider/*.go internal/*/*.go && /usr/local/go/bin/go vet ./... && /usr/local/go/bin/go test -v ./...'
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
5. Push a `v<version>` tag. CI creates or updates the GitHub Release and uploads
   `kiro-provider_<version>_linux_amd64.zip` plus `checksums.txt`. The ZIP
   contains `kiro-provider.so` at its root as required by the CPA Plugin Store.
6. Publish the optional customized `management.html` as a separate release
   asset when its Kiro quota adapter matches this plugin release.
7. To repair assets for an existing tag, manually run the CI workflow with
   `release_tag` set to that tag, such as `v0.7.0`.
8. Do not commit `dist/`, credentials, OAuth callbacks, or local CPA
   configuration.

## License

This plugin is licensed under AGPL-3.0-or-later. See `LICENSE` and `NOTICE`.
