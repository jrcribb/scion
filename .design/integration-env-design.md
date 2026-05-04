# Integration Environment for Scion-on-Scion Development

**Status:** Draft  
**Date:** 2026-05-04  
**Related:** [automated-qa.md](automated-qa.md), [mint-gcp-svc-accounts.md](mint-gcp-svc-accounts.md), [agent-credentials.md](agent-credentials.md), [sciontool-auth](hosted/auth/sciontool-auth.md), [user-access-tokens.md](user-access-tokens.md), [hosted-architecture.md](hosted/hosted-architecture.md)

---

## 1. Problem

Scion agents that develop Scion itself need a realistic integration environment. Today, agents can run unit tests and build binaries, but they cannot exercise the full platform end-to-end: hub API calls, broker dispatch, agent provisioning, GCP credential flows, and multi-agent coordination. Without this, large story-arc feature work — the kind that spans multiple agents over hours or days — is limited to mocking or manual verification.

Three specific gaps block true integration testing:

1. **No GCP identity for SSH.** Agents need a GCP service account that grants SSH access to a running Scion hub instance, so they can operate against real infrastructure rather than localhost-only stubs.
2. **No access to signing keys.** Agents need to generate valid hub API tokens (JWTs and UATs) programmatically. This requires access to the hub's signing key — either directly or through a controlled token-minting API.
3. **No long-horizon test harness.** Feature development that touches multiple subsystems (e.g., adding a new credential flow end-to-end) needs a persistent environment that survives across agent sessions and supports incremental verification.

---

## 2. Goals

- Agents working on Scion can run realistic end-to-end integration tests against a live hub.
- GCP service account provisioning is automated and scoped — agents get exactly the permissions they need.
- Token generation for hub API access is safe, auditable, and does not leak the hub's root signing key.
- The environment supports long-running, multi-session feature arcs: an agent can make changes, test them, hand off to another agent, and the environment persists.
- The integration environment is isolated from production and from other developers' work.

## 3. Non-Goals

- Replacing unit tests or the existing M1/M2 test methodologies from `automated-qa.md`.
- Providing agents with broad GCP IAM permissions beyond what integration testing requires.
- Multi-tenant hub sharing across unrelated teams (this is a single-team development environment).
- Automated CI/CD pipeline integration (future work; this doc covers the agent-interactive environment).

---

## 4. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Integration Environment                          │
│                                                                      │
│  ┌─────────────┐      SSH (GCP SA)      ┌──────────────────────┐    │
│  │ Scion Agent │ ──────────────────────► │   Integration Hub    │    │
│  │ (developer) │                         │   (GCE VM or GKE)   │    │
│  │             │ ◄─── Hub API (JWT) ───► │                      │    │
│  └─────────────┘                         │  ┌────────────────┐  │    │
│        │                                 │  │ Signing Keys   │  │    │
│        │ scion CLI                       │  │ (GCP SM)       │  │    │
│        │                                 │  └────────────────┘  │    │
│        ▼                                 │  ┌────────────────┐  │    │
│  ┌─────────────┐      Control Channel    │  │ Token Mint     │  │    │
│  │  Runtime    │ ◄────── WebSocket ────► │  │ Endpoint       │  │    │
│  │  Broker     │                         │  └────────────────┘  │    │
│  │  (Docker)   │                         │  ┌────────────────┐  │    │
│  └─────────────┘                         │  │ SQLite/PG      │  │    │
│        │                                 │  └────────────────┘  │    │
│        ▼                                 └──────────────────────┘    │
│  ┌─────────────┐                                                     │
│  │ Test Agents │  (provisioned by broker, exercise real flows)       │
│  └─────────────┘                                                     │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │                    GCP Project: scion-integration            │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │    │
│  │  │ Hub SA       │  │ Agent SAs    │  │ Secret Manager   │  │    │
│  │  │ (runs hub)   │  │ (per-grove)  │  │ (signing keys)   │  │    │
│  │  └──────────────┘  └──────────────┘  └──────────────────┘  │    │
│  └──────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────┘
```

The integration environment consists of:

1. **A dedicated GCP project** (`scion-integration` or similar) hosting the hub VM, service accounts, and Secret Manager.
2. **An integration hub** running on a GCE VM (or GKE pod), configured with GCP Secret Manager for signing key storage.
3. **Agent service accounts** minted by the hub or pre-provisioned, granting SSH access to the hub VM and scoped API permissions.
4. **A runtime broker** (co-located or remote) that provisions test agents for end-to-end lifecycle testing.

---

## 5. Detailed Design

### 5.1 GCP Service Account for Agent SSH Access

**Objective:** Give Scion development agents a GCP identity that allows SSH into the integration hub VM.

#### Service Account Structure

| Service Account | Purpose | Key Permissions |
|----------------|---------|-----------------|
| `scion-integ-hub@<project>.iam` | Runs the hub server | `secretmanager.versions.access`, `iam.serviceAccountCreator`, `iam.serviceAccountTokenCreator` |
| `scion-integ-agent@<project>.iam` | Shared agent identity for SSH + API | `compute.instances.osLogin`, `iap.tunnelInstances.accessViaIAP` (if using IAP) |
| `scion-integ-agent-<N>@<project>.iam` | Per-agent identity (optional, higher isolation) | Same as above, scoped per agent |

#### SSH Access Mechanism

Two options, depending on network topology:

**Option A: OS Login (recommended)**
- Enable OS Login on the hub VM (`enable-oslogin = TRUE` in instance metadata).
- Grant the agent SA `roles/compute.osLogin` on the project (or instance-level).
- Agents authenticate with their SA key and use `gcloud compute ssh` or raw SSH with OS Login certificates.
- Advantage: No SSH key management, automatic POSIX user mapping, audit logging via Cloud Audit Logs.

**Option B: IAP TCP Tunneling**
- Hub VM has no public IP; SSH goes through Identity-Aware Proxy.
- Grant the agent SA `roles/iap.tunnelResourceAccessor` on the tunnel.
- Agents use `gcloud compute ssh --tunnel-through-iap`.
- Advantage: Zero public attack surface, firewall rules are irrelevant.
- Recommended to combine with Option A for identity resolution.

#### SA Key Distribution

Agent containers receive their GCP SA credentials through the existing credential resolution pipeline (`pkg/harness/auth.go`):

1. **Hub-minted SA flow** (preferred): Hub mints an SA in the integration project using the existing `POST /api/v1/groves/{groveId}/gcp-service-accounts/mint` endpoint. The hub already has `roles/iam.serviceAccountTokenCreator` on minted SAs, so it can generate short-lived access tokens via impersonation. No long-lived keys required.

2. **Injected via environment**: The integration grove's environment is configured with `GOOGLE_APPLICATION_CREDENTIALS` pointing to a short-lived credential file refreshed by `sciontool` at start time. The existing `gcp_token.go` impersonation flow handles this.

3. **Workload Identity (GKE only)**: If the broker runs on GKE, the agent pod's Kubernetes SA is bound to the GCP SA via Workload Identity Federation. No key files at all.

#### Required IAM Bindings

```bash
# Hub SA can mint agent SAs and generate tokens for them
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-hub@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountCreator"

gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-hub@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountTokenCreator"

# Agent SA can SSH via OS Login
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-agent@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/compute.osLogin"

# Agent SA can tunnel through IAP (if using Option B)
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-agent@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/iap.tunnelResourceAccessor"
```

### 5.2 Signing Key Access for Token Generation

**Objective:** Enable agents to generate valid hub API tokens without exposing the raw signing key material.

The hub uses HS256 JWTs signed with `agent_signing_key` and `user_signing_key`, stored in GCP Secret Manager (production) or SQLite (development). Agents need to call hub APIs authenticated as either a user (for management operations) or an agent (for lifecycle operations). Three approaches, in order of preference:

#### Approach A: Token Minting via UAT-Scoped Endpoint (Recommended)

Use the existing User Access Token (UAT) system. A human operator (or bootstrap script) creates a long-lived UAT scoped to the integration grove with `agent:manage` permissions:

```bash
scion hub token create \
  --grove integration-grove \
  --name "integration-agent-token" \
  --scopes agent:manage,grove:read \
  --expires 90d
# Returns: scion_pat_<token>
```

This UAT is stored as a grove-level secret and injected into agent environments as `SCION_HUB_TOKEN`. The agent uses the Scion CLI or direct API calls, authenticated by the UAT. The hub validates the UAT against its hashed store — no signing key access required on the agent side.

**Pros:**
- No signing key exposure. The agent never sees key material.
- Uses existing, implemented infrastructure (UAT creation, validation, scoping).
- Scoped to exactly the permissions integration tests need.
- Revocable and rotatable without hub restart.

**Cons:**
- UATs have a maximum 365-day lifetime; requires periodic rotation.
- Cannot mint arbitrary agent JWTs (only hub can do that at dispatch time).
- UAT cannot create other UATs (enforced at middleware), so sub-agents need separate provisioning.

#### Approach B: Signing Key Access via GCP Secret Manager

Grant the agent SA `secretmanager.versions.access` on the specific signing key secrets:

```bash
# Grant access to agent signing key only
gcloud secrets add-iam-policy-binding "agent_signing_key" \
  --project=$PROJECT \
  --member="serviceAccount:scion-integ-agent@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

The agent can then read the signing key and mint its own JWTs using the same `AgentTokenService` code from `pkg/hub/agenttoken.go`.

**Pros:**
- Full flexibility: agent can mint tokens with any scope, lifetime, or claims.
- Useful for testing token validation edge cases (expired tokens, wrong scopes, etc.).

**Cons:**
- Exposes root signing key material to agent containers. A compromised agent could forge tokens for any identity.
- Requires the agent to embed token-minting logic, duplicating hub internals.
- Key rotation requires coordinating between hub and all agents.

**Mitigation if chosen:** Use a dedicated integration signing key (separate from production). The integration hub is configured with its own key stored under a distinct Secret Manager path. Even if compromised, it cannot forge tokens for the production hub.

#### Approach C: Dedicated Token Mint Endpoint on Integration Hub

Add a new hub API endpoint available only in integration/dev mode:

```
POST /api/v1/dev/mint-token
Authorization: Bearer <UAT with admin scope>

{
  "type": "agent",           // or "user"
  "agent_id": "test-agent-1",
  "grove_id": "integration-grove",
  "scopes": ["agent:status:update", "grove:secret:read"],
  "lifetime": "2h"
}
```

The hub signs the token internally and returns it. This gives agents flexible token minting without key exposure.

**Pros:**
- No key exposure, full token flexibility.
- Hub maintains control over all minted tokens (audit log, rate limiting).
- Can enforce policy (max lifetime, allowed scopes) at the endpoint level.

**Cons:**
- New code to write and maintain.
- Only available on integration/dev hubs (gated by `--dev-auth` or similar flag).

#### Recommendation

**Use Approach A (UAT) as the primary mechanism.** It requires zero new code, provides sufficient permissions for most integration test scenarios, and keeps the signing key contained within the hub process.

**Add Approach C (Mint Endpoint) as a follow-up** for advanced test scenarios that need custom token claims (e.g., testing token expiry handling, scope enforcement, or agent impersonation). Gate it behind `--dev-auth` mode so it never appears on production hubs.

**Reserve Approach B (direct key access) for dedicated security testing** only, and only with integration-specific keys that have no relationship to production.

### 5.3 Long-Horizon Feature Development Environment

**Objective:** Support multi-session, multi-agent feature arcs with realistic end-to-end testing.

#### Environment Lifecycle

The integration environment is **persistent** — it survives across agent sessions. An agent working on a feature can:

1. Build a new Scion binary from their branch.
2. Deploy it to the integration hub (or a staging slot).
3. Run integration tests against the updated hub.
4. Hand off to another agent (or resume later) with the environment intact.

#### Environment States

```
┌─────────┐     deploy      ┌──────────┐     test       ┌──────────┐
│  IDLE   │ ──────────────► │ DEPLOYED │ ─────────────► │ TESTING  │
│         │                 │          │                 │          │
└─────────┘                 └──────────┘                 └──────────┘
     ▲                           │                            │
     │         reset             │         teardown           │
     └───────────────────────────┴────────────────────────────┘
```

- **IDLE:** Hub is running with the last stable build. Signing keys and database are intact. Groves, agents, and test data from prior sessions persist.
- **DEPLOYED:** An agent has deployed a new binary. The hub is running the agent's branch build. Database state carries over (migrations run automatically).
- **TESTING:** Integration tests are executing against the deployed build. Test agents are provisioned and exercising real flows.

#### Persistent State

| State | Storage | Survives Restart | Survives Redeploy |
|-------|---------|-----------------|-------------------|
| Signing keys | GCP Secret Manager | Yes | Yes |
| Hub database | Persistent disk (GCE) or PVC (GKE) | Yes | Yes |
| Grove configs | Database | Yes | Yes |
| Agent containers | Ephemeral | No | No |
| Test results | GCS bucket | Yes | Yes |

#### Integration Grove Setup

A dedicated grove is pre-configured on the integration hub:

```yaml
# Integration grove configuration
grove:
  name: "scion-integration"
  git_remote: "https://github.com/anthropics/scion.git"

# Pre-provisioned resources
service_accounts:
  - scion-integ-agent  # Hub-minted, OS Login enabled

# Environment variables (stored as grove secrets)
secrets:
  SCION_HUB_TOKEN: "<UAT with agent:manage scope>"
  SCION_HUB_URL: "https://integ-hub.scion.internal"

# Templates available for test agents
templates:
  - claude-integration  # Claude harness with integration auth
  - generic-test        # Minimal harness for lifecycle tests
```

#### Test Scenario Categories

Building on the M1–M5 methodologies from `automated-qa.md`, the integration environment adds:

**M6: Agent-Driven Integration Hub Testing**

Full end-to-end tests that require a real hub with real GCP services:

| Category | What It Tests | Example |
|----------|--------------|---------|
| **Credential Flows** | GCP SA minting, impersonation, token injection | Mint SA → assign to grove → start agent → verify agent has GCP token |
| **Multi-Agent Lifecycle** | Agent creates sub-agents, message passing | Parent agent dispatches child → sends message → child responds → parent verifies |
| **Hub API Surface** | All CRUD endpoints with real auth | Create grove → configure secrets → push template → dispatch agent → attach → stop → delete |
| **Persistence** | Data survives hub restart | Create resources → restart hub → verify resources still exist |
| **Auth Edge Cases** | Token expiry, scope enforcement, revocation | Use expired token → expect 401; use wrong scope → expect 403; revoke UAT → expect rejection |
| **Template Sync** | Template push/pull with real GCS | Push template → pull on different broker → verify identical |
| **WebSocket Stability** | Control channel reconnection under load | Kill broker connection → verify auto-reconnect → verify agent state consistent |

#### Test Runner Architecture

```
┌──────────────────────────────────────────────────┐
│                 Test Runner Agent                  │
│                                                    │
│  1. Build scion binary from current branch        │
│  2. Deploy to integration hub (scp + restart)     │
│  3. Wait for hub healthy (GET /api/v1/health)     │
│  4. Execute test scenarios sequentially            │
│  5. Collect results + logs                         │
│  6. Report pass/fail summary                       │
│  7. Optionally roll back to stable build           │
│                                                    │
│  Test scenarios are Go test files or shell scripts │
│  in hack/integration/ that exercise the hub API    │
│  using the scion CLI and curl.                     │
└──────────────────────────────────────────────────┘
```

#### Deploy Flow

An agent deploys their branch to the integration hub:

```bash
# 1. Build for the hub's architecture
GOOS=linux GOARCH=amd64 make build

# 2. Copy binary to hub VM
gcloud compute scp ./scion integ-hub:~/scion-staging \
  --zone=us-central1-a

# 3. SSH in and swap the binary
gcloud compute ssh integ-hub --zone=us-central1-a -- \
  'sudo systemctl stop scion-hub && \
   sudo cp ~/scion-staging /usr/local/bin/scion && \
   sudo systemctl start scion-hub'

# 4. Wait for healthy
until curl -sf https://integ-hub.scion.internal/api/v1/health; do
  sleep 2
done
```

#### Concurrency and Locking

Multiple agents should not deploy to the integration hub simultaneously. Use a simple advisory lock:

```bash
# Acquire lock (stored as a grove secret or GCS object)
scion hub env set INTEG_LOCK_HOLDER="agent-$(scion agent id)" \
  --grove integration-grove

# Before deploying, check lock
HOLDER=$(scion hub env get INTEG_LOCK_HOLDER --grove integration-grove)
if [ -n "$HOLDER" ] && [ "$HOLDER" != "agent-$(scion agent id)" ]; then
  echo "Integration env locked by $HOLDER — skipping deploy"
  exit 1
fi
```

A more robust approach uses a GCS object with generation-match preconditions for atomic compare-and-swap, or a hub API endpoint that manages the lock.

### 5.4 Bootstrap Procedure

One-time setup to create the integration environment from scratch:

```bash
#!/bin/bash
# bootstrap-integration-env.sh

PROJECT="scion-integration"
ZONE="us-central1-a"
VM_NAME="integ-hub"

# 1. Create GCP project (or use existing)
gcloud projects create $PROJECT --name="Scion Integration" 2>/dev/null

# 2. Enable required APIs
gcloud services enable \
  compute.googleapis.com \
  iam.googleapis.com \
  secretmanager.googleapis.com \
  iap.googleapis.com \
  --project=$PROJECT

# 3. Create hub service account
gcloud iam service-accounts create scion-integ-hub \
  --display-name="Scion Integration Hub" \
  --project=$PROJECT

# 4. Grant hub SA its required roles
for ROLE in roles/iam.serviceAccountCreator \
            roles/iam.serviceAccountTokenCreator \
            roles/secretmanager.admin; do
  gcloud projects add-iam-policy-binding $PROJECT \
    --member="serviceAccount:scion-integ-hub@${PROJECT}.iam.gserviceaccount.com" \
    --role="$ROLE"
done

# 5. Create agent service account
gcloud iam service-accounts create scion-integ-agent \
  --display-name="Scion Integration Agent" \
  --project=$PROJECT

# 6. Grant agent SA SSH access
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-agent@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/compute.osLogin"

gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:scion-integ-agent@${PROJECT}.iam.gserviceaccount.com" \
  --role="roles/iap.tunnelResourceAccessor"

# 7. Create hub VM
gcloud compute instances create $VM_NAME \
  --project=$PROJECT \
  --zone=$ZONE \
  --machine-type=e2-standard-4 \
  --service-account="scion-integ-hub@${PROJECT}.iam.gserviceaccount.com" \
  --scopes=cloud-platform \
  --metadata=enable-oslogin=TRUE \
  --boot-disk-size=50GB \
  --tags=scion-hub

# 8. Create firewall rule for IAP SSH
gcloud compute firewall-rules create allow-iap-ssh \
  --project=$PROJECT \
  --direction=INGRESS \
  --action=ALLOW \
  --rules=tcp:22 \
  --source-ranges=35.235.240.0/20 \
  --target-tags=scion-hub

# 9. Install and start hub on VM
gcloud compute ssh $VM_NAME --zone=$ZONE --project=$PROJECT -- bash -s <<'REMOTE'
  # Install scion binary (from latest stable build or GCS)
  sudo mkdir -p /usr/local/bin
  # ... copy binary ...

  # Create systemd service
  sudo tee /etc/systemd/system/scion-hub.service <<EOF
[Unit]
Description=Scion Integration Hub
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/scion server start --foreground --enable-hub --dev-auth
Restart=always
RestartSec=5
Environment=SCION_HUB_GCP_PROJECT=scion-integration

[Install]
WantedBy=multi-user.target
EOF

  sudo systemctl daemon-reload
  sudo systemctl enable --now scion-hub
REMOTE

# 10. Create integration grove and UAT
HUB_URL="http://$(gcloud compute instances describe $VM_NAME \
  --zone=$ZONE --project=$PROJECT \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)'):8080"

# Using dev-auth for initial setup
export SCION_HUB_TOKEN="scion_dev_integration"
export SCION_HUB_URL="$HUB_URL"

scion hub grove create --name "scion-integration"
scion hub token create \
  --grove scion-integration \
  --name "agent-integration-token" \
  --scopes agent:manage,grove:read \
  --expires 90d
```

---

## 6. Security Considerations

### Isolation

- The integration GCP project is **separate from production**. No IAM bindings cross project boundaries.
- The integration hub uses its own signing keys, stored in the integration project's Secret Manager. Tokens minted here are invalid on production hubs.
- Agent SAs are permissionless beyond SSH and hub API access. They cannot access production GCP resources.

### Credential Lifecycle

| Credential | Lifetime | Rotation | Revocation |
|-----------|----------|----------|------------|
| Hub signing keys | Indefinite (hub-managed) | Hub restart with new key; old tokens invalidated | Delete from Secret Manager |
| Agent SA keys | Short-lived (impersonation) | Automatic via token refresh | Remove SA or IAM binding |
| UAT (hub API) | 90 days (configurable) | Create new token, revoke old | `scion hub token revoke` |
| SSH access | Session-scoped (OS Login) | N/A (ephemeral certificates) | Remove `compute.osLogin` role |

### Blast Radius

If the integration environment is compromised:
- **Agent SA compromised:** Attacker gets SSH to the integration hub and API access scoped to the integration grove. No production access. Revoke the SA to contain.
- **Signing key compromised:** Attacker can forge tokens for the integration hub only. Rotate the key in Secret Manager and restart the hub.
- **UAT compromised:** Attacker can manage agents in the integration grove. Revoke the UAT via CLI or API.

### Audit

- All SSH access logged via Cloud Audit Logs (OS Login).
- All hub API calls logged by the hub's request logger (including token identity).
- GCP Secret Manager access logged in Cloud Audit Logs.
- UAT usage tracked via `last_used` timestamp in the hub database.

---

## 7. Implementation Phases

### Phase 1: Foundation (1–2 weeks)

**Deliverables:**
- GCP project and hub VM provisioned with the bootstrap script.
- Hub running with GCP Secret Manager for signing keys.
- Agent SA created with OS Login SSH access.
- UAT created and stored as grove secret.
- Agent template configured with `SCION_HUB_TOKEN` and `SCION_HUB_URL`.
- Smoke test: agent SSHs to hub, calls `GET /api/v1/health`, creates and deletes a test agent.

**Verification:**
```bash
# From an agent container with the integration SA
gcloud compute ssh integ-hub --tunnel-through-iap --zone=us-central1-a
# Should succeed with OS Login

# API access via UAT
curl -H "Authorization: Bearer $SCION_HUB_TOKEN" \
  $SCION_HUB_URL/api/v1/health
# Should return 200
```

### Phase 2: Test Suite (2–3 weeks)

**Deliverables:**
- `hack/integration/` directory with test scenarios organized by category.
- Test runner script that deploys a branch build and executes the suite.
- Coverage of core scenarios: agent lifecycle, credential flows, template sync.
- Results written to a GCS bucket with run ID, timestamp, and pass/fail per scenario.

**Test structure:**
```
hack/integration/
├── run.sh                    # Orchestrator: deploy, test, report
├── lib/                      # Shared helpers (auth, assertions, cleanup)
│   ├── auth.sh
│   ├── assert.sh
│   └── cleanup.sh
├── 01-health/                # Hub health and basic connectivity
│   └── test.sh
├── 02-agent-lifecycle/       # Create, start, stop, delete agents
│   └── test.sh
├── 03-credential-flows/      # GCP SA minting, token injection
│   └── test.sh
├── 04-multi-agent/           # Parent-child agent coordination
│   └── test.sh
├── 05-persistence/           # Data survives restart
│   └── test.sh
└── 06-auth-edge-cases/       # Expired tokens, wrong scopes
    └── test.sh
```

### Phase 3: Long-Horizon Support (1–2 weeks)

**Deliverables:**
- Deploy locking mechanism (GCS-based or hub API).
- Environment state tracking (which branch is deployed, by whom, since when).
- Rollback script to restore last known stable build.
- Documentation for agents on how to use the integration environment in multi-session feature arcs.

### Phase 4: Token Mint Endpoint (Optional, 1 week)

**Deliverables:**
- `POST /api/v1/dev/mint-token` endpoint, gated behind `--dev-auth`.
- Supports minting agent and user JWTs with configurable claims.
- Rate-limited and audit-logged.
- Test scenarios for auth edge cases using minted tokens.

---

## 8. Open Questions

1. **VM vs GKE:** Should the integration hub run on a standalone GCE VM (simpler, cheaper) or a GKE pod (closer to production topology)? Recommendation: start with GCE VM for simplicity, migrate to GKE when multi-broker testing becomes a priority.

2. **Shared vs per-agent environments:** Should each agent get its own integration hub, or share one? Recommendation: share one with deploy locking. Per-agent hubs waste resources and diverge from the real multi-user scenario we want to test.

3. **Database reset policy:** Should the database be wiped between test runs, or should tests be idempotent? Recommendation: tests should be idempotent (create with unique names, clean up after). A `reset` command is available for when the database gets into a bad state.

4. **Branch deploy strategy:** When an agent deploys their branch, should the hub run their modified binary exclusively, or should we support A/B testing (two hubs, one stable, one experimental)? Recommendation: single hub with rollback capability. A/B adds complexity without clear value at this stage.

5. **Hub SA scope:** Should the hub SA also have `roles/secretmanager.admin` or just `roles/secretmanager.secretAccessor`? Admin is needed for initial key creation but is over-privileged for steady state. Recommendation: use admin during bootstrap, downgrade to accessor afterward, and use a separate admin SA for key rotation.
