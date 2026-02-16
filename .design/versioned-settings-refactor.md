# Versioned Settings Refactor: Design & Transition Plan

## Status: Draft (Revised)
## Date: 2026-02-16
## Supersedes: `.design/_archive/settings-refactor.md`

---

## 1. Motivation

The current settings system evolved organically and has several structural problems:

1. **Ambiguous grouping.** Settings like `active_profile`, `default_template`, `grove_id`, and `bucket` sit at the top level without a clear domain owner. Some are CLI concerns, some are profile concerns, some are hub concerns.

2. **No schema or versioning.** There is no machine-readable schema for settings. Typos, missing fields, and structural errors are only discovered at runtime (or never). There is no way to determine which features a given settings file supports.

3. **Two disjoint config systems.** The CLI/agent settings (`settings.yaml`) and the server config (`server.yaml`, `GlobalConfig`) use separate loading paths, separate structs, and separate env-var conventions, even though they share concepts like `brokerID`.

4. **Missing feature support.** Upcoming features (interactive mode, max agent duration, max turns, named harness configs) need settings support that doesn't exist in the current flat model.

5. **No deprecation path.** Changing the settings structure would silently break existing users. There is no mechanism to detect legacy vs modern settings, warn about deprecated fields, or guide migration.

6. **Inconsistent field naming.** The current code mixes camelCase koanf tags (`groveId`, `apiKey`, `brokerNickname`) with snake_case tags (`active_profile`, `grove_id`, `local_only`). Some env var overrides (e.g., `SCION_HUB_GROVE_ID`, `SCION_HUB_BROKER_NICKNAME`) do not work because the Koanf key mapping produces snake_case keys that don't match the camelCase struct tags. The versioned settings will standardize on snake_case everywhere.

---

## 2. Target Settings Groups

The new settings structure recognizes these primary domain groups:

### 2.1 `server` (global-only)

Server/broker process configuration. Only valid at the global level (`~/.scion/settings.yaml`), never in grove-level settings.

```yaml
server:
  env: prod                        # deployment environment label (new)
  hub:                             # hub API server settings (when running scion-server)
    port: 9810
    host: "0.0.0.0"
    # public_url is the externally-reachable URL for this Hub server.
    # Passed to agents so they can report status back. Distinct from
    # the hub CLIENT endpoint (Section 2.2) which is where clients connect.
    public_url: "https://hub.example.com"
    read_timeout: 30s
    write_timeout: 60s
    cors:
      enabled: true
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
      allowed_headers: ["Authorization", "Content-Type", "X-Scion-Broker-Token", "X-Scion-Agent-Token", "X-API-Key"]
      max_age: 3600
    admin_emails: []
  runtime_broker:
    enabled: false
    port: 9800
    host: "0.0.0.0"
    read_timeout: 30s
    write_timeout: 120s
    hub_endpoint: ""               # Hub API endpoint for status reporting (when Hub is remote)
    broker_id: ""                  # unique broker identifier (UUID, auto-generated if empty)
    broker_name: ""                # human-readable broker name
    broker_nickname: ""            # human-readable display name (defaults to hostname)
    broker_token: ""               # token received when registering with Hub
    cors:
      enabled: true
      allowed_origins: ["*"]
      allowed_methods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
      allowed_headers: ["Authorization", "Content-Type", "X-Scion-Broker-Token", "X-API-Key"]
      max_age: 3600
  database:
    driver: sqlite
    url: ""
  auth:
    dev_mode: false
    dev_token: ""
    dev_token_file: ""
    authorized_domains: []
  oauth:
    web:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
    cli:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
    device:
      google: { client_id: "", client_secret: "" }
      github: { client_id: "", client_secret: "" }
  storage:
    provider: local
    bucket: ""
    local_path: ""
  secrets:
    backend: local
    gcp_project_id: ""
    gcp_credentials: ""
  log_level: info
  log_format: text
```

**Changes from original draft:**
- `broker_id` moved from server top-level into `server.runtime_broker` (where it already lives in the current `RuntimeBrokerConfig` struct).
- `broker_nickname` and `broker_token` moved from `hub` client section (Section 2.2) into `server.runtime_broker`. These fields describe this machine's identity as a broker and are inherently machine-scoped (global-only), not per-grove.
- `server.hub.endpoint` renamed to `server.hub.public_url` to distinguish from the hub CLIENT endpoint.
- `server.runtime_broker` now includes `read_timeout`, `write_timeout`, and full CORS settings to match the current `RuntimeBrokerConfig` struct.
- CORS `allowed_headers` defaults updated to match actual code (includes `X-Scion-Broker-Token`, `X-Scion-Agent-Token`, `X-API-Key`).

**Rationale:** This consolidates the current `GlobalConfig`/`server.yaml` system into the unified settings file. The separate `server.yaml` continues to work during the transition but the canonical location becomes `settings.yaml` under the `server` key.

### 2.2 `hub` (hub client)

Settings for connecting to a remote Scion Hub as a client. Valid at global or grove level (grove overrides global).

```yaml
hub:
  enabled: true
  endpoint: "https://hub.example.com"   # Hub API URL to connect to (as a client)
  token: ""                              # bearer token for Hub auth (see Open Question 1)
  api_key: ""                            # API key for Hub auth (alternative to token)
  grove_id: ""                           # grove identifier from Hub registration
  local_only: false                      # operate in local-only mode even when Hub is configured
  last_synced_at: ""                     # RFC3339 timestamp of last successful Hub sync (runtime-managed)
```

**Changes from original draft:**
- `broker_id`, `broker_nickname`, `broker_token` moved to `server.runtime_broker` (Section 2.1). Broker identity is per-machine, not per-grove.
- `last_synced_at` added. This field exists in the current `HubClientConfig` and is written back to settings by the sync logic. It tracks when this grove last synced with the Hub, used to distinguish locally-deleted agents from remotely-created ones.

**Note:** The `hub.endpoint` field is the URL this CLI/broker connects to as a Hub client. This is distinct from `server.hub.public_url`, which is the public-facing URL of a Hub server process (used for agent callback URLs). They often have the same value but serve different roles.

### 2.3 `cli`

Controls CLI behavior. Valid at global or grove level.

```yaml
cli:
  autohelp: true
  interactive_disabled: false      # new: disable interactive prompts
```

### 2.4 `runtimes` (named map)

Container runtime definitions. Valid at global or grove level.

The name of a runtime entry is an arbitrary label chosen by the user — it does **not** need to match the `type` field. This allows defining multiple runtimes of the same type with different configurations (e.g., `staging-docker` and `prod-docker` both with `type: docker`).

```yaml
runtimes:
  docker:                          # name matches type (conventional default)
    type: docker
    host: ""
    env: {}
    sync: ""
  container:
    type: container
    tmux: true
  kubernetes:
    type: kubernetes
    context: ""
    namespace: ""
  staging-docker:                  # name differs from type
    type: docker
    host: "tcp://staging.example.com:2376"
    env:
      DOCKER_TLS_VERIFY: "1"
```

**Change from current:** An explicit `type` field is added to each runtime. This was implicit before (the runtime name *was* the type). With the `type` field, users can define multiple runtimes of the same type with different configurations. The legacy names-as-types behavior is preserved for backward compatibility (if `type` is absent, the name is used as the type).

### 2.5 `harness_configs` (named map)

Named harness configurations. This replaces the current `harnesses` map. Multiple configs can exist for the same harness type.

```yaml
harness_configs:
  gemini:                          # default config for gemini harness
    harness: gemini
    image: "us-central1-docker.pkg.dev/.../scion-gemini:latest"
    user: scion
    model: ""
    args: []
    env: {}
    volumes: []
    auth_selected_type: ""         # e.g., "gemini-api-key", "vertex-ai", "oauth-personal"
  claude:                          # default config for claude harness
    harness: claude
    image: "us-central1-docker.pkg.dev/.../scion-claude:latest"
    user: scion
    model: ""
    args: []
    env: {}
    volumes: []
  opencode:                        # default config for opencode harness
    harness: opencode
    image: "us-central1-docker.pkg.dev/.../scion-opencode:latest"
    user: scion
  codex:                           # default config for codex harness
    harness: codex
    image: "us-central1-docker.pkg.dev/.../scion-codex:latest"
    user: scion
  gemini-high-security:            # named variant (arbitrary name)
    harness: gemini
    image: "us-central1-docker.pkg.dev/.../scion-gemini:hardened"
    user: scion
    model: "gemini-2.5-pro"
    args: ["--sandbox=strict"]
    env:
      GEMINI_SAFETY: "maximum"
```

**Change from current:** The `harnesses` map only allowed one entry per harness type (keyed by harness name). The new `harness_configs` map is keyed by an arbitrary config name, with an explicit `harness` field specifying the harness type. There is a convention that each harness has a "default" config whose name matches the harness (e.g., config named `gemini` with `harness: gemini`).

**New fields:** `model` and `args` are new additions to harness configs. The current `HarnessConfig` struct does not have these — they exist only in the agent-level `ScionConfig`. Adding them to `harness_configs` allows setting model and arguments as defaults at the settings level.

### 2.6 `profiles` (named map)

Named environment profiles. Valid at global or grove level.

```yaml
profiles:
  local:
    runtime: container
    default_template: gemini
    default_harness_config: gemini  # which harness_config to use by default
    tmux: true
    env: {}
    volumes: []
    resources: null
    harness_overrides:              # per-harness-config overrides
      gemini:
        image: "custom:dev"
  remote:
    runtime: kubernetes
    default_template: gemini
    default_harness_config: gemini
    tmux: false
```

**Change from current:** `default_template` and `default_harness_config` are added to profiles. The top-level `default_template` and `active_profile` remain for backward compatibility but profiles can now be self-describing.

### 2.7 `agent` (template configuration)

Agent/template-level settings. These live in `scion-agent.yaml` within template directories, not in `settings.yaml`.

```yaml
# In .scion/templates/<name>/scion-agent.yaml
harness_config: gemini             # references a key in harness_configs
env: {}
volumes: []
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "2"
    memory: "2Gi"
  disk: "10Gi"
max_turns: 50                      # new
max_duration: "2h"                 # new
services:                          # sidecar services
  - name: browser
    command: ["chromium", "--headless"]
    restart: on-failure
    ready_check:
      type: tcp
      target: "localhost:9222"
      timeout: "10s"
```

**Change from original draft:** `harness` renamed to `harness_config` to clearly indicate that this field references a named harness configuration (a key in the `harness_configs` map), not a raw harness type. This avoids ambiguity when users define custom-named configs like `gemini-high-security`.

**Note:** For backward compatibility, if `harness_config` is not present but `harness` is, the value is treated as both the harness type and the config name (matching the legacy behavior where harness name = harness type = config key).

### 2.8 Top-level metadata

```yaml
$schema: "https://scion.dev/schemas/settings/v1.json"
schema_version: "1"

active_profile: local
default_template: gemini           # preserved for backward compatibility
```

---

## 3. JSON Schema

### 3.1 Schema Location and Naming

Schemas are stored in the repository at `pkg/config/schemas/` and embedded into the binary.

```
pkg/config/schemas/
  settings-v1.schema.json          # settings.yaml schema
  agent-v1.schema.json             # scion-agent.yaml schema
```

### 3.2 Schema Standard

JSON Schema Draft 2020-12 (`https://json-schema.org/draft/2020-12/schema`).

### 3.3 Custom Annotations

Each schema property that can be set via environment variable includes:

```json
{
  "x-env-var": "SCION_HUB_ENDPOINT",
  "x-env-var-prefix": "SCION_"
}
```

Each schema property includes scope metadata:

```json
{
  "x-scope": "global",          // "global" = global-only, "any" = global or grove
  "x-since": "1",               // schema version that introduced this field
  "x-deprecated-by": "2"        // schema version that deprecated this field (if applicable)
}
```

### 3.4 Versioning Strategy

- The schema version is a simple monotonic integer (`"1"`, `"2"`, `"3"`, ...).
- The `schema_version` field in settings.yaml declares which schema version the file conforms to.
- The binary embeds all supported schema versions and validates against the declared version.
- Feature gates can check `schema_version >= N` to determine if a feature's settings are available.

### 3.5 Schema Sketch (v1)

main settings schema kept in './settings-schema.md'

A separate agent schema (`agent-v1.schema.json`) will be defined for `scion-agent.yaml` files. Its structure mirrors the existing `ScionConfig` with additions:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://scion.dev/schemas/agent/v1.json",
  "title": "Scion Agent Configuration",
  "type": "object",
  "properties": {
    "schema_version": { "type": "string", "const": "1" },
    "harness_config": {
      "type": "string",
      "description": "Name of the harness config entry to use (key in harness_configs map). Falls back to 'harness' field for legacy compat."
    },
    "harness": {
      "type": "string",
      "description": "Legacy: harness type name. Deprecated in favor of harness_config.",
      "x-deprecated-by": "1"
    },
    "env": { "type": "object", "additionalProperties": { "type": "string" } },
    "volumes": { "type": "array", "items": { "$ref": "#/$defs/volumeMount" } },
    "resources": { "$ref": "#/$defs/resourceSpec" },
    "max_turns": {
      "type": "integer",
      "minimum": 1,
      "description": "Maximum number of LLM turns before the agent is stopped.",
      "x-since": "1"
    },
    "max_duration": {
      "type": "string",
      "pattern": "^[0-9]+(s|m|h)$",
      "description": "Maximum wall-clock duration before the agent is stopped (e.g., '2h', '30m').",
      "x-since": "1"
    },
    "services": {
      "type": "array",
      "items": { "$ref": "#/$defs/serviceSpec" }
    },
    "image": { "type": "string", "description": "Override container image." },
    "user": { "type": "string", "description": "Override unix user." },
    "model": { "type": "string", "description": "LLM model identifier." },
    "args": { "type": "array", "items": { "type": "string" } },
    "detached": { "type": "boolean", "description": "Run agent in detached (background) mode. Defaults to true." },
    "config_dir": { "type": "string", "description": "Agent config directory." },
    "command_args": { "type": "array", "items": { "type": "string" }, "description": "Additional command arguments." },
    "gemini": {
      "type": "object",
      "description": "Gemini-specific configuration.",
      "properties": {
        "auth_selected_type": { "type": "string" }
      }
    },
    "kubernetes": {
      "type": "object",
      "description": "Kubernetes-specific configuration.",
      "properties": {
        "context": { "type": "string" },
        "namespace": { "type": "string" },
        "runtime_class_name": { "type": "string" },
        "service_account_name": { "type": "string" },
        "resources": {
          "type": "object",
          "properties": {
            "requests": { "type": "object", "additionalProperties": { "type": "string" } },
            "limits": { "type": "object", "additionalProperties": { "type": "string" } }
          }
        }
      }
    }
  }
}
```

---

## 4. Environment Variable Mapping

### 4.1 Convention

All environment variables use the `SCION_` prefix. Nesting is represented by underscores. The schema's `x-env-var` annotation is the canonical source of truth.

**Important:** The new versioned settings will use snake_case consistently for all koanf struct tags. This fixes the current inconsistency where some tags use camelCase (`groveId`, `apiKey`, `brokerNickname`) while env var mappings produce snake_case keys, causing silent mismatches.

### 4.2 Settings Env Vars

These override values in `settings.yaml`. Prefix: `SCION_`.

| Settings Path | Env Var | Type |
|---|---|---|
| `active_profile` | `SCION_ACTIVE_PROFILE` | string |
| `default_template` | `SCION_DEFAULT_TEMPLATE` | string |
| `grove_id` | `SCION_GROVE_ID` | string |
| `hub.enabled` | `SCION_HUB_ENABLED` | bool |
| `hub.endpoint` | `SCION_HUB_ENDPOINT` | string |
| `hub.token` | `SCION_HUB_TOKEN` | string |
| `hub.api_key` | `SCION_HUB_API_KEY` | string |
| `hub.grove_id` | `SCION_HUB_GROVE_ID` | string |
| `hub.local_only` | `SCION_HUB_LOCAL_ONLY` | bool |
| `bucket.provider` | `SCION_BUCKET_PROVIDER` | string |
| `bucket.name` | `SCION_BUCKET_NAME` | string |
| `bucket.prefix` | `SCION_BUCKET_PREFIX` | string |
| `cli.autohelp` | `SCION_CLI_AUTOHELP` | bool |
| `cli.interactive_disabled` | `SCION_CLI_INTERACTIVE_DISABLED` | bool |

**Note:** `cli.*` and `hub.grove_id` env var mappings are new — they don't work in the current (legacy) Koanf loader due to missing key transformations. The versioned loader must implement these.

### 4.3 Server Env Vars

These override values in `server.yaml` / `settings.yaml` `server` section. Prefix: `SCION_SERVER_`.

| Settings Path | Env Var | Type |
|---|---|---|
| **Hub Server** | | |
| `server.hub.port` | `SCION_SERVER_HUB_PORT` | int |
| `server.hub.host` | `SCION_SERVER_HUB_HOST` | string |
| `server.hub.public_url` | `SCION_SERVER_HUB_ENDPOINT` | string |
| `server.hub.read_timeout` | `SCION_SERVER_HUB_READTIMEOUT` | duration |
| `server.hub.write_timeout` | `SCION_SERVER_HUB_WRITETIMEOUT` | duration |
| `server.hub.cors_enabled` | `SCION_SERVER_HUB_CORSENABLED` | bool |
| `server.hub.admin_emails` | `SCION_SERVER_HUB_ADMINEMAIL` | string (CSV) |
| **Runtime Broker** | | |
| `server.runtime_broker.enabled` | `SCION_SERVER_RUNTIMEBROKER_ENABLED` | bool |
| `server.runtime_broker.port` | `SCION_SERVER_RUNTIMEBROKER_PORT` | int |
| `server.runtime_broker.host` | `SCION_SERVER_RUNTIMEBROKER_HOST` | string |
| `server.runtime_broker.read_timeout` | `SCION_SERVER_RUNTIMEBROKER_READTIMEOUT` | duration |
| `server.runtime_broker.write_timeout` | `SCION_SERVER_RUNTIMEBROKER_WRITETIMEOUT` | duration |
| `server.runtime_broker.hub_endpoint` | `SCION_SERVER_RUNTIMEBROKER_HUBENDPOINT` | string |
| `server.runtime_broker.broker_id` | `SCION_SERVER_RUNTIMEBROKER_BROKERID` | string |
| `server.runtime_broker.broker_name` | `SCION_SERVER_RUNTIMEBROKER_BROKERNAME` | string |
| `server.runtime_broker.broker_nickname` | `SCION_SERVER_RUNTIMEBROKER_BROKERNICKNAME` | string |
| `server.runtime_broker.broker_token` | `SCION_SERVER_RUNTIMEBROKER_BROKERTOKEN` | string |
| **Database** | | |
| `server.database.driver` | `SCION_SERVER_DATABASE_DRIVER` | string |
| `server.database.url` | `SCION_SERVER_DATABASE_URL` | string |
| **Auth** | | |
| `server.auth.dev_mode` | `SCION_SERVER_AUTH_DEVMODE` | bool |
| `server.auth.dev_token` | `SCION_SERVER_AUTH_DEVTOKEN` | string |
| `server.auth.dev_token_file` | `SCION_SERVER_AUTH_DEVTOKENFILE` | string |
| `server.auth.authorized_domains` | `SCION_SERVER_AUTH_AUTHORIZEDDOMAINS` | string (CSV) |
| **OAuth — Web** | | |
| `server.oauth.web.google.client_id` | `SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTID` | string |
| `server.oauth.web.google.client_secret` | `SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.web.github.client_id` | `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID` | string |
| `server.oauth.web.github.client_secret` | `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTSECRET` | string |
| **OAuth — CLI** | | |
| `server.oauth.cli.google.client_id` | `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID` | string |
| `server.oauth.cli.google.client_secret` | `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.cli.github.client_id` | `SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTID` | string |
| `server.oauth.cli.github.client_secret` | `SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTSECRET` | string |
| **OAuth — Device** | | |
| `server.oauth.device.google.client_id` | `SCION_SERVER_OAUTH_DEVICE_GOOGLE_CLIENTID` | string |
| `server.oauth.device.google.client_secret` | `SCION_SERVER_OAUTH_DEVICE_GOOGLE_CLIENTSECRET` | string |
| `server.oauth.device.github.client_id` | `SCION_SERVER_OAUTH_DEVICE_GITHUB_CLIENTID` | string |
| `server.oauth.device.github.client_secret` | `SCION_SERVER_OAUTH_DEVICE_GITHUB_CLIENTSECRET` | string |
| **Storage** | | |
| `server.storage.provider` | `SCION_SERVER_STORAGE_PROVIDER` | string |
| `server.storage.bucket` | `SCION_SERVER_STORAGE_BUCKET` | string |
| `server.storage.local_path` | `SCION_SERVER_STORAGE_LOCALPATH` | string |
| **Secrets** | | |
| `server.secrets.backend` | `SCION_SERVER_SECRETS_BACKEND` | string |
| `server.secrets.gcp_project_id` | `SCION_SERVER_SECRETS_GCPPROJECTID` | string |
| `server.secrets.gcp_credentials` | `SCION_SERVER_SECRETS_GCPCREDENTIALS` | string |
| **Logging** | | |
| `server.log_level` | `SCION_SERVER_LOG_LEVEL` | string |
| `server.log_format` | `SCION_SERVER_LOG_FORMAT` | string |

**Note on CORS env vars:** The current `envKeyToConfigKey()` function in `hub_config.go` does not have camelCase mappings for CORS-related fields (`corsEnabled`, `corsAllowedOrigins`, etc.). This means CORS settings cannot currently be overridden via environment variables. The versioned settings implementation should add these mappings or switch to snake_case koanf tags.

### 4.4 Agent Container Env Vars (runtime-injected)

These are environment variables injected into agent containers by the runtime/broker at startup. They are **not** settings — they are set programmatically and are documented here for completeness.

| Env Var | Set By | Description |
|---|---|---|
| `SCION_AGENT_NAME` | `agent/run.go`, `harness/generic.go` | Agent display name |
| `SCION_AGENT_ID` | `runtimebroker/handlers.go` | Hub UUID for the agent (hosted mode) |
| `SCION_TEMPLATE_NAME` | `agent/run.go` | Template used to create the agent |
| `SCION_BROKER_NAME` | `agent/run.go`, `runtimebroker/handlers.go` | Broker name (defaults to "local") |
| `SCION_CREATOR` | `agent/run.go`, `runtimebroker/handlers.go` | User who created the agent (OS user or Hub email) |
| `SCION_HOST_UID` | `runtime/common.go` | Host user ID (for file ownership mapping) |
| `SCION_HOST_GID` | `runtime/common.go` | Host group ID (for file ownership mapping) |
| `SCION_HUB_URL` | `runtimebroker/handlers.go` | Hub callback URL for agent status reporting (hosted mode) |
| `SCION_HUB_TOKEN` | `runtimebroker/handlers.go` | Hub auth token for agent callbacks (hosted mode) |
| `SCION_HOOKS_DIR` | (user-set or default) | Override path for agent lifecycle hooks directory |
| `SCION_GRACE_PERIOD` | (user-set) | Override container shutdown grace period |
| `SCION_MODEL` | Harness-specific | Model identifier used by the harness |
| `SCION_HARNESS` | Harness-specific | Harness type identifier |

### 4.5 Utility / Debug Env Vars

These are standalone environment variables used by the CLI and server binaries for debugging and operational purposes. They are not part of the settings schema.

| Env Var | Description |
|---|---|
| `SCION_DEBUG` | Enable debug logging when set to "1" |
| `SCION_LOG_LEVEL` | Override log level at runtime ("debug", "info", etc.) |
| `SCION_LOG_GCP` | Enable GCP Cloud Logging when set to "true" |
| `SCION_CLOUD_LOGGING` | Enable Cloud Logging integration |
| `SCION_CLOUD_LOGGING_LOG_ID` | Log ID for Cloud Logging |
| `SCION_GCP_PROJECT_ID` | GCP project ID (for Cloud Logging; priority over `GOOGLE_CLOUD_PROJECT`) |
| `SCION_HEADLESS` | Force headless mode when set to "1" (skips browser-open operations) |
| `SCION_GIT_BINARY` | Override path to the git binary |
| `SCION_DEV_TOKEN` | Dev auth token for Hub/Broker API client authentication |
| `SCION_DEV_TOKEN_FILE` | Path to file containing dev auth token (fallback: `~/.scion/dev-token`) |
| `SCION_HUB_STORAGE_BUCKET` | Override Hub storage bucket (used in `cmd/server.go`) |

### 4.6 LLM Provider Env Vars (pass-through)

These are discovered by harness implementations and passed into agent containers. They are not part of the settings schema but affect agent behavior.

| Env Var | Harness | Description |
|---|---|---|
| `GEMINI_API_KEY` | gemini | Gemini API key |
| `GOOGLE_API_KEY` | gemini | Alternative Google API key |
| `GOOGLE_APPLICATION_CREDENTIALS` | gemini | Path to GCP service account JSON |
| `GOOGLE_CLOUD_PROJECT` / `GCP_PROJECT` | gemini | GCP project ID |
| `GOOGLE_CLOUD_LOCATION` | gemini | GCP location for Vertex AI |
| `VERTEX_API_KEY` | gemini | Vertex AI API key |
| `ANTHROPIC_API_KEY` | claude, opencode, generic | Anthropic API key |
| `OPENAI_API_KEY` | opencode, codex, generic | OpenAI API key |
| `CODEX_API_KEY` | codex | Codex-specific API key |

---

## 5. Detection & Transition Strategy

### 5.1 How Legacy vs Versioned Settings Are Detected

```
if file contains "schema_version" key:
    → versioned settings: validate against declared schema, use new loader
else if file contains top-level "harnesses" key:
    → legacy settings (current format): load via legacy path, emit deprecation warning
else if file is empty or missing:
    → no settings: use embedded defaults (versioned format)
```

### 5.2 Legacy Compatibility Layer

A `LegacySettingsAdapter` converts legacy `Settings` into the new versioned structure:

```go
func AdaptLegacySettings(legacy *LegacySettings) (*VersionedSettings, []string) {
    // Returns adapted settings + list of deprecation warnings
    // Mapping:
    //   legacy.Harnesses → versioned.HarnessConfigs (name = harness type, harness = name)
    //   legacy.Bucket → see Open Question 2 (not 1:1 with server.storage)
    //   legacy.GroveID → preserved as-is
    //   legacy.DefaultTemplate → preserved as-is
    //   legacy.Hub.BrokerID → moved to server.runtime_broker.broker_id (with warning)
    //   legacy.Hub.BrokerNickname → moved to server.runtime_broker.broker_nickname
    //   legacy.Hub.BrokerToken → moved to server.runtime_broker.broker_token
    //   All other fields map 1:1
}
```

### 5.3 Deprecation Warning Format

```
WARNING: Legacy settings format detected in /path/to/settings.yaml
  The following fields are deprecated and will be removed in a future version:
    - "harnesses" → use "harness_configs" with explicit "harness" field
    - "bucket" → see migration guidance (run 'scion config migrate')
    - "hub.broker_id", "hub.broker_nickname", "hub.broker_token"
        → moved to "server.runtime_broker" (global settings only)
  Run 'scion config migrate' to automatically update your settings.
```

---

## 6. Phased Implementation Plan

### Phase 1: Schema Foundation

**Goal:** Introduce the JSON Schema, versioned settings struct, and detection/validation infrastructure without changing any runtime behavior.

**Deliverables:**
1. Create `pkg/config/schemas/settings-v1.schema.json` (the full schema from Section 3.5).
2. Create `pkg/config/schemas/agent-v1.schema.json`.
3. Embed schemas via `//go:embed` in a new `pkg/config/schema.go`.
4. Implement `DetectSettingsFormat(data []byte) (version string, isLegacy bool)` — inspects a settings file to determine if it's versioned or legacy.
5. Implement `ValidateSettings(data []byte, schemaVersion string) []ValidationError` — validates a settings file against its declared schema using an embedded JSON Schema validator.
6. Add a `scion config validate` command that validates the current effective settings and reports errors.
7. Write tests for schema validation with valid, invalid, and legacy input.

**No behavior changes.** Existing settings loading continues to use the legacy path.

### Phase 2: New Settings Structs & Loader

**Goal:** Implement the new Go structs and a parallel loading path that can load versioned settings files.

**Deliverables:**
1. Define `VersionedSettings` struct in `pkg/config/settings_v1.go` with all new groups (`Server`, `Hub`, `CLI`, `Runtimes`, `HarnessConfigs`, `Profiles`). **Use snake_case koanf tags consistently.**
2. Define `HarnessConfigEntry` struct (the `harness_configs` value type with its explicit `harness` field, plus new `model` and `args` fields).
3. Implement `LoadVersionedSettings(grovePath string) (*VersionedSettings, error)` using Koanf, loading with the same hierarchy (defaults → global → grove → env vars).
4. Implement `AdaptLegacySettings(legacy *Settings) (*VersionedSettings, []string)` that converts the current `Settings` struct to `VersionedSettings`, returning deprecation warnings.
5. Create a unified `LoadEffectiveSettings(grovePath string) (*VersionedSettings, []string, error)` that:
   - Detects format.
   - If versioned: validates and loads via the new path.
   - If legacy: loads via old path, adapts, emits warnings.
6. Update `pkg/config/embeds/default_settings.yaml` to use the versioned format (with `schema_version: "1"`).
7. Write comprehensive tests for both loading paths and the adapter.

**No consumer changes yet.** All existing code still uses the legacy `Settings` struct. The new loader exists but is not wired in.

### Phase 3: Consumer Migration — Core Resolution

**Goal:** Wire the new settings into the core resolution and provisioning paths.

**Deliverables:**
1. Add `ResolveHarnessConfig(profileName, harnessConfigName string) (HarnessConfigEntry, error)` to `VersionedSettings` — replaces `ResolveHarness` with support for named configs.
2. Add `ResolveRuntime(profileName string) (RuntimeConfig, string, error)` to `VersionedSettings` — same semantics, now uses `type` field.
3. Update `pkg/agent/provision.go` to accept `*VersionedSettings`. The function receives a `*VersionedSettings` and uses `ResolveHarnessConfig` instead of `ResolveHarness`.
4. Update `pkg/agent/run.go` to use `*VersionedSettings` for image, user, tmux resolution.
5. Update `cmd/create.go`, `cmd/start.go`, and other commands to call `LoadEffectiveSettings` and pass the result through.
6. Introduce a `--harness-config` flag to `scion create` (in addition to existing `--harness`) to select a named harness config.
7. Wire deprecation warnings to stderr output when legacy settings are detected.
8. Test that existing settings files (legacy format) produce identical behavior.

### Phase 4: Server Config Consolidation

**Goal:** Merge `server.yaml` / `GlobalConfig` into the unified settings under the `server` key.

**Deliverables:**
1. Update `LoadGlobalConfig` to check for `server` key in `settings.yaml` first, falling back to `server.yaml` for backward compatibility.
2. Add `ServerConfig` struct (mirrors current `GlobalConfig`) to `VersionedSettings`.
3. Map `SCION_SERVER_*` env vars to `server.*` paths in the unified Koanf loader.
4. When both `server.yaml` and `settings.yaml.server` exist, emit a warning and prefer `settings.yaml`.
5. Add `scion config migrate --server` to merge `server.yaml` into `settings.yaml`.
6. Update `scion server` and `scion broker` commands to read from the unified config.
7. Document that `server.yaml` is deprecated in favor of `settings.yaml` `server` section.
8. Fix CORS env var mappings — add camelCase entries to `envKeyToConfigKey()` or switch to snake_case tags.

### Phase 5: New Feature Gates

**Goal:** Implement features that are gated on versioned settings.

**Deliverables:**
1. **`max_turns`**: In the agent runner, check `scionConfig.MaxTurns`. Only available when `schema_version >= 1` in the agent template. If the agent's harness supports turn counting (requires harness-level support), enforce the limit by sending a stop signal.
2. **`max_duration`**: In the agent runner, start a timer based on `scionConfig.MaxDuration`. Terminate the agent container after the duration elapses. Only available when `schema_version >= 1`.
3. **`cli.interactive_disabled`**: Check this setting in interactive prompts (attach, confirmations). When `true`, skip prompts and use defaults or fail with an error.
4. **Named harness configs**: With `harness_configs` fully wired, users can create agents with `scion create --harness-config gemini-high-security myagent`.
5. **Runtime type field**: Runtimes with explicit `type` fields resolve correctly through the factory.

### Phase 6: Migration Tooling & Cleanup

**Goal:** Provide automated migration and remove legacy code paths.

**Deliverables:**
1. Implement `scion config migrate` command:
   - Reads legacy settings file.
   - Produces versioned settings file.
   - Backs up the original as `settings.yaml.bak`.
   - Reports changes made.
2. Implement `scion config migrate --server` to fold `server.yaml` into `settings.yaml`.
3. Implement `scion config migrate --dry-run` for preview.
4. Update documentation (`docs-site/`) with new settings reference.
5. After a release cycle, remove the `LegacySettingsAdapter` and legacy loading path (Phase 2 code), making versioned settings the only supported format.

---

## 7. File Layout Changes

### Before (legacy)
```
~/.scion/
  settings.yaml              # flat Settings struct
  server.yaml                # separate GlobalConfig
.scion/
  settings.yaml              # grove-level Settings
  templates/
    gemini/
      scion-agent.json       # agent config (no schema)
```

### After (versioned)
```
~/.scion/
  settings.yaml              # VersionedSettings with schema_version, includes server section
.scion/
  settings.yaml              # grove-level VersionedSettings (no server section)
  templates/
    gemini/
      scion-agent.yaml       # agent config with schema_version
```

---

## 8. Key Decisions

### 8.1 Why not separate files per group?

A single `settings.yaml` with clear top-level groups is simpler to manage than multiple files. The Koanf merge hierarchy (defaults → global → grove → env) already handles layering. Splitting into `hub.yaml`, `runtimes.yaml`, etc. would multiply the number of files users must manage and complicate the merge logic.

### 8.2 Why absorb `server.yaml`?

The server config shares infrastructure with settings (Koanf loading, env vars, YAML format). Having two separate files with two separate loading paths is a maintenance burden. The `server` key is scoped to global-only, so there is no ambiguity about where it can appear.

### 8.3 Why `harness_configs` instead of extending `harnesses`?

The current `harnesses` map is keyed by harness type name, enforcing a 1:1 relationship between name and type. The new `harness_configs` map breaks this constraint, allowing multiple configurations for the same harness type. This is a semantic change that warrants a new key name to avoid confusion during the transition.

### 8.4 Why integer versioning?

Semantic versioning (major.minor.patch) is overkill for a settings schema. A simple monotonic integer is sufficient. Each increment represents a set of additive changes. The schema itself uses `x-since` annotations to track which version introduced each field, and `x-deprecated-by` to track removals.

### 8.5 Why JSON Schema instead of Go-only validation?

JSON Schema is language-neutral and can be used by IDEs (via `$schema` in YAML) for autocompletion and validation. It serves as documentation, validation specification, and tooling integration in one artifact. Go code validates against it at runtime using an embedded validator library.

### 8.6 Why move broker identity from `hub` to `server.runtime_broker`?

Broker identity (`broker_id`, `broker_nickname`, `broker_token`) describes **this machine's** role as a compute broker. It is inherently per-machine, not per-grove. Placing it under `server.runtime_broker` (which is global-only) correctly scopes it. The previous placement under `hub` (which allows grove-level overrides) was a historical artifact of the broker registering through the hub client.

---

## 9. Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Legacy adapter produces different behavior than direct legacy loading | Agents behave differently after upgrade | Comprehensive comparison tests: load legacy file both ways, diff the resolved configs |
| Schema validation rejects valid-but-unusual settings | Blocks users on upgrade | `additionalProperties: false` is strict by design but the migrate command preserves all known fields. Unknown fields are reported as warnings, not errors, during the transition period |
| `server.yaml` users don't notice the deprecation | Two config files drift out of sync | Emit a deprecation warning on every server start when `server.yaml` exists |
| Named harness configs break profile override resolution | Wrong harness config selected | Profile `harness_overrides` keys match harness-config names, not harness types. Document this clearly |
| Koanf deep merge behavior changes between legacy and versioned structs | Subtle config differences | Test merge behavior exhaustively with multi-layer configs |
| Moving broker identity from `hub` to `server.runtime_broker` breaks save logic | Broker registration writes to wrong location | Migration adapter must detect and relocate these fields; `SaveSettings` must handle the new location |
| CORS env vars don't work in current code | Server CORS can't be configured via env | Add missing camelCase mappings or switch to snake_case (Phase 4 deliverable) |

---

## 10. Testing Strategy

### Unit Tests
- Schema validation: valid v1 file passes, missing required fields fail, unknown fields fail.
- Legacy detection: files with/without `schema_version` classified correctly.
- Legacy adapter: every field in `Settings` maps correctly to `VersionedSettings`.
- Resolution: `ResolveHarnessConfig` with default names, named variants, profile overrides.
- Env var mapping: every `x-env-var` in the schema is honored by the Koanf loader.
- Broker identity migration: fields move from `hub` to `server.runtime_broker` correctly.

### Integration Tests
- Round-trip: write a `VersionedSettings` to YAML, reload it, compare.
- Migration: take a legacy `settings.yaml`, run the adapter, validate the output against the schema.
- Feature gates: `max_duration` and `max_turns` are only active when `schema_version >= 1`.
- Server consolidation: `server` key in `settings.yaml` produces the same `GlobalConfig` as a standalone `server.yaml`.

### Compatibility Tests
- The default embedded settings (upgraded to versioned format) produce the same resolved configs as the current embedded defaults.
- Existing grove-level settings (legacy format) work without modification and emit a deprecation warning.

---

## 11. Open Questions

### OQ-1: Should `hub.token` be renamed to `hub.dev_token`?

**Context:** The current `hub.token` / `SCION_HUB_TOKEN` stores a bearer token for Hub authentication. In practice, this is typically a dev token issued by the server's dev auth system. The original feedback suggested renaming it to `dev_token` to make this explicit.

**Arguments for renaming:**
- Clarifies that this is for development/manual auth, not production OAuth flows.
- Avoids confusion with `server.auth.dev_token` (the server-side dev token).
- Makes it clear that production deployments should use OAuth, not static tokens.

**Arguments against:**
- The field could hold *any* bearer token (including service account tokens or personal access tokens if those are added later). `dev_token` is too restrictive.
- Breaking change: `SCION_HUB_TOKEN` is widely used and documented.
- Creates a second `dev_token` field alongside `server.auth.dev_token`, which may be more confusing.

**Recommendation:** Keep as `token` with documentation clarifying its typical use as a dev auth token. Revisit when a formal access token mechanism is added.

### OQ-2: What is the migration path for `Settings.Bucket`?

**Context:** The current `Settings` struct has a top-level `Bucket` field (`BucketConfig` with `Provider`, `Name`, `Prefix`) that configures cloud storage for agent workspace persistence. The design doc originally proposed moving this to `server.storage`. However, `server.storage` (`StorageConfig` with `Provider`, `Bucket`, `LocalPath`) serves a different purpose — it's the server's template/asset storage backend.

**Differences:**
- `Settings.Bucket`: Client/grove-level. Used for persisting agent workspace data to GCS.
- `GlobalConfig.Storage`: Server-level. Used for storing templates and assets.

**Options:**
1. Keep `bucket` as a separate top-level setting in versioned settings.
2. Move it into a new `storage` section at the grove level (distinct from `server.storage`).
3. Fold it into the `volumes` system with GCS volume mounts.
4. Drop it entirely if the feature is underused.

**Status:** Needs investigation into actual usage patterns before deciding.

### OQ-3: How should `hub.last_synced_at` be handled as runtime state?

**Context:** `last_synced_at` is not a user-configured setting — it's runtime state written back to the settings file by the sync logic. Mixing runtime state with configuration in the same file is an anti-pattern that complicates validation and editing.

**Options:**
1. Keep it in `settings.yaml` (status quo, simple, but impure).
2. Move it to a separate state file (e.g., `~/.scion/state.yaml` or `.scion/state.yaml`).
3. Store it in a lightweight local database or key-value store.

**Recommendation:** Keep in settings for now (pragmatic), but add `"x-runtime-managed": true` annotation in the schema to indicate this field shouldn't be manually edited. Consider extracting runtime state to a separate file if more runtime-managed fields are added in the future.

### OQ-4: Env var naming for the `server.hub.public_url` rename

**Context:** The current env var `SCION_SERVER_HUB_ENDPOINT` maps to the `hub.endpoint` field in `GlobalConfig.Hub`. The design doc renames this to `server.hub.public_url`. Should the env var also be renamed?

**Options:**
1. Keep `SCION_SERVER_HUB_ENDPOINT` for backward compatibility (document that it maps to `public_url`).
2. Rename to `SCION_SERVER_HUB_PUBLIC_URL` and add a deprecation shim for the old name.
3. Support both during the transition period.

**Recommendation:** Keep `SCION_SERVER_HUB_ENDPOINT` (option 1). The env var doesn't need to match the YAML field name exactly, and changing env vars is more disruptive than changing YAML keys.

### OQ-5: Should `server.runtime_broker` be shortened to `server.broker`?

**Context:** The current struct is `RuntimeBrokerConfig` and the YAML key is `runtimeBroker` / `runtime_broker`. This is verbose. The feedback originally suggested "broker" as the section name.

**Arguments for `server.broker`:** Shorter, cleaner, matches common usage (users say "broker" not "runtime broker").

**Arguments for `server.runtime_broker`:** Maintains consistency with the current naming. Distinguishes from other possible broker types if the concept evolves.

**Status:** Low priority. Can be decided during Phase 4 implementation.

### OQ-6: CORS env var mapping is broken in current code

**Context:** The `envKeyToConfigKey()` function in `hub_config.go` maps env var suffixes to koanf keys. It has camelCase mappings for fields like `clientId`, `brokerId`, `devMode`, etc. However, CORS-related fields (`corsEnabled`, `corsAllowedOrigins`, `corsAllowedMethods`, `corsAllowedHeaders`, `corsMaxAge`) are NOT in the mapping table.

This means env vars like `SCION_SERVER_HUB_CORSENABLED` would produce the koanf key `hub.corsenabled` instead of `hub.corsEnabled`, silently failing to override the setting.

**Resolution:** This is a pre-existing bug that should be fixed regardless of the versioned settings refactor. Either:
1. Add the missing camelCase entries to `envKeyToConfigKey()`.
2. Switch the `GlobalConfig` struct's koanf tags to snake_case (aligned with the versioned settings approach).

Option 2 is preferred and should be done as part of Phase 4.

### OQ-7: Inconsistency between `SCION_HUB_URL` and `SCION_HUB_ENDPOINT`

**Context:** Two different env vars exist for what appears to be the same concept:
- `SCION_HUB_ENDPOINT`: Used in `settings.yaml` loading (`koanf.go`) to override `hub.endpoint`.
- `SCION_HUB_URL`: Used in `runtimebroker/handlers.go` and `sciontool` to tell agents where the Hub is.

`SCION_HUB_URL` is injected into agent containers at runtime (Section 4.4) and is read by `sciontool` inside the container. `SCION_HUB_ENDPOINT` is a settings override on the host.

**Resolution:** These serve different purposes (host-side setting override vs container-injected runtime config) but the naming is confusing. Document clearly and consider aligning in a future version. The versioned settings should keep `SCION_HUB_ENDPOINT` for the client setting and `SCION_HUB_URL` for the container injection.
