# Design: Non-Git Workspace Mount Support

## Problem Statement

Currently, when `scion` is used outside of a git repository, the agent's workspace is initialized as an empty directory within the agent's local folder (`.scion/agents/<name>/workspace`). This isolates the agent from the project code it is intended to work on. 

Unlike git repositories, where `git worktree` provides a clean way to create concurrent, isolated copies of the codebase, non-git projects require a different approach to give agents access to the project files.

## Proposed Solution

Instead of creating a local empty directory, `scion` should mount the project root (the "grove root") directly into the container's `/workspace` directory when operating in a non-git environment.

### 1. Provisioning Logic (`ProvisionAgent`)

During the provisioning phase:
- Detect if the current environment is NOT a git repository.
- If not a git repo:
    - Determine the **Host Workspace Source**:
        - If the grove is project-local (has a `.scion` directory in the project tree), the source is the parent directory of `.scion`.
        - If the grove is global (e.g., `playground`), the source is the Current Working Directory (CWD) where the `scion start` command was executed.
    - Add a `VolumeMount` entry to the agent's `scion-agent.json`:
        ```json
        "volumes": [
          {
            "source": "/absolute/path/to/host/project",
            "target": "/workspace",
            "read_only": false
          }
        ]
        ```
    - The `ProvisionAgent` function will return an empty string for the `agentWorkspace` host path to signal that no dedicated agent-local workspace directory should be managed/mounted by the runtime logic (as it is now handled via the explicit volume).

### 2. Runtime Configuration (`Start` & `buildCommonRunArgs`)

The `Start` and `buildCommonRunArgs` logic must be updated to handle cases where the workspace is provided via the `volumes` block rather than the dedicated `Workspace` field in `RunConfig`.

- **Workdir Resolution**: If `runCfg.Workspace` is empty, but a volume mount targeting `/workspace` exists in `runCfg.Volumes`, the runtime should still set `--workdir /workspace`.
- **Precedence**: The `volumes` block in `scion-agent.json` should be the source of truth for these custom mounts.

### 3. Handling Global/Playground Grove

For the global grove, the behavior should be:
- The host CWD is mounted to `/workspace`.
- This allows an agent created in the global grove to act on files in the directory where it was launched.

## Key Benefits

- **Immediate Access**: Agents have immediate access to project code in non-git environments.
- **Configurability**: The mount is explicitly recorded in `scion-agent.json`, allowing for easy inspection and manual override if needed.
- **Simplicity**: Avoids the overhead of copying entire directory trees into agent-local folders.

## Considerations & Constraints

- **Concurrent Access**: Unlike git worktrees, multiple agents in a non-git project will be sharing the *exact same* host files. This means:
    - Agents might interfere with each other if they modify the same files simultaneously.
    - Users should be aware that non-git groves do not provide the same level of filesystem isolation as git-based groves.
- **Recursion**: Since `.scion` (and thus the agent's `home`) is often inside the mounted project root, agents should be configured (via `.gitignore` equivalents or harness settings) to ignore the `.scion` directory to prevent recursive analysis of their own logs and state.
- **macOS VirtioFS**: Care must be taken to ensure that mounting the project root doesn't conflict with the `home` directory mount if they overlap. However, since the `home` directory is located at `.../.scion/agents/<name>/home`, it is a subdirectory of the project root. Docker handles nested bind mounts correctly. The Apple `container` runtime must be verified for this scenario.
