# Multi-Host Agent Management

## Status
**Proposed**

## 1. Overview
In the "Hosted" architecture, a Grove (project) can span multiple Runtime Brokers. This document outlines the UX and implementation for specifying which host should run an agent during creation, and how existing agents are managed across hosts.

## 2. Goals
*   **Explicit Host Selection:** Allow users to specify a preferred host via a `--host` flag.
*   **Default Behavior:** Leverage the Grove's `defaultRuntimeBrokerId` for common operations.
*   **Interactive UX:** Provide a multiple-choice list of available hostnames when selection is ambiguous.
*   **Location Transparency:** Existing agent operations (stop, attach, delete) should use agent name lookup, with the Hub handling routing to the correct host.
*   **Global Grove Support:** Ensure the co-located Hub/Host setup (Solo/Hybrid) defaults to the local server.

## 3. User Experience

### 3.1. Agent Creation (`scion create` and `scion start`)

When creating a new agent in Hub mode, the CLI will resolve the target host using the following priority:

1.  **Explicit Flag:** `--host <hostname|id>`
2.  **Interactive Selection:** If multiple online hosts are available for the grove and no host is specified, the CLI will present a list.
3.  **Grove Default:** If only one host is registered or a `defaultRuntimeBrokerId` is set in the grove, it is used automatically.
4.  **Error Fallback:** If no hosts are available, the Hub returns an error.

#### Example: Explicit Host
```bash
scion start fix-bug --host my-laptop
```

#### Example: Interactive Selection
If `scion start fix-bug` is run and the grove has multiple online hosts:
```
Multiple runtime brokers available for grove 'my-project':
1) my-laptop (online) *default*
2) prod-k8s (online)
3) dev-server (offline)

Select a host [1]: _
```

### 3.2. Managing Existing Agents (`stop`, `attach`, `delete`, `message`)

Operations on existing agents do **not** require a `--host` flag. The Hub tracks the `runtimeBrokerId` for every agent and routes commands accordingly.

```bash
# Hub knows 'fix-bug' is on 'prod-k8s' and routes the request
scion stop fix-bug
```

### 3.3. Hub Integration with Solo Mode

For users running the co-located Hub/Host (e.g., `scion server start --enable-hub --enable-runtime-broker`), the `Global` grove is initialized with the local server as the default host. This ensures that `scion start` without any context continues to work as expected.

## 4. Implementation Details

### 4.1. Hub API Changes

The Hub already supports `runtimeBrokerId` in the `CreateAgentRequest`. The `resolveRuntimeBroker` helper in `pkg/hub/handlers.go` implements the resolution logic:

1.  If `requestedHostId` is provided, verify it's a contributor.
2.  Otherwise, use `grove.DefaultRuntimeBrokerId` if online.
3.  If exactly one contributor exists, use it.
4.  If ambiguous, return `422 Unprocessable Entity` with `ErrCodeNoRuntimeBroker` and the list of `availableHosts` in the error details.

### 4.2. CLI Changes

#### `CreateAgentRequest` Update
The CLI's `createAgentViaHub` (in `cmd/create.go` and `cmd/start.go`) must be updated to include the `RuntimeBrokerID` if provided by the user.

#### Host Resolution and Prompting
The CLI should implement a `ResolveHost` helper:
1.  Try to use the `--host` flag value.
2.  If the Hub returns `ErrCodeNoRuntimeBroker`, the CLI catches the error, parses the `availableHosts` from the details, and prompts the user (if terminal is interactive).
3.  After selection, the CLI retries the creation request with the selected `runtimeBrokerId`.

#### Flag Implementation
Add `--host` flag to:
- `scion create`
- `scion start`
- `scion resume`

### 4.3. Hub Dispatcher
The `HttpDispatcher` in `pkg/hub/httpdispatcher.go` handles the actual routing of commands to Runtime Brokers based on the `RuntimeBrokerID` stored in the agent record.

## 5. Security & Collisions

### 5.1. Container Name Collisions
Since agents are identified by name within a grove, and a runtime broker may be running agents from multiple groves, there is a risk of container name collisions.
*   **Resolution:** Runtime hosts should prefix container names with a unique identifier (e.g., `scion-<grove-slug>-<agent-name>`).
*   **Guard Rails:** The Hub enforces agent name uniqueness within a grove.

### 5.2. Host Authorization
Only hosts that have registered as contributors to a grove can run agents for that grove. This is enforced by the Hub during the `resolveRuntimeBroker` phase.
