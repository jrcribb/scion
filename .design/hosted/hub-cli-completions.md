# Hub CLI Completions Design

## Overview
This document outlines the design for extending the `scion` CLI completion mechanism to support hosted agents managed by the Scion Hub. Currently, completions only scan the local filesystem, which is insufficient for the hosted architecture where agents may run remotely.

## Current State
The current completion logic (`cmd/completion_helper.go`) scans:
1.  The current grove's `agents/` directory.
2.  The global grove's `agents/` directory.

This works well for `local` mode but fails to discover agents that are:
*   Running on remote Runtime Brokers.
*   Managed by the Hub but not present locally.

## Goals
1.  **Unified Discovery**: Completion should list agents from both the local filesystem and the Hub (if enabled).
2.  **Responsiveness**: Shell completions must be fast (< 500ms). Network calls shouldn't hang the shell.
3.  **Graceful Degradation**: If the Hub is unreachable or auth is missing, fall back to local files without erroring.
4.  **Context Awareness**: Respect `--grove` flags and Hub configuration.

## Proposed Design

### Hybrid Discovery Strategy
We will modify `getAgentNames` to merge results from two sources:
1.  **Local Scan** (Existing): Fast, reliable for local-only agents.
2.  **Hub API** (New): Fetches remote agents if Hub is configured.

### The "Fast-Path" Hub Client
To ensure responsiveness:
1.  **Short Timeout**: The Hub client used for completion will have a strict timeout (e.g., 500ms - 1s). If the API doesn't respond in time, we silently skip Hub results.
2.  **Simplified Auth**: Reuse existing credentials (token/API key) from `config` or `~/.scion`. If login is required (interactive), skip Hub completion.

### Caching Strategy
To mitigate latency and ensure responsiveness, we will implement a "write-through" cache.

*   **Cache Location**: `~/.scion/cache/agents_<grove_hash>.json` (Using a hash of the grove path ensures uniqueness).
*   **Write**: Every successful `scion list` or `scion agent list`, and every successful completion API call, writes the list of agent names to the cache file.
*   **Read Strategy (Option A - Cache-Fallback)**:
    1.  Initiate Hub API call with a short timeout (e.g., 500ms).
    2.  If API succeeds, use API results and update cache.
    3.  If API times out or fails, read from the cache.
    4.  Merge with local scan results.

*Decision Logic*: We selected **Option A (Cache-Fallback)** over **Option B (Cache-First)**. Option B involves showing cached results immediately and updating in the background. While faster, Option B runs the risk of showing deleted agents or missing newly created ones during the current interaction, leading to "file not found" errors when the user hits enter. Option A prioritizes correctness while using the cache as a safety net for network instability or latency.

### API Interaction
We will use the existing `hubclient.Agents().List()` method.
*   **Filter**: `groveId` matching the current context.
*   **Fields**: We only need names. Ideally, the API would support partial responses (e.g., `?fields=name`), but fetching the standard list is likely acceptable for < 100 agents.

## User Experience

### Scenario 1: Hub Enabled, Online
User types `scion attach <TAB>`.
1.  CLI scans local `./.scion/agents/`.
2.  CLI calls Hub API `GET /agents`.
3.  Results are merged, deduplicated, and sorted.
4.  User sees all agents.
5.  List of agents is written to cache.

### Scenario 2: Hub Enabled, Slow Network
User types `scion attach <TAB>`.
1.  CLI scans local.
2.  CLI calls Hub API.
3.  Timeout reached (500ms).
4.  CLI reads from `~/.scion/cache/agents_...json`.
5.  CLI merges local and cached remote agents.
6.  User sees a mix of local and (potentially stale) remote agents. (User experience: Shell remains responsive).

### Scenario 3: Hub Disabled (`--no-hub`)
User types `scion attach --no-hub <TAB>`.
1.  CLI checks flag/config.
2.  Skips Hub call.
3.  Returns local agents only.

## Implementation Plan

1.  **Refactor `getAgentNames`**:
    *   Accept a context with timeout.
    *   Check `config.LoadSettings` to determine if Hub is enabled.
2.  **Implement `fetchHubAgents`**:
    *   Initialize `hubclient` with short timeout.
    *   Call `List`.
    *   Return names.
3.  **Merge Logic**:
    *   Combine local and remote lists.
    *   Deduplicate strings.
4.  **Safety**:
    *   Ensure no panics or printed errors (stderr usage breaks completion).

## Open Questions
*   **Latency**: Is 500ms too generous? Too strict? 
*   **Auth**: Does initializing the Hub client trigger any heavy setup (e.g., refreshing tokens) that might be too slow?
*   **Output**: Cobra expects completions on stdout. Any debug logs must go to stderr or be suppressed.

## Alternative Considerations
*   **Dedicated Completion Endpoint**: If `GET /agents` becomes heavy (fetching status, extensive metadata), we might need a `GET /agents/names` endpoint.
*   **Shell-Side Caching**: Some shells (zsh) support caching, but it's hard to control consistently across bash/zsh/fish. App-level control is better.
