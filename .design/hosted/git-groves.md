# Git Groves: Remote Git-Based Workspace Design

**Created:** 2026-02-19
**Status:** Draft
**Supersedes:** `.design/kubernetes/scm.md` (largely out of date)
**Related:** `git-ws.md` (research), `secrets.md`, `hosted-architecture.md`, `sync-design.md`

---

## 1. Overview

This document proposes how Scion will support **git-anchored groves on the Hub** — groves that are defined purely by a remote git repository URL and can provision agents with full git workspaces without requiring any local filesystem representation.

Today, groves are primarily created by running `scion grove init` inside a local git checkout, with Hub registration as a secondary step. This design introduces a **Hub-first flow** where a user creates a grove directly on the Hub from a git URL, sets authentication credentials, and starts agents that clone the repository at initialization time.

### Goals

1. Allow users to create groves on the Hub with only a git URL and a secret — no local checkout required.
2. Agent containers clone the repository at startup using `sciontool`, producing a fully functional git workspace.
3. Agents can commit and push changes back to the remote repository.
4. All lifecycle state changes are reported via Hub events for web UI visibility.

### Non-Goals

- GCS-based workspace sync (addressed separately in `sync-design.md`).
- Multi-provider git hosting abstraction (GitLab, Bitbucket, etc.) — this design targets GitHub as the initial implementation.
- GitHub App token exchange flow — deferred to a later phase.
- Automated branch protection enforcement — not possible with fine-grained PATs today.

---

## 2. Git URL as Grove Identity

### 2.1 Current State

Groves use a normalized git remote as their unique identity (`github.com/org/repo`). The normalization pipeline (`pkg/util/git.go:NormalizeGitRemote`) strips protocol, converts SSH notation, removes `.git` suffix, and lowercases.

### 2.2 Updated Policy

The git URL used to anchor a grove should be **any valid remote git URL** — not limited to the `origin` remote of a local checkout. Acceptable inputs:

| Input | Normalized Form |
|-------|----------------|
| `https://github.com/acme/widgets.git` | `github.com/acme/widgets` |
| `git@github.com:acme/widgets.git` | `github.com/acme/widgets` |
| `ssh://git@github.com/acme/widgets` | `github.com/acme/widgets` |
| `https://github.com/acme/widgets` (no `.git`) | `github.com/acme/widgets` |

Repositories without a remote URL (purely local repos, or non-git directories) are treated as **regular workspaces with no git anchoring**. They cannot be created as Hub-first groves and continue to use the existing local-only flow.

### 2.3 Slug Derivation

When creating a grove from a URL, the slug is derived from the normalized form:

- **Default**: repository name only (e.g., `widgets`)
- **With `--slug` override**: user-specified slug
- **Collision handling**: if `widgets` already exists, suggest `acme-widgets` (org-repo format)

> **Open Question**: Should the default slug be `repo` or `org/repo` format? The `org/repo` form avoids collisions across organizations but introduces a path separator into the slug. Using `org-repo` (hyphenated) as the default may be the best compromise.

---

## 3. Hub-First Grove Creation

### 3.1 New Command: `scion hub grove create`

```
scion hub grove create <git-url> [flags]
```

**Arguments:**
- `<git-url>` — Any valid remote git URL (HTTPS or SSH format)

**Flags:**
- `--slug <slug>` — Override the auto-derived slug
- `--name <name>` — Human-friendly display name (defaults to repo name)
- `--branch <branch>` — Default branch for new agents (defaults to `main`)
- `--visibility <private|team|public>` — Grove visibility (defaults to `private`)

**Behavior:**

1. Validate the URL is a parseable git remote URL.
2. Normalize via `NormalizeGitRemote()`.
3. Check for existing grove with same normalized remote (error if exists).
4. Derive slug from URL (repo name, or `org-repo` on collision).
5. Call `POST /api/v1/groves` with:
   ```json
   {
     "name": "widgets",
     "slug": "widgets",
     "gitRemote": "github.com/acme/widgets",
     "labels": {
       "scion.dev/default-branch": "main"
     }
   }
   ```
6. Print grove ID, slug, and next-steps guidance.

**Example Session:**

```
$ scion hub grove create https://github.com/acme/widgets.git
Grove created:
  ID:     a1b2c3d4-...
  Slug:   widgets
  Remote: github.com/acme/widgets

Next steps:
  1. Set git credentials:
     scion hub secret set GITHUB_TOKEN --grove widgets <your-pat>

  2. Start an agent:
     scion start my-agent --grove widgets "implement feature X"
```

### 3.2 Hub API Changes

The existing `POST /api/v1/groves` endpoint already supports creating groves without a broker. The request body needs no structural changes — the `gitRemote` field is already supported. The only change is ensuring the endpoint accepts creation requests where `gitRemote` is provided but no broker is associated.

### 3.3 Local Grove Linking (Existing Flow, No Changes)

The existing `scion hub link` command continues to work for users who have a local checkout. When a user runs `scion hub link` in a git repository, the grove is registered/linked using the detected `origin` remote. This flow is unchanged.

---

## 4. Authentication: GitHub Fine-Grained PATs

### 4.1 Token Type

The initial implementation uses **GitHub Fine-Grained Personal Access Tokens (PATs)**. These tokens:

- Are scoped to specific repositories
- Support granular permissions (Contents: read/write, Metadata: read, etc.)
- Have configurable expiration
- Are bound to a single user account

### 4.2 Required Token Permissions

For agents to clone, commit, and push:

| Permission | Access | Purpose |
|------------|--------|---------|
| Contents | Read and write | Clone, commit, push |
| Metadata | Read | Repository info |
| Pull requests | Read and write | Open PRs (optional) |

### 4.3 Branch Protection Limitation

GitHub fine-grained PATs **do not support restricting push operations to specific branches**. A token with Contents write access can push to any branch the user has access to, including `main` and other protected branches (subject to GitHub branch protection rules configured on the repository).

**Mitigation strategies:**

1. **GitHub branch protection rules** — Configure the repository with branch protection on `main`/`master` requiring PR reviews. This is the primary defense and is external to Scion.
2. **Scion convention** — Agents are configured (via harness instructions and system prompt) to work on feature branches (`scion/<agent-name>`) and never push directly to protected branches. This is an advisory control, not enforced at the credential level.
3. **Future: GitHub App tokens** — GitHub App installation tokens can be issued with more granular control. This is deferred to a later phase.

### 4.4 Secret Storage

The PAT is stored as a grove-scoped secret using the existing secret management system:

```
scion hub secret set GITHUB_TOKEN --grove widgets <pat-value>
```

This creates a secret with:
- **Key**: `GITHUB_TOKEN`
- **Type**: `environment` (injected as env var)
- **Scope**: `grove`
- **ScopeID**: grove UUID (resolved from slug `widgets`)

The existing `scion hub secret set` command already supports `--grove` scoping. The secret value is encrypted at rest (Hub DB encryption or GCP Secret Manager, per `secrets.md`).

---

## 5. Agent Workspace Provisioning

### 5.1 Flow Overview

When `scion start my-agent --grove widgets` is executed against a Hub-first git grove (no local checkout), the workspace must be provisioned by cloning the repository inside the agent container.

```
User CLI                  Hub                    Runtime Broker             Container
   |                       |                          |                        |
   |-- start agent ------->|                          |                        |
   |                       |-- resolve grove -------->|                        |
   |                       |   (git remote, secrets)  |                        |
   |                       |                          |                        |
   |                       |-- CreateAgent ---------->|                        |
   |                       |   (with gitCloneConfig)  |                        |
   |                       |                          |-- create container --->|
   |                       |                          |   (GITHUB_TOKEN env,   |
   |                       |                          |    GIT_CLONE_URL,      |
   |                       |                          |    GIT_BRANCH)         |
   |                       |                          |                        |
   |                       |                          |                  sciontool init
   |                       |                          |                   ├─ git clone
   |                       |                          |                   ├─ git checkout
   |                       |                          |                   ├─ configure git
   |                       |                          |                   └─ start harness
   |                       |                          |                        |
   |                       |<-- status: CLONING ------|<-- event --------------|
   |                       |<-- status: RUNNING ------|<-- event --------------|
   |<-- agent started -----|                          |                        |
```

### 5.2 CreateAgent Payload Extension

The `CreateAgent` command payload (sent from Hub to Runtime Broker via WebSocket control channel) is extended with git clone configuration:

```json
{
  "type": "command",
  "command": "create_agent",
  "payload": {
    "agentId": "...",
    "name": "my-agent",
    "config": {
      "template": "coder",
      "image": "claude-sandbox:latest",
      "harness": "claude",
      "gitClone": {
        "url": "https://github.com/acme/widgets.git",
        "branch": "main",
        "depth": 1
      },
      "env": ["GITHUB_TOKEN=<resolved-secret-value>"],
      ...
    }
  }
}
```

The `gitClone` object is a new field on the agent config. When present, it signals that the workspace should be populated via `git clone` rather than volume mounting or file sync.

### 5.3 Runtime Broker Handling

When the Runtime Broker receives a `CreateAgent` command with `gitClone` config:

1. **No worktree creation** — skip the `util.CreateWorktree()` call.
2. **No workspace mount** — the workspace will be created inside the container.
3. **Environment injection** — pass `GITHUB_TOKEN`, `SCION_GIT_CLONE_URL`, `SCION_GIT_BRANCH`, and `SCION_GIT_DEPTH` as environment variables to the container.
4. **Container start** — start the container with `sciontool init` as the entrypoint (unchanged).

### 5.4 sciontool: Git Clone Phase

`sciontool init` gains a new **git clone phase** that runs before the harness process starts. This slots into the existing initialization sequence:

```
sciontool init
  1. StartReaper()
  2. setupHostUser()
  3. Start Telemetry
  4. Initialize Lifecycle Hooks
  5. RunPreStart()
  6. *** NEW: gitCloneWorkspace() ***     <-- git clone phase
  7. Start Sidecar Services
  8. Create Supervisor
  9. Start Child Process (harness)
  ...
```

#### `gitCloneWorkspace()` Implementation

Triggered when the environment variable `SCION_GIT_CLONE_URL` is set and `/workspace` is empty (or does not exist).

```
gitCloneWorkspace():
  1. Report status: CLONING (via Hub heartbeat)

  2. Construct authenticated URL:
     https://oauth2:${GITHUB_TOKEN}@github.com/acme/widgets.git

  3. Execute:
     git clone --depth=${SCION_GIT_DEPTH:-1} \
       --branch=${SCION_GIT_BRANCH:-main} \
       <authenticated-url> /workspace

  4. Configure git identity:
     git -C /workspace config user.name "Scion Agent (${SCION_AGENT_NAME})"
     git -C /workspace config user.email "agent@scion.dev"

  5. Configure credential helper for subsequent operations:
     git -C /workspace config credential.helper \
       '!f() { echo "password=${GITHUB_TOKEN}"; echo "username=oauth2"; }; f'

  6. Create and checkout agent feature branch:
     git -C /workspace checkout -b scion/${SCION_AGENT_NAME}

  7. Report status: STARTING (clone complete, proceeding to harness startup)
```

**Error handling**: If `git clone` fails (bad URL, invalid token, network error), sciontool reports status `ERROR` with a descriptive message and exits with a non-zero code. The Hub event includes the error detail for UI display.

**Security**: The authenticated URL is constructed in-process and never written to disk or logs. The `GITHUB_TOKEN` environment variable is available for the credential helper to use during subsequent push operations, but is not embedded in the git config.

### 5.5 Environment Variables

| Variable | Source | Description |
|----------|--------|-------------|
| `GITHUB_TOKEN` | Grove secret (resolved) | GitHub PAT for authentication |
| `SCION_GIT_CLONE_URL` | Grove gitRemote (HTTPS form) | Repository URL (without credentials) |
| `SCION_GIT_BRANCH` | Agent config / grove label | Branch to clone and checkout |
| `SCION_GIT_DEPTH` | Agent config (optional) | Clone depth (default: 1) |

### 5.6 Branch Strategy

When an agent starts on a git grove:

1. The repository is cloned at the specified branch (default: grove's default branch, typically `main`).
2. A new feature branch is created: `scion/<agent-name>`.
3. The agent works on this feature branch exclusively.
4. Commits and pushes go to `scion/<agent-name>` on the remote.

This matches the existing local worktree behavior where each agent gets its own branch.

---

## 6. Agent Lifecycle with Git Groves

### 6.1 Starting an Agent

```
scion start my-agent --grove widgets "implement login page"
```

**Resolution flow:**

1. CLI resolves `widgets` to a grove ID via Hub API (`GET /api/v1/groves?slug=widgets`).
2. CLI calls `POST /api/v1/agents` with the grove ID.
3. Hub resolves grove's git remote and `GITHUB_TOKEN` secret.
4. Hub selects an available Runtime Broker for the grove.
5. Hub sends `CreateAgent` command to Broker with `gitClone` config and resolved secrets.
6. Broker starts container with appropriate environment.
7. `sciontool` clones repo, creates branch, starts harness.
8. Status transitions visible in web UI: `PENDING` → `CLONING` → `STARTING` → `RUNNING`.

### 6.2 Status Events

The `sciontool` status reporting system already supports arbitrary status values. The git clone phase introduces a new status:

| Status | Meaning |
|--------|---------|
| `PENDING` | Agent record created, container not yet started |
| `CLONING` | **New** — `sciontool` is cloning the git repository |
| `STARTING` | Container running, harness initializing |
| `RUNNING` | Harness active, agent accepting work |
| `WAITING_FOR_INPUT` | Agent needs human interaction |
| `COMPLETED` | Task finished |
| `ERROR` | Fatal error (including clone failure) |

These status transitions are reported via:
- `sciontool` → Hub API (`POST /api/v1/agents/{id}/status`)
- Hub → WebSocket event bus → Web UI

### 6.3 Pushing Changes

Once the agent has made changes, it can commit and push using standard git operations. The credential helper configured during clone provides authentication for push:

```bash
git add .
git commit -m "implement login page"
git push -u origin scion/my-agent
```

The harness instructions should guide the agent to:
1. Never push to `main` or other protected branches directly.
2. Always push to the `scion/<agent-name>` branch.
3. Open a PR via the GitHub API (using `GITHUB_TOKEN`) if requested.

### 6.4 Detach / Reattach

Git groves work identically to local groves for attach/detach operations. The container persists with its cloned workspace between sessions:

```
scion attach my-agent --grove widgets
scion detach
```

### 6.5 Agent Deletion

When an agent is deleted, the container is removed. The cloned workspace is ephemeral (container-local storage) and is discarded. Any unpushed commits are lost. The remote branch (`scion/my-agent`) remains on GitHub.

---

## 7. New CLI Command: `scion hub grove create`

### 7.1 Command Registration

Add to `cmd/hub.go` under the existing `hubGrovesCmd` group:

```go
var hubGroveCreateCmd = &cobra.Command{
    Use:   "create <git-url>",
    Short: "Create a grove on the Hub from a git repository URL",
    Long: `Creates a new grove on the Hub anchored to a remote git repository.
The grove can be used to start agents without a local checkout of the repository.`,
    Args: cobra.ExactArgs(1),
    RunE: runHubGroveCreate,
}
```

### 7.2 Implementation Sketch

```go
func runHubGroveCreate(cmd *cobra.Command, args []string) error {
    gitURL := args[0]

    // Validate URL format
    if !util.IsGitURL(gitURL) {
        return fmt.Errorf("invalid git URL: %s", gitURL)
    }

    normalized := util.NormalizeGitRemote(gitURL)
    repoName := util.ExtractRepoName(gitURL)
    slug := slugOverride
    if slug == "" {
        slug = util.Slugify(repoName)
    }
    displayName := nameOverride
    if displayName == "" {
        displayName = repoName
    }

    client := getHubClient()

    // Check for existing grove
    existing, _ := client.Groves().List(ctx, &hubclient.ListGrovesOptions{
        GitRemote: normalized,
    })
    if len(existing) > 0 {
        return fmt.Errorf("grove already exists for %s (slug: %s)", normalized, existing[0].Slug)
    }

    // Create grove
    grove, err := client.Groves().Create(ctx, &hubclient.CreateGroveRequest{
        Name:      displayName,
        Slug:      slug,
        GitRemote: normalized,
        Labels: map[string]string{
            "scion.dev/default-branch": defaultBranch,
            "scion.dev/clone-url":      util.ToHTTPSCloneURL(gitURL),
        },
    })
    if err != nil {
        return err
    }

    // Print result and guidance
    fmt.Printf("Grove created:\n")
    fmt.Printf("  ID:     %s\n", grove.ID)
    fmt.Printf("  Slug:   %s\n", grove.Slug)
    fmt.Printf("  Remote: %s\n", grove.GitRemote)
    fmt.Printf("\nNext steps:\n")
    fmt.Printf("  1. Set git credentials:\n")
    fmt.Printf("     scion hub secret set GITHUB_TOKEN --grove %s <your-pat>\n\n", grove.Slug)
    fmt.Printf("  2. Start an agent:\n")
    fmt.Printf("     scion start my-agent --grove %s \"your task\"\n", grove.Slug)

    return nil
}
```

### 7.3 URL Validation Utility

A new utility function `IsGitURL()` validates that a string is a plausible git remote URL:

```go
func IsGitURL(s string) bool {
    // Accept HTTPS URLs to known git hosts
    // Accept SSH format (git@host:path)
    // Accept ssh:// URLs
    // Reject local paths, empty strings, etc.
}
```

Additionally, `ToHTTPSCloneURL()` converts any valid git URL to the HTTPS clone form (needed for `git clone`):

```go
func ToHTTPSCloneURL(gitURL string) string {
    // git@github.com:org/repo.git → https://github.com/org/repo.git
    // ssh://git@github.com/org/repo → https://github.com/org/repo.git
    // https://github.com/org/repo.git → https://github.com/org/repo.git (passthrough)
}
```

---

## 8. Config and Data Model Changes

### 8.1 AgentConfig Extension

Add `GitClone` field to the agent configuration model:

```go
// GitCloneConfig specifies how to clone a git repository into the workspace.
type GitCloneConfig struct {
    URL    string `json:"url"`              // HTTPS clone URL (without credentials)
    Branch string `json:"branch,omitempty"` // Branch to clone (default: main)
    Depth  int    `json:"depth,omitempty"`  // Clone depth (default: 1, 0 = full)
}
```

This appears in:
- `AgentAppliedConfig` (stored in Hub DB with agent record)
- `CreateAgent` command payload (Hub → Broker)
- `runtime.RunConfig` (Broker → container)

### 8.2 Grove Labels

The grove's default branch and HTTPS clone URL are stored as labels on the grove record:

| Label | Value | Purpose |
|-------|-------|---------|
| `scion.dev/default-branch` | `main` | Default branch for new agents |
| `scion.dev/clone-url` | `https://github.com/org/repo.git` | HTTPS-form URL for cloning |

Labels are used rather than new schema fields to avoid schema migration for what are effectively configuration preferences.

### 8.3 Hub Secret Resolution for Git Clone

When the Hub prepares the `CreateAgent` payload for a git grove, it must:

1. Look up the grove's git remote and clone URL.
2. Resolve the `GITHUB_TOKEN` secret at grove scope.
3. Include both in the agent's environment.

The existing secret resolution pipeline (`GET /api/v1/agents/{id}/resolved-secrets`) already handles scope merging. The `GITHUB_TOKEN` secret is resolved like any other grove-scoped environment secret.

---

## 9. Runtime Broker Changes

### 9.1 Provisioning Path for Git Groves

When the Runtime Broker receives a `CreateAgent` command with `gitClone` configuration:

1. **Skip worktree creation** — `ProvisionAgent()` detects `gitClone` config and skips the `util.CreateWorktree()` call.
2. **Skip workspace mounting** — no host-side workspace directory is bind-mounted.
3. **Inject git environment** — add `SCION_GIT_CLONE_URL`, `SCION_GIT_BRANCH`, `SCION_GIT_DEPTH` to container environment.
4. **Inject resolved secrets** — `GITHUB_TOKEN` is injected as a container environment variable (same as any environment-type secret).
5. **Ensure writable workspace** — the container's `/workspace` directory must be writable. For Docker, this is automatic (container filesystem). For Kubernetes, an `emptyDir` volume at `/workspace`.

### 9.2 Docker Runtime

No init container is needed for Docker. The `sciontool init` entrypoint handles the clone as part of its startup sequence. The Docker run command looks like:

```bash
docker run -t -d \
  -e GITHUB_TOKEN=ghp_xxx \
  -e SCION_GIT_CLONE_URL=https://github.com/acme/widgets.git \
  -e SCION_GIT_BRANCH=main \
  -e SCION_GIT_DEPTH=1 \
  -e SCION_AGENT_NAME=my-agent \
  ... \
  claude-sandbox:latest \
  sciontool init -- tmux new-session -s scion claude
```

### 9.3 Kubernetes Runtime

For Kubernetes, the clone can happen either in an init container or via `sciontool init` in the main container. Using `sciontool init` is preferred for consistency with Docker:

```yaml
spec:
  containers:
  - name: agent
    image: claude-sandbox:latest
    command: ["sciontool", "init", "--"]
    args: ["tmux", "new-session", "-s", "scion", "claude"]
    env:
    - name: GITHUB_TOKEN
      valueFrom:
        secretKeyRef:
          name: scion-git-creds-<grove-slug>
          key: token
    - name: SCION_GIT_CLONE_URL
      value: "https://github.com/acme/widgets.git"
    - name: SCION_GIT_BRANCH
      value: "main"
    volumeMounts:
    - name: workspace
      mountPath: /workspace
  volumes:
  - name: workspace
    emptyDir: {}
```

---

## 10. GCS Workspace Sync (Side Note)

For groves that use GCS-based workspace synchronization (non-git workspaces, or groves where the user prefers file sync over git clone), the existing `sync-design.md` flow applies. Key differences:

| Aspect | Git Clone | GCS Sync |
|--------|-----------|----------|
| Source of truth | Git remote | GCS bucket |
| History | Full git history (within depth) | No history |
| Push-back | `git push` to remote | `scion sync from` to download |
| Branch awareness | Yes | No |
| Credential type | Git PAT | GCS service account / signed URLs |
| Offline capability | Full (after clone) | Full (after sync) |

These two strategies are **mutually exclusive per grove** — a grove either has a git remote (and uses clone) or does not (and uses GCS sync). The presence of `gitRemote` on the grove record determines which path is used.

A more detailed design for GCS workspace sync improvements should be captured in a separate document.

---

## 11. Web UI Visibility

### 11.1 Status Reporting

All state transitions during agent startup with git clone are reported through the existing Hub event system:

1. **Agent created** → `PENDING` (Hub creates agent record)
2. **Container starting** → broker reports container status
3. **Clone starting** → `CLONING` (sciontool reports via `POST /api/v1/agents/{id}/status`)
4. **Clone complete** → `STARTING` (sciontool reports)
5. **Harness ready** → `RUNNING` (sciontool reports)

The `CLONING` status should include metadata about the clone operation:

```json
{
  "status": "CLONING",
  "metadata": {
    "repository": "github.com/acme/widgets",
    "branch": "main"
  }
}
```

### 11.2 Error Reporting

If the clone fails, the error is reported with actionable context:

```json
{
  "status": "ERROR",
  "error": "git clone failed: authentication failed",
  "metadata": {
    "repository": "github.com/acme/widgets",
    "phase": "git-clone",
    "suggestion": "Check that GITHUB_TOKEN is set and has Contents read access"
  }
}
```

### 11.3 Event Stream

The existing WebSocket event endpoints (`WS /api/v1/agents/{id}/events`, `WS /api/v1/groves/{id}/events`) carry these status updates in real-time. The web frontend can display clone progress and errors without polling.

---

## 12. End-to-End Example

### 12.1 Setup (One-Time)

```bash
# Create a grove from a GitHub repository
$ scion hub grove create https://github.com/acme/widgets.git
Grove created:
  ID:     a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Slug:   widgets
  Remote: github.com/acme/widgets

# Store a GitHub PAT as a grove secret
$ scion hub secret set GITHUB_TOKEN --grove widgets ghp_xxxxxxxxxxxxxxxxxxxx
Secret 'GITHUB_TOKEN' set for grove 'widgets'
```

### 12.2 Start an Agent

```bash
# Start an agent on the grove (no local checkout needed)
$ scion start my-coder --grove widgets "implement user authentication"
Agent 'my-coder' starting on grove 'widgets'...
  Cloning github.com/acme/widgets (branch: main)...
  Branch: scion/my-coder
  Status: RUNNING

# Attach to interact
$ scion attach my-coder --grove widgets
```

### 12.3 Agent Operations (Inside Container)

```bash
# Agent is in /workspace with a full git clone
$ pwd
/workspace

$ git branch
* scion/my-coder
  main

$ git remote -v
origin  https://github.com/acme/widgets.git (fetch)
origin  https://github.com/acme/widgets.git (push)

# After making changes...
$ git add .
$ git commit -m "implement user auth module"
$ git push -u origin scion/my-coder
```

### 12.4 Multiple Agents

```bash
# Start additional agents on the same grove
$ scion start reviewer --grove widgets "review the auth PR"
$ scion start tester --grove widgets "write tests for the auth module"

# Each gets its own branch
# reviewer → scion/reviewer
# tester   → scion/tester
```

---

## 13. Implementation Plan

### Phase 1: Hub-First Grove Creation

1. Add `scion hub grove create <git-url>` command to `cmd/hub.go`.
2. Add `IsGitURL()` and `ToHTTPSCloneURL()` utilities to `pkg/util/git.go`.
3. Store clone URL as grove label (`scion.dev/clone-url`).
4. Verify existing `scion hub secret set` works for this flow (no changes expected).

### Phase 2: sciontool Git Clone

1. Add `gitCloneWorkspace()` function to `cmd/sciontool/commands/init.go`.
2. Add `CLONING` status reporting.
3. Configure git identity and credential helper.
4. Create and checkout agent feature branch.
5. Handle errors with actionable messages.

### Phase 3: Runtime Broker Integration

1. Add `GitCloneConfig` to agent config models.
2. Update `ProvisionAgent()` to detect git clone mode and skip worktree creation.
3. Update `buildCommonRunArgs()` to inject git clone environment variables.
4. Update Docker and Kubernetes runtime paths.

### Phase 4: Hub Orchestration

1. Update Hub's agent creation handler to resolve grove git remote and inject `gitClone` config into `CreateAgent` payload.
2. Resolve `GITHUB_TOKEN` secret and include in agent environment.
3. Pass `SCION_GIT_CLONE_URL` and `SCION_GIT_BRANCH` through to broker.

### Phase 5: Testing and Polish

1. Integration tests for the full flow (create grove → set secret → start agent → verify clone).
2. Error case testing (bad URL, expired token, private repo without token, network failure).
3. Web UI display of `CLONING` status.
4. Documentation updates.

---

## 14. Open Questions

### 14.1 Slug Format

Should the default slug for `scion hub grove create https://github.com/acme/widgets.git` be:
- `widgets` (repo name only) — simpler, but collides across orgs
- `acme-widgets` (org-repo) — avoids collisions, slightly longer
- `acme/widgets` (path-style) — matches GitHub convention but introduces `/` in slug

**Recommendation**: Default to `widgets`, fall back to `acme-widgets` on collision, allow `--slug` override.

### 14.2 Clone Depth

Should the default clone depth be:
- `1` (shallow) — fastest, but limits `git log`, `git blame`, etc.
- `0` (full) — slower initial clone, but full history available
- Configurable per grove or per agent?

**Recommendation**: Default to depth 1 for speed. Allow override via `--depth` on `scion start` or as a grove label. Agents that need history can run `git fetch --unshallow`.

### 14.3 Token Refresh for Long-Running Agents

Fine-grained PATs have user-configured expiration (7 days to 1 year, or no expiration). If a token expires while an agent is running:
- Push operations will fail.
- The user must update the secret and restart the agent (or the agent must be able to re-read the secret).

Should we implement a token refresh mechanism (e.g., sciontool periodically checks if the token is still valid)? Or defer this to the GitHub App phase?

**Recommendation**: Defer. Users can set long-lived tokens. Token refresh is better solved by GitHub App integration in a future phase.

### 14.4 SSH URL Input

If a user provides an SSH URL (`git@github.com:org/repo.git`), should we:
- Convert to HTTPS for cloning (since we're using PAT auth)?
- Support SSH key-based auth as an alternative?

**Recommendation**: Always convert to HTTPS for clone operations in this phase. SSH key support can be added later as an alternative auth method.

### 14.5 Private Fork Considerations

Some workflows involve forking. Should `scion hub grove create` accept a fork URL and track the upstream? Or should forks be treated as independent groves?

**Recommendation**: Treat forks as independent groves. Fork/upstream relationships are a GitHub concept and can be handled at the PR level by the agent.

### 14.6 Default Branch Detection

Rather than assuming `main`, should `scion hub grove create` probe the remote to detect the default branch (e.g., via `git ls-remote --symref <url> HEAD`)?

**Recommendation**: Yes, if a `GITHUB_TOKEN` is already available at grove creation time. Otherwise, default to `main` and allow override via `--branch`.

### 14.7 Workspace Persistence Across Restarts

When an agent container is stopped and restarted, should the workspace persist (avoiding re-clone)?

- **Docker**: container filesystem persists across stop/start but not across delete/create.
- **Kubernetes**: `emptyDir` is lost when the pod is deleted.

**Recommendation**: Accept re-clone on container recreation. For Docker, workspace persists naturally across stop/start. For fresh starts, a shallow clone is fast enough for most repositories.
