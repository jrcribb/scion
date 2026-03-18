# GitHub App Integration for Scion Agents

**Created:** 2026-03-18
**Status:** Draft / Proposal
**Related:** `hosted/git-groves.md`, `hosted/secrets-gather.md`, `agent-credentials.md`, `hosted/auth/oauth-setup.md`

---

## 1. Overview

Today, Scion agents authenticate to GitHub using **Personal Access Tokens (PATs)** stored as secrets (`GITHUB_TOKEN`). This works but has significant limitations:

- **PATs are user-scoped**: Tied to a single person's identity. If that person leaves or rotates credentials, all groves using their token break.
- **No automatic rotation**: PATs have fixed expiration. When they expire, agents fail until someone manually updates the secret.
- **Coarse permission model**: Fine-grained PATs can be scoped to repos, but the permissions are static — there's no way to issue narrower tokens per-agent or per-operation.
- **Attribution**: All commits and API calls appear as the PAT owner, not as the agent or the system.
- **Organization governance**: Org admins have limited visibility into which PATs access their repos and no central revocation mechanism.

**GitHub Apps** address all of these issues. This document proposes a design for integrating GitHub App authentication into Scion as a first-class alternative to PATs.

### Goals

1. Support GitHub App installation tokens as a credential source for agent git operations (clone, push) and GitHub API access (PRs, issues).
2. Automatic short-lived token generation — no manual rotation required.
3. Clear ownership model: who registers the app, who installs it, how installations map to groves.
4. Coexist with the existing PAT flow — GitHub App is an alternative, not a replacement.

### Non-Goals

- Webhook-driven automation (GitHub App receiving events to trigger agent creation). Deferred to a future design.
- GitHub App as a Scion Hub user authentication provider (the existing GitHub OAuth flow handles Hub login separately).
- Multi-provider abstraction (GitLab, Bitbucket app equivalents). This design targets GitHub only.
- GitHub App Manifest flow for automated app creation.

---

## 2. GitHub App Primer

### 2.1 What Is a GitHub App?

A GitHub App is a first-class integration registered on GitHub. Unlike OAuth Apps or PATs, a GitHub App:

- Has its **own identity** separate from any user.
- Is **installed** on organizations or user accounts, granting it access to specific repositories.
- Authenticates using a **private key** (RSA) to generate short-lived JWTs, which are exchanged for **installation access tokens**.
- Has **fine-grained permissions** declared at registration time (e.g., Contents: read/write, Pull Requests: read/write, Issues: read/write).
- Can further **restrict tokens to specific repositories** at token creation time.

### 2.2 Authentication Flow

```
                GitHub App (registered)
                     |
                     | Private Key (PEM)
                     v
            ┌─────────────────┐
            │  Generate JWT   │  (signed with private key, 10-min expiry)
            │  (app identity) │
            └────────┬────────┘
                     |
                     v
            ┌─────────────────┐
            │  POST /app/     │  (JWT as Bearer token)
            │  installations/ │
            │  {id}/access_   │
            │  tokens         │
            └────────┬────────┘
                     |
                     v
            ┌─────────────────┐
            │ Installation    │  (scoped to repos, 1-hour expiry)
            │ Access Token    │
            └─────────────────┘
```

1. **JWT Generation**: The app signs a JWT using its private key. The JWT identifies the app (by App ID) and expires in 10 minutes.
2. **Token Request**: The JWT is used to call `POST /app/installations/{installation_id}/access_tokens`, optionally scoping to specific repositories and permissions.
3. **Installation Token**: GitHub returns a token (format `ghs_xxx`) valid for 1 hour. This token is used for git operations and API calls.

### 2.3 Installation Model

A GitHub App can be installed on:

- **An organization account**: Grants access to repos owned by that org. An org admin approves the installation.
- **A user account**: Grants access to repos owned by that user.

Each installation has a unique `installation_id`. A single GitHub App can have many installations across different orgs and users.

The installer chooses which repositories the app can access:
- **All repositories** in the org/account.
- **Selected repositories** — a specific subset.

### 2.4 Key Properties for Scion

| Property | PAT | GitHub App |
|----------|-----|------------|
| **Identity** | Personal user | App (machine identity) |
| **Token lifetime** | User-configured (max 1 year) | 1 hour (auto-generated) |
| **Rotation** | Manual | Automatic |
| **Repo scoping** | At PAT creation time (static) | Per-token request (dynamic) |
| **Permission scoping** | At PAT creation time (static) | Per-token request (dynamic, up to app max) |
| **Org visibility** | Limited (admin audit log) | Full (installed apps page, permissions visible) |
| **Rate limits** | User-level (5000/hr shared) | App-level (5000/hr per installation, separate from user) |
| **Revocation** | Per-token | Per-installation or per-app |
| **Commit attribution** | PAT owner | App identity (configurable) |

---

## 3. Ownership Model: Who Owns What?

This is the central design question. There are three levels at which a GitHub App could be attached to Scion:

### 3.1 Option A: Hub-Level App (Recommended)

**One GitHub App per Scion Hub deployment.**

```
Scion Hub
  └── GitHub App (registered by Hub admin)
        ├── Installation: org-acme (installation_id: 12345)
        │     ├── Grove: acme-widgets → repo: acme/widgets
        │     └── Grove: acme-api → repo: acme/api
        ├── Installation: org-beta (installation_id: 67890)
        │     └── Grove: beta-platform → repo: beta/platform
        └── Installation: user-alice (installation_id: 11111)
              └── Grove: alice-dotfiles → repo: alice/dotfiles
```

**Who does what:**

| Actor | Action |
|-------|--------|
| **Hub Admin** | Registers the GitHub App on GitHub. Configures App ID + private key on the Hub server. |
| **Org Admin / Repo Owner** | Installs the GitHub App on their org or user account (via GitHub UI). Selects which repos the app can access. |
| **Grove Creator** | Links a grove to a GitHub App installation (by providing the installation ID or via auto-discovery). |

**Pros:**
- Single app to manage. Org admins see one "Scion" app in their installed apps.
- Hub admin controls the app's maximum permissions.
- Natural fit for the Hub's role as central state authority.
- The private key never leaves the Hub — brokers receive only short-lived installation tokens.

**Cons:**
- Requires the Hub admin to register a GitHub App (operational burden for self-hosted deployments).
- The Hub must be reachable to mint tokens (already true for hosted mode).
- All organizations using the Hub must trust the same app identity.

### 3.2 Option B: User-Brought App (BYOA)

**Each user (or organization) registers their own GitHub App and provides credentials to the Hub.**

```
Scion Hub
  ├── User: alice
  │     └── GitHub App: alice-scion-app (App ID: 111, private key stored as secret)
  │           └── Installation: org-acme (installation_id: 12345)
  │                 └── Grove: acme-widgets
  └── User: bob
        └── GitHub App: bob-scion-app (App ID: 222, private key stored as secret)
              └── Installation: org-beta (installation_id: 67890)
                    └── Grove: beta-platform
```

**Who does what:**

| Actor | Action |
|-------|--------|
| **User / Org Admin** | Registers their own GitHub App. Provides App ID + private key to Scion (stored as a secret). |
| **User** | Installs the app on their org/account and associates installations with groves. |

**Pros:**
- No Hub admin involvement for GitHub setup.
- Users maintain full control over their app's permissions and installations.
- Different orgs can have fully independent app configurations.

**Cons:**
- More complex UX — every user must understand GitHub App registration.
- Private keys are uploaded as secrets to the Hub (acceptable with existing encrypted secret storage, but expands the trust surface).
- Multiple apps installed on the same org creates visual clutter in GitHub's UI.

### 3.3 Option C: Grove-Level App

**Each grove can have its own GitHub App configuration.**

This is essentially a finer-grained variant of Option B. Rather than one app per user, each grove can reference a different app. This adds flexibility but multiplies complexity. Not recommended as a primary model but should be supported as an escape hatch.

### 3.4 Recommendation

**Primary: Option A (Hub-Level App)** with **Option B (BYOA) as an advanced override.**

The Hub-level app covers the majority case: a team or organization deploys Scion Hub and configures a single GitHub App. Users install it on their orgs. This is the simplest UX for grove creators — they don't need to know about GitHub App internals.

For advanced users or multi-tenant deployments where organizations don't want to share an app identity, BYOA allows storing a user-specific or grove-specific GitHub App configuration. This uses the existing secret storage system.

The resolution hierarchy for GitHub App credentials follows the existing scope pattern:

```
Grove GitHub App config  →  (most specific, if set)
  ↓ fallback
User GitHub App config   →  (BYOA, if user registered their own app)
  ↓ fallback
Hub GitHub App config    →  (default, managed by Hub admin)
  ↓ fallback
GITHUB_TOKEN secret      →  (legacy PAT flow)
```

---

## 4. Data Model

### 4.1 GitHub App Configuration (Hub-Level)

The Hub server gains a new configuration section for the GitHub App:

```yaml
# Hub server config (e.g., hub.yaml or server flags)
github_app:
  app_id: 123456
  private_key_path: /etc/scion/github-app-key.pem
  # OR inline:
  # private_key: |
  #   -----BEGIN RSA PRIVATE KEY-----
  #   ...
```

In Go:

```go
type GitHubAppConfig struct {
    AppID          int64  `json:"app_id" yaml:"app_id" koanf:"app_id"`
    PrivateKeyPath string `json:"private_key_path,omitempty" yaml:"private_key_path,omitempty" koanf:"private_key_path"`
    PrivateKey     string `json:"private_key,omitempty" yaml:"private_key,omitempty" koanf:"private_key"`
}
```

### 4.2 Installation Registration

Each GitHub App installation is registered as a Hub resource, linked to an organization or user:

```go
type GitHubInstallation struct {
    InstallationID int64     `json:"installation_id"`
    AccountLogin   string    `json:"account_login"`   // GitHub org or user login
    AccountType    string    `json:"account_type"`     // "Organization" or "User"
    AppID          int64     `json:"app_id"`           // Which app this installation belongs to
    CreatedAt      time.Time `json:"created_at"`
    CreatedBy      string    `json:"created_by"`       // Scion user who registered it
}
```

### 4.3 Grove-to-Installation Mapping

A grove references a GitHub App installation for its credential source:

```go
// Existing Grove model, extended:
type Grove struct {
    // ... existing fields ...

    // GitHubInstallationID links this grove to a GitHub App installation.
    // When set, agents use installation tokens instead of PATs.
    GitHubInstallationID *int64 `json:"github_installation_id,omitempty"`
}
```

Alternatively, this can be stored as a grove label:
```
scion.dev/github-installation-id: "12345"
```

### 4.4 BYOA: User-Level App Credentials

For Option B, the user stores their GitHub App credentials as secrets:

```bash
# Store App ID as a user-scoped secret
scion hub secret set GITHUB_APP_ID --type variable 123456

# Store private key as a user-scoped file secret
scion hub secret set GITHUB_APP_PRIVATE_KEY --type file @./my-app-key.pem
```

Or at grove scope for grove-level override:

```bash
scion hub secret set GITHUB_APP_ID --grove acme-widgets --type variable 789
scion hub secret set GITHUB_APP_PRIVATE_KEY --grove acme-widgets --type file @./grove-key.pem
```

---

## 5. Token Lifecycle

### 5.1 Token Minting

The Hub is the sole authority for minting installation tokens. This ensures private keys never leave the Hub.

```
Agent Start                   Hub                          GitHub API
    |                          |                              |
    |-- CreateAgent ---------->|                              |
    |                          |-- Resolve grove ------------>|
    |                          |   (has installation_id?)     |
    |                          |                              |
    |                          |-- Generate JWT (app key) --->|
    |                          |                              |
    |                          |-- POST /installations/       |
    |                          |   {id}/access_tokens ------->|
    |                          |   { repositories: [repo],    |
    |                          |     permissions: {            |
    |                          |       contents: write,        |
    |                          |       pull_requests: write    |
    |                          |     }                         |
    |                          |   }                          |
    |                          |                              |
    |                          |<-- token: ghs_xxx (1hr) -----|
    |                          |                              |
    |<-- GITHUB_TOKEN=ghs_xxx-|                              |
    |    (in resolved env)     |                              |
```

The minted token is injected as `GITHUB_TOKEN` in the agent's environment — **the agent doesn't know or care whether the token came from a PAT or a GitHub App**. This is key: the credential source is transparent to the agent and harness.

### 5.2 Token Refresh

Installation tokens expire after 1 hour. Agents that run longer than 1 hour need token refresh. Three approaches:

#### Approach 1: Refresh via sciontool Sidecar (Recommended)

`sciontool` already runs inside the container as the init process. It can run a background goroutine that refreshes the token before expiry:

```
sciontool init
  └── tokenRefreshLoop():
        every 50 minutes:
          1. POST to Hub: /api/v1/agents/{id}/refresh-token
          2. Hub mints new installation token
          3. Hub returns token
          4. sciontool updates:
             - $GITHUB_TOKEN env var (for new processes)
             - git credential helper cache
             - writes to /tmp/.github-token (for running processes to read)
```

The Hub endpoint `/api/v1/agents/{id}/refresh-token` is authenticated using the agent's existing Hub session/token. The Hub resolves the grove's installation and mints a fresh token.

**Pros:** Transparent to the harness. Token stays fresh automatically.
**Cons:** Requires the agent container to maintain Hub connectivity.

#### Approach 2: Hub-Side Token Cache with Short TTL

The Hub pre-generates tokens and caches them. Agents receive the current token at start. If an agent needs a fresh token, the `sciontool` credential helper calls the Hub on-demand.

**Credential helper integration:**

```bash
# Git credential helper (configured during clone):
git config credential.helper '!sciontool credential-helper'

# sciontool credential-helper:
#   1. Check cached token age
#   2. If fresh (< 50 min): return cached token
#   3. If stale: call Hub refresh endpoint, cache new token, return
```

**Pros:** Lazy refresh — tokens only minted when actually needed.
**Cons:** Git operations could stall if Hub is slow to respond.

#### Approach 3: Long-Lived PAT Fallback

If token refresh is too complex initially, keep the architecture but allow graceful fallback to PATs for long-running agents. The initial implementation could set a 1-hour agent timeout warning.

### 5.3 Recommendation

**Start with Approach 2** (credential helper + on-demand refresh). It's the most natural fit with git's credential helper architecture and avoids polling. The credential helper is already configured by `sciontool` during clone (see `git-groves.md` §5.4). Extending it to call the Hub for fresh tokens is a small change.

Approach 1 (background loop) can be added later if non-git GitHub API usage (e.g., `gh` CLI for PR creation) needs proactive refresh.

---

## 6. Installation Discovery and Association

### 6.1 Manual Association

The simplest flow: the user provides the installation ID when creating or configuring a grove.

```bash
# During grove creation
scion hub grove create https://github.com/acme/widgets.git --github-installation 12345

# Or after creation
scion hub grove set acme-widgets --github-installation 12345
```

The user finds the installation ID from the GitHub App's installation page or from `GET /app/installations` (which the Hub can proxy).

### 6.2 Auto-Discovery

When a grove is created from a GitHub URL and the Hub has a GitHub App configured, the Hub can automatically discover matching installations:

```
1. Hub generates JWT (app identity)
2. Hub calls GET /app/installations (lists all installations)
3. For each installation, calls GET /installation/repositories
4. Finds installation(s) that include the grove's target repo
5. If exactly one match: auto-associate
6. If multiple matches: prompt user to select (or pick the org-level one)
7. If no match: fall back to PAT, suggest installing the app
```

This auto-discovery runs during `scion hub grove create` or `scion start` (if the grove doesn't yet have an installation associated).

### 6.3 Installation Registration Flow

For Hub-level apps, a streamlined flow:

```bash
# Hub admin: configure the app (one-time)
scion server --github-app-id 123456 --github-app-key /path/to/key.pem

# User: install the app on their org (happens on GitHub.com)
# GitHub redirects to Hub callback URL after installation

# User: create a grove (auto-discovers installation)
scion hub grove create https://github.com/acme/widgets.git
# Output:
#   Grove created: acme-widgets
#   GitHub App: Found installation for org 'acme' (id: 12345)
#   Credential source: GitHub App (auto-refresh enabled)
```

### 6.4 Installation Webhook (Future)

GitHub sends a webhook when the app is installed or uninstalled. The Hub can register a webhook endpoint to automatically track installations:

```
POST /api/v1/webhooks/github

Payload: { action: "created", installation: { id: 12345, account: { login: "acme" } } }
→ Hub creates GitHubInstallation record

Payload: { action: "deleted", installation: { id: 12345 } }
→ Hub marks installation as deleted, alerts affected groves
```

This is a future enhancement — the initial implementation uses manual or auto-discovery flows.

---

## 7. Hub API Changes

### 7.1 New Endpoints

```
# GitHub App configuration (admin only)
GET    /api/v1/github-app              → Returns app config (app ID, status, not the key)
PUT    /api/v1/github-app              → Update app config

# Installations
GET    /api/v1/github-app/installations           → List known installations
POST   /api/v1/github-app/installations/discover   → Trigger discovery from GitHub API
GET    /api/v1/github-app/installations/{id}       → Get installation details

# Grove association
PUT    /api/v1/groves/{id}/github-installation     → Set installation for grove
DELETE /api/v1/groves/{id}/github-installation     → Remove (fall back to PAT)

# Token refresh (called by sciontool inside agent container)
POST   /api/v1/agents/{id}/refresh-token           → Mint fresh installation token
```

### 7.2 Modified Endpoints

The existing agent creation flow (`POST /api/v1/groves/{id}/agents` and the Hub→Broker dispatch) is modified to:

1. Check if the grove has a `github_installation_id`.
2. If yes: mint an installation token and include it as `GITHUB_TOKEN` in resolved environment.
3. If no: fall through to existing PAT secret resolution.

This is transparent to the Broker and agent — they always receive a `GITHUB_TOKEN` env var regardless of source.

---

## 8. Permission Model

### 8.1 App-Level Permissions (Set at Registration)

The GitHub App should be registered with the **maximum permissions** any agent might need:

| Permission | Access | Purpose |
|------------|--------|---------|
| Contents | Read and write | Clone, commit, push |
| Metadata | Read | Repository info |
| Pull requests | Read and write | Create/update PRs |
| Issues | Read and write | Create/comment on issues |
| Checks | Read and write | Report CI status (future) |
| Actions | Read | Read workflow status (future) |

### 8.2 Per-Token Permission Restriction

When minting an installation token, the Hub can request a **subset** of the app's registered permissions. This enables least-privilege per grove or per agent:

```go
// Token request body
{
    "repositories": ["widgets"],           // Scope to specific repo
    "permissions": {
        "contents": "write",
        "pull_requests": "write",
        "metadata": "read"
    }
}
```

A grove could declare the permissions it needs:

```yaml
# Grove labels or settings
scion.dev/github-permissions: "contents:write,pull_requests:write,metadata:read"
```

Or this could be template-driven:

```yaml
# In scion-agent.yaml template
github_permissions:
  contents: write
  pull_requests: write
  metadata: read
```

### 8.3 Default vs Custom Permissions

For simplicity, the initial implementation uses a **default permission set** (Contents: write, Pull Requests: write, Metadata: read) for all installation tokens. Per-grove or per-template permission customization is a future enhancement.

---

## 9. Integration with Existing Systems

### 9.1 Secret Resolution Pipeline

The GitHub App integration slots into the existing secret resolution pipeline as a new resolution source. The priority order:

```
1. Grove-scoped GITHUB_TOKEN secret (explicit PAT override)
2. GitHub App installation token (if grove has installation_id)
3. User-scoped GITHUB_TOKEN secret (user's PAT)
4. Hub-level GITHUB_TOKEN secret (shared PAT, if any)
```

If a grove has both a `GITHUB_TOKEN` secret and a `github_installation_id`, the explicit secret wins. This allows per-grove override (e.g., a grove that needs a token with org-admin permissions that the app doesn't have).

### 9.2 Agent Transparency

The agent and harness code requires **zero changes**. The credential arrives as `GITHUB_TOKEN` regardless of source. The git credential helper configured by `sciontool` works identically with both PATs and installation tokens. The `gh` CLI also uses `GITHUB_TOKEN` natively.

### 9.3 sciontool Changes

`sciontool` gains:

1. **Token refresh credential helper**: When `SCION_GITHUB_APP_ENABLED=true` is set in the environment, the credential helper calls the Hub to refresh tokens instead of returning a static value.
2. **Token metadata awareness**: `sciontool` receives `SCION_GITHUB_TOKEN_EXPIRY` to know when the initial token expires, enabling proactive refresh.

### 9.4 Web UI

The web frontend gains:

1. **Grove detail page**: Shows credential source (PAT vs GitHub App), installation status, token health.
2. **Hub admin page**: GitHub App configuration, installation list, discovery trigger.
3. **Grove creation flow**: Option to select a GitHub App installation or enter PAT.

---

## 10. Alternatives Considered

### 10.1 GitHub OAuth User Tokens for Git Operations

Instead of a GitHub App, use the existing GitHub OAuth flow (already used for Hub login) to obtain user tokens with repo access scopes.

**Why rejected:**
- OAuth user tokens inherit the user's full access — no way to restrict to specific repos.
- Token refresh requires user interaction (re-auth).
- Commits attributed to the user, not the system.
- Conflates Hub authentication (who is this person?) with agent authorization (what can this agent do?).

### 10.2 GitHub App as Sole Auth Method (Replace PATs Entirely)

Force all users to use GitHub App, deprecate PAT support.

**Why rejected:**
- PATs are simpler for solo/local mode where there's no Hub.
- Not all users have org admin access to install apps.
- GitHub Enterprise Server may have restrictions on GitHub Apps.
- Backward compatibility — existing deployments rely on PATs.

### 10.3 Per-Agent GitHub App (One App per Agent)

Register a separate GitHub App for each agent.

**Why rejected:**
- GitHub has limits on app creation per account.
- Massive operational overhead.
- No benefit over installation-scoped tokens from a single app.

### 10.4 GitHub App Owned by Grove Creator (Not Hub Admin)

Instead of the Hub admin registering the app, require each grove creator to register one.

**Why rejected as primary:**
- Unreasonable UX burden for most users.
- However, this is preserved as the BYOA escape hatch (Option B in §3.2).

### 10.5 Proxy All Git Operations Through Hub

Instead of giving agents tokens, route all git clone/push through a Hub-side proxy that handles auth.

**Why rejected:**
- Massive bandwidth and latency implications.
- Breaks standard git tooling inside the agent.
- Over-engineered for the problem.

---

## 11. Open Questions

### 11.1 Solo Mode Support

**Question:** Should GitHub App support work in solo/local mode (no Hub), or is it Hub-only?

**Consideration:** In solo mode, there's no server to hold the private key or mint tokens. Options:
- (a) Hub-only — simplest. Solo mode continues to use PATs exclusively.
- (b) Local key storage — the CLI stores the private key in `~/.scion/github-app-key.pem` and mints tokens locally. Simpler than it sounds, but introduces key management concerns for non-technical users.

**Leaning:** (a) Hub-only initially. GitHub App requires infrastructure (key management, token minting) that naturally lives on a server.

### 11.2 Multi-Repo Groves

**Question:** Can a grove span multiple repositories? If so, how are installation tokens scoped?

**Consideration:** Today a grove maps to exactly one git remote. If this changes, the installation token request would need to list multiple repositories. GitHub App installation tokens support scoping to multiple repos, so the mechanics work — the data model just needs to handle it.

**Leaning:** Defer. Keep the 1:1 grove-to-repo mapping for now.

### 11.3 Commit Attribution

**Question:** Should agent commits be attributed to the GitHub App bot account, or to a specific user?

**Options:**
- (a) App bot identity: Commits from `scion-app[bot]@users.noreply.github.com`. Clear that it's automated. But: some orgs require commits from real users.
- (b) Configurable: Allow groves or templates to specify `git user.name` and `git user.email`. The installation token still authenticates the push, but the commit author differs.
- (c) Co-authored-by: Use the bot identity but add `Co-authored-by: Alice <alice@example.com>` trailers linking to the Scion user who started the agent.

**Leaning:** (a) by default with (b) as a configuration option. The `sciontool` git identity configuration (`git config user.name/email`) is already separate from the auth token.

### 11.4 Rate Limiting

**Question:** GitHub App installation tokens have their own rate limit (5000 req/hr per installation). With many agents on the same grove (same installation), could rate limits be exhausted?

**Consideration:** 5000/hr is generous for git operations. API-heavy agents (e.g., fetching hundreds of issues) could be a concern. Rate limit headers should be monitored and surfaced in agent status.

**Leaning:** Monitor and log rate limit headers. Add rate limit status to agent health checks in a future iteration.

### 11.5 GitHub Enterprise Server

**Question:** Does this design work with GitHub Enterprise Server (GHES)?

**Consideration:** GHES supports GitHub Apps with a different base URL. The Hub config would need a configurable API base URL:

```yaml
github_app:
  app_id: 123
  private_key_path: /path/to/key.pem
  api_base_url: https://github.mycompany.com/api/v3  # default: https://api.github.com
```

**Leaning:** Support it from the start — it's just a URL configuration.

### 11.6 Installation Scope: All Repos vs Selected

**Question:** Should the Hub enforce or recommend "selected repositories" mode for app installations?

**Consideration:** "All repositories" mode means the app (and therefore any grove) could mint tokens for any repo in the org. This is convenient but overly permissive. "Selected repositories" is more secure but requires the org admin to update the selection when new repos are added.

**Leaning:** Recommend "selected repositories" in documentation. The Hub can warn if an installation has "all repositories" access. Enforcement is at the GitHub level, not Scion's.

### 11.7 What Happens When Installation Is Revoked?

**Question:** If an org admin uninstalls the GitHub App, what happens to running agents?

**Consideration:** Running agents hold a valid token (up to 1 hour). After expiry, token refresh will fail. The Hub should detect this (403 from GitHub API) and:
1. Mark the installation as `suspended` or `deleted`.
2. Report agent error status with guidance: "GitHub App installation revoked for org 'acme'."
3. Affected groves fall back to PAT if one is configured, or error out.

**Leaning:** Handle gracefully with status reporting. Future webhook support (§6.4) can detect this proactively.

### 11.8 Private Key Rotation

**Question:** How is the GitHub App private key rotated?

**Consideration:** GitHub allows generating multiple private keys for an app. The rotation flow:
1. Generate new key on GitHub.
2. Update Hub config to point to new key.
3. Restart Hub server (or support hot-reload).
4. Delete old key on GitHub.

**Leaning:** Document the rotation procedure. Hot-reload of the private key is a nice-to-have but not critical for initial implementation.

---

## 12. Implementation Phases

### Phase 1: Hub-Level App Configuration and Token Minting

1. Add `GitHubAppConfig` to Hub server configuration.
2. Implement JWT generation from private key (`pkg/hub/githubapp/`).
3. Implement installation token minting via GitHub API.
4. Add Hub API: `GET /api/v1/github-app`, `PUT /api/v1/github-app`.
5. Add `GitHubInstallation` model and store operations.
6. Add Hub API: `GET/POST /api/v1/github-app/installations`.
7. Unit tests for JWT generation and token exchange.

### Phase 2: Grove Association and Secret Resolution Integration

1. Add `github_installation_id` to Grove model (or as label).
2. Modify `scion hub grove create` to accept `--github-installation` flag.
3. Implement auto-discovery of installations for a given repo.
4. Integrate into secret resolution: when grove has installation, mint token instead of resolving PAT.
5. Transparent injection as `GITHUB_TOKEN` in agent environment.
6. Integration tests: grove create → agent start → git clone with app token.

### Phase 3: Token Refresh

1. Add Hub API: `POST /api/v1/agents/{id}/refresh-token`.
2. Extend `sciontool` credential helper to call Hub for fresh tokens.
3. Add `SCION_GITHUB_APP_ENABLED` and `SCION_GITHUB_TOKEN_EXPIRY` env vars.
4. Test long-running agents with token refresh cycle.

### Phase 4: BYOA and Advanced Features

1. Support user-scoped and grove-scoped GitHub App secrets (`GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY`).
2. Resolution hierarchy: grove app → user app → hub app → PAT.
3. Per-grove permission customization.
4. Web UI for installation management and credential source visibility.
5. GitHub Enterprise Server URL configuration.

### Phase 5: Webhooks (Future)

1. Webhook endpoint for installation events (created, deleted, suspended).
2. Proactive installation tracking without manual registration.
3. Webhook-driven agent creation (e.g., on PR events). Separate design doc.

---

## 13. Security Considerations

### 13.1 Private Key Protection

The GitHub App private key is the most sensitive credential in this system. It can mint tokens for any installation of the app.

- **At rest**: Stored on the Hub server's filesystem or in a cloud secret manager (GCP SM, AWS SM). Never in the database.
- **In transit**: Never leaves the Hub. Brokers and agents receive only installation tokens.
- **Access**: Only the Hub server process reads the key. Filesystem permissions: `0600`, owned by the Hub service user.
- **Rotation**: Supported via GitHub's multi-key feature. Document the procedure.

### 13.2 Installation Token Scope

Installation tokens are always scoped to the **minimum necessary**:
- **Repositories**: Scoped to the grove's target repository (single repo).
- **Permissions**: Default set (Contents: write, Pull Requests: write, Metadata: read). Extensible per grove.

Even if an installation grants access to "all repositories" in an org, the minted token only gets access to the specific repo the grove targets.

### 13.3 Token Exposure

Installation tokens are treated identically to PATs in the security model:
- Injected as environment variables (same as today).
- Never logged or written to disk by `sciontool` (existing sanitization applies).
- 1-hour expiry limits blast radius of token theft.

### 13.4 Trust Boundary

The Hub is the trust anchor. Organizations installing the GitHub App are trusting:
1. The Hub operator (who holds the private key).
2. The Scion platform (to mint correctly scoped tokens).
3. Their own installation scope (which repos the app can access).

This is comparable to installing any third-party GitHub App (CI systems, code review tools, etc.).

---

## 14. References

- **GitHub Docs**: [About GitHub Apps](https://docs.github.com/en/apps/overview)
- **GitHub Docs**: [Authenticating as a GitHub App](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/about-authentication-with-a-github-app)
- **GitHub Docs**: [Creating an installation access token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app)
- **Scion Design**: `.design/hosted/git-groves.md` — Current PAT-based git authentication
- **Scion Design**: `.design/hosted/secrets-gather.md` — Secret provisioning and resolution
- **Scion Design**: `.design/agent-credentials.md` — Agent credential management
- **Scion Design**: `.design/hosted/auth/oauth-setup.md` — Hub OAuth configuration (user auth, separate from this)
