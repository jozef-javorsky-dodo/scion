# Technical Debt Review - Agent Lifecycle & Architecture

## Overview
This document tracks findings from a deep technical debt review of the Scion Agent system, focusing on agent lifecycle, settings inheritance, and runtime/harness abstractions.

## Findings

### 1. Harness Abstraction Leaks
The `Harness` interface is currently focused on the runtime execution phase (generating commands, env vars), but it lacks proper hooks for the provisioning phase. This has led to abstraction leaks where harness-specific logic resides in the core `agent` package.

-   **Provisioning Leak**: `pkg/agent/provision.go` contains `UpdateClaudeJSON`, which directly manipulates Claude-specific configuration files. This function should be part of the `ClaudeCode` harness implementation.
-   **Template Seeding Leak**: `pkg/config/init.go`'s `SeedTemplateDir` contains hardcoded `if harness == "claude"` logic to determine directory names (`.claude` vs `.gemini`) and file lookups. This prevents adding new harnesss without modifying core config code.
-   **Missing Lifecycle Hooks**: The `Harness` interface needs methods like `OnProvision(agentHome string)` or `ConfigureAgent(agentConfig *api.ScionConfig)` to encapsulate setup logic.

### 2. Configuration & Settings Scalability
The configuration system uses strong typing that couples the core `Settings` struct to every supported runtime and harness.

-   **Monolithic Settings Struct**: `pkg/config/settings.go` defines `Settings` with specific fields for `Kubernetes` and `Docker`. As we add more runtimes (e.g., `Firecracker`, `Remote`) or harnesss, this struct will grow indefinitely.
-   **Recommendation**: Move to a more dynamic structure for runtime-specific settings (e.g., `map[string]interface{}` or `json.RawMessage`) or a registry pattern where runtimes define their own config schemas.
-   **Error Handling**: `LoadSettings` suppresses some errors (e.g., corrupt files) which might lead to silent failures or unexpected defaults.

### 3. Runtime & Image Logic
The `Start` function in `pkg/agent/run.go` is becoming a "god function" for runtime orchestration.

-   **Image Logic Coupling**: Logic for appending `:tmux` tags and resolving default images (`gemini-cli-sandbox`) is hardcoded in `Start`. This makes it difficult to support other agents that might use different tagging conventions or default images.
-   **Complex Flag Resolution**: The resolution priority (CLI flags > Agent Config > defaults) is handled procedurally in `Start`. This logic is duplicated for `Model`, `Detached`, `Image`, etc.
-   **User Assumptions**: `pkg/harness/gemini_cli.go` uses `unixUsername` to construct paths, but some logic (like `UpdateClaudeJSON`) hardcodes container paths like `/repo-root`. Consistency is needed in how container paths are mapped.

### 4. Code Reuse & Organization
-   **`GetAgent` Complexity**: The logic to load an agent, find its template chain, merge configurations, and apply overrides is complex and spread across `pkg/agent/provision.go`. It duplicates some logic found in `ProvisionAgent`.
-   **No Rollback**: `ProvisionAgent` performs multiple filesystem and git operations. If a later step fails (e.g., `UpdateClaudeJSON` fails), the earlier steps (created directories, worktrees) are not automatically rolled back, leaving the agent in a "half-provisioned" state.

### 5. Kubernetes Runtime Specifics
-   **Redundant Auth Injection**: `pkg/runtime/k8s_runtime.go` manually re-injects auth environment variables (`GEMINI_API_KEY`, etc.) that should have already been handled by the `Harness` and passed via `RunConfig.Env`. This violates the separation of concerns.
-   **Fragile Sync**: `syncContext` relies on `tar` being present in the container image. This limits compatibility with minimal images (e.g., distroless).
-   **Log Scalability**: `GetLogs` fetches the entire log history, which will become a performance bottleneck for long-running agents. Streaming or tailing is required.

### 6. Future Control Plane Alignment
-   The current file-based status and config are sufficient for CLI usage but will require significant refactoring for the proposed API-based Control Plane (`control-plane-design.md`).
-   **State**: Agent state is currently "fire and forget" in `scion-agent.json` or calculated from container status. A persistent state store will be needed.

## Proposed Action Items

1.  **Refactor Harness Interface**:
    -   Add `Provision(ctx context.Context, agentHome string, workspace string) error`.
    -   Add `GetTemplateDir() string` (to replace hardcoded `.gemini`/`.claude` checks).
    -   Move `UpdateClaudeJSON` into `ClaudeCode.Provision`.
2.  **Decouple Settings**:
    -   Refactor `Settings` to use a generic `RuntimeConfig map[string]interface{}`.
3.  **Clean up `Start`**:
    -   Extract image resolution and flag merging into a `ResolveRunConfig` helper.
4.  **Strengthen Error Handling**:
    -   Implement rollback in `ProvisionAgent`.
    -   Make `LoadSettings` return specific errors for corrupt files.
5.  **Kubernetes Improvements**:
    -   Remove manual auth injection in `k8s_runtime.go`.
    -   Investigate `kubectl cp` style mechanisms or ephemeral containers for syncing if `tar` is missing.
