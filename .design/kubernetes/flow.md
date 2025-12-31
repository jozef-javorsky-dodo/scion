# Kubernetes Agent Deployment Flow

This document traces the code path and logic for deploying a scion agent to a Kubernetes cluster using the `remote` or `k8s` runtime.

## Process Flow

### 1. Initiation
- **Command**: `scion start <agent-name>` or `scion resume <agent-name>`.
- **Entry Point**: `cmd/start.go` (or `cmd/resume.go`) triggers the `RunE` function.
- **Common Logic**: Calls `RunAgent(cmd, args, resumeBool)` in `cmd/common.go`.

### 2. Runtime Resolution
- **Location**: `cmd/common.go` calls `runtime.GetRuntime(grovePath)`.
- **Logic (`pkg/runtime/factory.go`)**:
    1.  **Env Check**: Checks `GEMINI_SANDBOX` environment variable.
    2.  **Agent Settings**: Checks `config.GetAgentSettings()` for `Tools.Sandbox`.
    3.  **Global Settings**: Checks `.scion/settings.json` (resolved via `config.LoadSettings`) for `DefaultRuntime`.
    4.  **Resolution**: Maps `remote` to `kubernetes`.
- **Instantiation**:
    -   If `kubernetes`, calls `k8s.NewClient(os.Getenv("KUBECONFIG"))` (`pkg/k8s/client.go`).
    -   Returns a `KubernetesRuntime` struct (`pkg/runtime/kubernetes/runtime.go`).

### 3. Agent Preparation
- **Location**: `cmd/common.go` (`RunAgent`).
- **Config Resolution**: Calls `GetAgent` to setup directories and config.
    -   **Paths**: Resolves `agentsDir`, `agentHome`, and `agentWorkspace`.
    -   **Templates**: Uses `config.GetTemplateChain` and `config.MergeScionConfig` to build the final `api.ScionConfig`.
- **Harness**: Instantiates the harness (e.g., `gemini`) via `harness.New`.
- **Auth**: Calls `h.DiscoverAuth(agentHome)` to load credentials (e.g., API keys) into an `api.AuthConfig` object.
- **RunConfig**: Constructs an `api.RunConfig` object containing `Name`, `Image`, `Env`, `Auth`, `Workspace`, etc.

### 4. Kubernetes Resource Provisioning
- **Entry**: `RunAgent` calls `rt.Run(ctx, runCfg)`.
- **Logic (`pkg/runtime/kubernetes/runtime.go` -> `Run`)**:
    1.  **Namespace**: Selected from labels (`scion.namespace` or `namespace`), defaulting to `default` or the runtime's configured default.
    2.  **Claim Construction**: Builds a `v1alpha1.SandboxClaim` struct.
        -   `Metadata.Name`: set to `runCfg.Name`.
        -   `Spec.TemplateRef.Name`: set to `runCfg.Template` (defaults to `default-scion-agent`).
    3.  **Creation**: Calls `r.Client.CreateSandboxClaim` to submit the resource to the API server.
    4.  **Wait**: Calls `waitForReady`.
        -   **Polling**: Loops every 2 seconds calling `GetSandboxClaim`.
        -   **Condition**: Checks if `Status.Conditions` contains `Type: Ready` with `Status: True`.
    5.  **Pod Resolution**: Calls `getPodName`.
        -   Retrieves the `SandboxClaim` to find `Status.SandboxStatus.Name`.
        -   Retrieves the `Sandbox` resource.
        -   Lookups annotation `agents.x-k8s.io/pod-name` to find the actual Pod name.

### 5. Context Synchronization
- **Condition**: If `runCfg.Workspace` is not empty.
- **Logic (`pkg/runtime/kubernetes/runtime.go` -> `syncContext`)**:
    1.  **Local Archive**: Executes `tar -cz -C <workspacePath> .` locally.
    2.  **Remote Extract**: Prepares a remote command `tar -xz -C /workspace` (destination is hardcoded).
    3.  **Transport**: Uses `remotecommand.NewSPDYExecutor` to pipe the local stdout (tar stream) to the remote command's stdin.

### 6. Finalization
- **Status Update**: `RunAgent` calls `UpdateAgentStatus` to write `"status": "running"` (or `"resumed"`) to the agent's `scion-agent.json`.
- **Attach (Optional)**: If not detached, calls `rt.Attach(ctx, id)`.
    -   **Logic**: Starts a remote shell (`/bin/sh`) using SPDY executor, connecting `os.Stdin/Stdout/Stderr` to the Pod.

---

## Identified Flaws and Limitations

### 1. Incomplete Context Sync
- **Observation**: `syncContext` only synchronizes the `Workspace` directory.
- **Flaw**: The `HomeDir` (containing `scion-agent.json`, `gemini.md`, and system prompts) is **not** synchronized to the Pod. Most harnesses expect these files in the container's home directory to function correctly.

### 2. Missing Configuration Propagation
- **Observation**: `KubernetesRuntime.Run` completely ignores several fields in `api.RunConfig`:
    -   `Env`: Environment variables prepared in `RunAgent` are dropped.
    -   `Volumes`: Volume mounts defined in `scion-agent.json` are dropped.
    -   `Auth`: Credentials discovered by `DiscoverAuth` are dropped.
    -   `Image`: The resolved image is ignored; the runtime relies entirely on the `SandboxTemplate` referenced by `TemplateRef`.
- **Flaw**: There is no mechanism in the current `SandboxClaim` to pass these per-agent overrides to the underlying Pod.

### 3. Credential Propagation
- **Observation**: `RunAgent` discovers authentication credentials locally.
- **Flaw**: These credentials (e.g., `GEMINI_API_KEY`) are stored in the `Auth` field of `RunConfig` but are never passed to the Kubernetes resource. Unless the K8s operator has out-of-band access to these secrets, the agent in the cluster will lack authentication.

### 4. Synchronous Sync Issues
- **Observation**: `syncContext` happens after the Sandbox is `Ready`.
- **Potential Issue**: If the agent process starts immediately upon Pod readiness, it might begin executing before the workspace files have finished uploading, leading to race conditions or missing files.

### 5. Hardcoded Paths
- **Observation**: `syncContext` hardcodes the destination to `/workspace`.
- **Limitation**: This may conflict with images that expect a different workspace location or different user permissions.
