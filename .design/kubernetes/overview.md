# Kubernetes Runtime Design

## Overview
This document outlines the design for adding a Kubernetes (K8s) runtime to the `scion-agent` CLI. This will allow agents to execute as Pods in a remote or local Kubernetes cluster, enabling scalability, resource management, and isolation superior to local Docker execution.

## Goals
- Allow `scion run` to execute agents in a Kubernetes cluster.
- Maintain a developer experience (DX) as close as possible to the local `docker` runtime.
- Support "Agent Sandbox" technologies for secure execution.
- Solve the challenges of remote file system access and user identity using the unified Harness abstraction.
- Support a single 'grove' (project) utilizing a mix of local and remote agents.

## Architecture

The `scion` CLI will act as a Kubernetes client, interacting directly with the Kubernetes API (using `client-go`) to manage the lifecycle of agents.

```mermaid
graph TD
    CLI[Scion CLI] -->|KubeConfig| API[Kubernetes API Server]
    API -->|Schedule| Node[K8s Node]
    Node -->|Run| Pod[Agent Pod]
    CLI -->|Stream Logs/Exec| Pod
    CLI -->|Port Forward| Pod
```

## Key Challenges & Solutions

### 1. The Context Problem (Source Code & Workspace)
In the local `docker` runtime, we bind-mount the project directory. In K8s, the Pod is remote.

#### Solution: Snapshot & Sync (Copy-on-Start)
We will use a "Snapshot" approach for the MVP to align with the "run this task" mental model.
*   **Startup:**
    1.  Create Pod with an `EmptyDir` volume for `/workspace`.
    2.  Wait for Pod to be `Running`.
    3.  `tar` the local directory (respecting `.gitignore`) and stream it to the Pod:
        `tar -cz . | kubectl exec -i <pod> -- tar -xz -C /workspace`
    4.  Start the agent process using the command provided by the harness.

#### Data Synchronization (Sync-Back)
Since the workspace is ephemeral, changes made by the agent must be explicitly retrieved.
*   **Manual Sync:** A new command `scion sync <agent-name>` will stream files from the Pod's `/workspace` back to the local directory.
*   **On Stop:** When `scion stop <agent-name>` is called, the CLI will prompt (or accept a flag `--sync`) to pull changes before destroying the Pod.
    *   *Mechanism:* `kubectl exec -i <pod> -- tar -cz -C /workspace . | tar -xz -C ./local/path`
*   **Future improvement** Use mutagen for live sync

### 2. The Identity Problem (Auth & Config)
Different agents (Gemini, Claude) require different authentication credentials and configuration files.

#### Solution: Harness-Driven Secret Projection
The `KubernetesRuntime` will rely on the `harness.Harness` interface to discover and project identity.

*   **Auth Discovery:** The runtime will call `Harness.DiscoverAuth(agentHome)` to identify necessary credentials (e.g., API keys, default credentials).
*   **Environment:** `Harness.GetEnv` will provide necessary environment variables (including API keys), which will be injected into the Pod definition.
*   **Volume Projection:** The runtime will iterate over volumes returned by `Harness.GetVolumes` and those defined in `scion-agent.json`.
    *   **Mechanism:** For **local files** (e.g., `~/.config/gcloud`, `~/.anthropic`), the CLI will create ephemeral **Kubernetes Secrets** containing these files and mount them into the Pod at the target locations.
    *   *Note:* Care must be taken with large directories. For MVP, we may restrict support to small credential files.

### 3. Security & Isolation (Agent Sandbox)
#### Solution: K8s agent sandbox
The `KubernetesRuntime` will support a https://github.com/kubernetes-sigs/agent-sandbox - This project is developing a Sandbox Custom Resource Definition (CRD) and controller for Kubernetes. A research note on this is available in `agent-sandbox.md` in the `.design/kubernetes` folder of this repo.

## Local Representation & State

Even though agents run remotely, their "handle" must remain local to maintain a consistent CLI experience.

### Directory Structure
We will retain the `.scion/agents/<agent-name>/` directory for every agent, regardless of runtime.

*   **`.scion/agents/<agent-name>/scion-agent.json`**:
    *   **`harness`**: `"claude-code"` (or `"gemini-cli"`)
    *   **`runtime`**: `"kubernetes"`
    *   **`kubernetes`**: (Read-only metadata)
        *   `cluster`: "my-cluster-context"
        *   `namespace`: "scion-agents"
        *   `podName`: "scion-agent-xyz-123"
        *   `syncedAt`: Timestamp of last sync.
*   **`.scion/agents/<agent-name>/pod.yaml`**: The generated Pod specification used to create the agent.

### State Management
*   **Listing:** `scion list` will iterate through `.scion/agents/`. For K8s agents, it will perform a lightweight API check (e.g., `GetPod`) to update the status (Running/Completed/Error).
*   **Orphaned Pods:** If a local agent directory is deleted, the CLI should eventually allow "garbage collection" of managed Pods in the cluster via labels (`managed-by=scion`).

## Grove Configuration

A "Grove" (the current project context) needs to define where its remote agents should live. This configuration can be provided via an optional `kubernetes-config.json` in the project's `.scion/` directory.

### Configuration Schema (`kubernetes-config.json`)

We will rely on the user's standard `~/.kube/config` for authentication and endpoint details, avoiding the need to manage sensitive credentials within Scion itself.

```json
{
  "context": "minikube",        // Optional: specific kubeconfig context to use
  "namespace": "scion-dev",     // Optional: target namespace (default: default)
  "runtimeClassName": "gvisor", // Optional: for sandboxing
  "resources": {                // Optional: default resource requests/limits
    "requests": { "cpu": "500m", "memory": "512Mi" },
    "limits": { "cpu": "2", "memory": "2Gi" }
  }
}
```

## Runtime Selection & Preferences

To ensure maximum flexibility, the choice between `docker` and `kubernetes` runtimes follows a strict resolution hierarchy.

### Resolution Hierarchy (Precedence)
1.  **Command-line Flag:** `scion run --runtime kubernetes` (One-time override).
2.  **Agent State:** `.scion/agents/<name>/scion-agent.json` (Locked to the runtime chosen at creation).
3.  **Template Config:** `templates/<name>/scion-agent.json` (Specific to an agent type/requirement).
4.  **Grove (Project) Preference:** `.scion/settings.json` (Project-wide defaults).
5.  **Global Preference:** `~/.scion/settings.json` (User-wide defaults).
6.  **Default:** `docker`.

### Sticky Runtimes
Once an agent is created, its runtime is **immutable** and stored in its local `scion-agent.json`. Subsequent `start`, `stop`, or `attach` commands will always use the runtime specified in the agent's state, regardless of changes to global or grove settings.

## Implementation Plan

The implementation will build upon the `Runtime` interface and the new `Harness` abstraction.

### 1. Kubernetes Runtime Construction
Create `pkg/runtime/kubernetes` and implement the `Runtime` interface.
*   **`Run(ctx, config)`**:
    *   **Harness Initialization:** Use `config.Harness` to determine harness specifics.
    *   **PodSpec Generation:**
        *   **Image**: Use `config.Image`.
        *   **Command**: Use `config.Harness.GetCommand(config.Task, config.Resume)`.
        *   **Env**: Merge `config.Harness.GetEnv(...)` with `config.Env`.
        *   **Volumes**: Iterate through `config.Harness.GetVolumes()` and `config.Volumes`.
            *   Read local content for file-based volumes.
            *   Create K8s Secrets/ConfigMaps with content.
            *   Add Volume and VolumeMounts to PodSpec.
    *   **Execution**:
        *   Create Pod via K8s Client.
        *   Wait for Ready state.
        *   Execute "Snapshot" sync (tar/untar) to `/workspace`.
        *   Exec start command (if not using container entrypoint).
*   **`Stop`/`Delete`**:
    *   Delete Pod.
    *   Delete associated Secrets/ConfigMaps (managed via OwnerReferences for auto-cleanup).

### 2. Client & API
*   Use `client-go` for standard operations.
*   Implement `KubernetesRuntime` struct satisfying `pkg/runtime/Runtime`.

### 3. Verification
*   Verify with both `gemini-cli` and `claude-code` harnesses to ensure the abstraction holds for the remote K8s environment.

## Future Work
*   **Sidecar Syncing:** Integrate with tools like Mutagen for real-time bidirectional syncing.
*   **Web Attach:** Provide a web-based gateway/proxy to attach to agents via browser.
*   **Job Mode:** Support running agents as K8s Jobs for finite, non-interactive tasks.
