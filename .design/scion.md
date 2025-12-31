# Scion Design Specification

This document details the design for `scion`, a container-based orchestration tool for managing concurrent Gemini CLI agents. The system enables parallel execution of specialized sub-agents with isolated identities, credentials, and workspaces.

## System Goals

- **Parallelism**: Run multiple agents concurrently as independent processes.
- **Isolation**: Ensure strict separation of identities, credentials (e.g., `gcloud`), and configuration.
- **Context Management**: Provide each agent with a dedicated git worktree to prevent conflicts.
- **Specialization**: Support role-based agent configuration via templates (e.g., "Security Auditor", "QA Tester").
- **Interactivity**: Support "detached" background operation with the ability for a user to "attach" for human-in-the-loop interaction.

## Architecture Overview

The system follows a Manager-Worker architecture:
- **Grove Manager (`scion`)**: A host-side CLI that orchestrates the lifecycle of agents within a **Grove** (or **Group**).
- **Grove Workers**: Isolated containers running the Gemini CLI, acting as independent agents.

### 1. Groves & Contexts

A **Grove** (alias: **Group**) is the grouping construct for a set of agents. The `.scion` directory represents a grove.

- **Grove Name**: A slugified version of the parent directory containing the `.scion` directory. If the `.scion` directory is in the user's home folder, the name is "global".
- **Resolution**: The `scion` command resolves the active grove in the following order:
  1. Explicitly via the root `--grove` (`-g`) flag.
  2. Project-level `.scion` (git root or current directory).
  3. Global `.scion` in the home directory.
- **Project Grove**: Linked to a project directory. By default, it is initialized at the root of the current git repository if one is detected, otherwise in the current directory.
- **Playground Grove**: A default global grove (`playground`) for ad-hoc agents not tied to a specific project, stored in the user's home directory.

### 2. Agent Templates
...
### 3. Grove Manager CLI (`scion`)

The `scion` tool manages the lifecycle of groves and agents.

**Root Flags:**
- `--grove`, `-g <path>`: Explicitly specify the path to a `.scion` grove directory.

**Grove Commands:**
- `scion grove init`: Initialize the `.scion/` directory representing a grove.
...
**Agent Commands:**
- `scion start <agent-name> <task...>`: Provision and launch a new agent in the current grove.
- `scion list [--all, -a]`: Show running agents. By default, only shows agents in the current grove. Use `--all` to show agents from all groves.
- `scion attach <agent>`: Connect the host TTY to the agent's container session.
- `scion stop <agent>`: Gracefully terminate an agent and cleanup resources.

### 4. Resource Isolation

Each agent runs in a dedicated container with strictly isolated resources.

- **Filesystem**:
  - **Host Path**: `.scion/agents/<agent-name>/home` (Project) or `~/.scion/agents/...` (Playground).
  - **Container Mount**: `/home/gemini`.
  - **Contents**: Populated from the template at startup. Includes unique `settings.json`, `.config/gcloud`, persistent `.gemini/history`, and an updated `scion-agent.json` containing agent-specific metadata.
- **Namespace Labeling**: Every agent container is labeled with `scion.grove=<grove-name>`. This grove name is also written to an `agent` section in the `scion-agent.json` file within the agent's home directory.
- **Network**:
  - Agents share the `gemini-cli-sandbox` bridge network but are otherwise isolated.

### 5. Workspace Strategy (Git Worktrees)

To allow concurrent modification of the codebase without conflicts, `scion` uses `git worktree`.

1.  **Creation**: On `start`, the Manager creates a new worktree on the host.
    - Path: `../.scion_worktrees/<grove>/<agent>` (kept outside the main worktree to avoid recursion).
    - Branch: Creates a new feature branch for the agent.
2.  **Mounting**: The worktree is mounted to `/workspace` inside the container.
3.  **Sync**: The shared `.git` directory ensures all agents see the same repository history, while working directories remain independent.

### 6. Runtime & Execution

Agents run as **detached containers** with allocated TTYs.

- **Launch Command**:
  The platform-specific runtime (`container` on macOS, `docker` on Linux) is used:
  ```bash
  RUNTIME run -d -t \
    --name <grove>-<agent> \
    -v <host_home_path>:/home/gemini \
    -v <host_worktree_path>:/workspace \
    gemini-cli-image
  ```
- **Platform Constraints (macOS)**:
  - The Apple `container` CLI has a limitation where the **same host source directory** cannot be mounted to multiple destinations (causes VirtioFS tag conflicts).
  - **Design Compliance**: `scion` adheres to this by ensuring `<host_home_path>` and `<host_worktree_path>` are always distinct, non-overlapping directories on the host.
- **"Yolo" Mode**: Configurable via `settings.json` or CLI flag. Enables the agent to execute tools without requiring user confirmation for every step.

### 7. Observability & Human-in-the-Loop

The system provides visibility into agent states and facilitates intervention.

- **Status Mechanism**:
  - Agents write their state to a file: `/home/gemini/.gemini-status.json`.
  - **States**: `STARTING`, `THINKING`, `EXECUTING`, `WAITING_FOR_INPUT`, `COMPLETED`, `ERROR`.
- **Intervention Loop**:
  1.  Agent hits a tool requiring confirmation.
  2.  Agent updates status to `WAITING_FOR_INPUT`.
  3.  Manager polls status and alerts the user (via `list` or notification).
  4.  User runs `scion attach <agent>` to take control.
  5.  User provides input/confirmation and detaches (Ctrl-P, Ctrl-Q).
  6.  Agent resumes `EXECUTING`.
