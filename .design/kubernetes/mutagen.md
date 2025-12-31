# Mutagen Design for Kubernetes Runtime

## Overview
This document outlines the detailed design for integrating [Mutagen](https://mutagen.io/) into the Scion Kubernetes Runtime. Mutagen provides high-performance, real-time, bidirectional file synchronization and flexible network forwarding.

The primary goal is to provide a seamless "local-feeling" developer experience (DX) where agents run in remote Kubernetes Pods but interact with files and services as if they were local. Crucially, this design prioritizes the **preservation of the local work tree** as the source of truth for final review and merging.

## Objectives
1.  **Augmented Runtime:** Introduce Mutagen as a "Live Sync" mode, sitting alongside the existing "Snapshot & Sync" mode. It is not intended to immediately replace the simpler snapshot model but to provide an enhanced experience for interactive sessions.
2.  **Local Workflow:** Ensure the user's local git repository remains the primary interface for version control (diffs, commits, merges).
3.  **Service Access:** Transparently expose services running in the agent Pod to the local machine (e.g., `localhost:3000` -> `pod:3000`).
4.  **Resilience:** Automatically handle connection drops, latency, pod restarts, and client sleep/wake cycles.

## Architecture

The Scion CLI acts as the orchestrator, managing the lifecycle of both the Kubernetes resources (Pods, Secrets) and the Mutagen sessions.

```mermaid
graph TD
    subgraph Local Machine
        CLI[Scion CLI]
        Daemon[Mutagen Daemon]
        Worktree[Local Git Worktree]
        Browser[Web Browser / Tools]
    end

    subgraph Kubernetes Cluster
        API[K8s API Server]
        subgraph Agent Pod
            Container[Agent Container]
            Agent[Mutagen Agent]
            App[User Application]
        end
    end

    CLI -->|1. Create Pod| API
    CLI -->|2. Create Session| Daemon
    Daemon <-->|3. Sync (via Port Forward)| Agent
    Agent <-->|Read/Write| Container
    Worktree <-->|Sync| Daemon
    Browser <-->|Forward| Daemon
    Daemon <-->|Forward| App
```

### Components
1.  **Scion CLI:** Manages the higher-level "Session" concept (Agent + Sync + Forwarding).
2.  **Mutagen Daemon:** A background process on the local machine that handles the actual data transfer and state monitoring.
3.  **Mutagen Agent:** A lightweight binary automatically injected by Mutagen into the target container to facilitate efficient filesystem operations.

#### Agent Pattern: Sidecar vs. Injected Binary

The default and most common Mutagen pattern is **Injection**.
*   **Injection (Preferred):** Mutagen copies a small binary into the running container (via `kubectl cp`/`exec`) to a temporary location (e.g., `~/.mutagen/agents/`).
    *   *Pros:* No changes to PodSpec; works with any base image (provided it has tar/sh); zero overhead when not syncing.
    *   *Cons:* Requires `tar` in the container.
*   **Sidecar:** A dedicated container running the Mutagen agent, sharing a volume with the main container.
    *   *Pros:* Works with "Distroless" images; cleaner separation.
    *   *Cons:* Complicates PodSpec; requires shared volume configuration; potential permission issues.

**Decision:** Scion will use the **Injection** pattern by default for simplicity and broad compatibility, falling back to Sidecar only if specifically configured for restrictive environments.

## Workflow

### 1. Agent Start (`scion run`)

When the user runs `scion run --runtime kubernetes`, the following sequence occurs:

1.  **Pod Creation:**
    *   The CLI creates the Agent Pod in Kubernetes.
    *   It waits for the Pod to reach the `Running` state.
    *   *Note:* The Pod typically uses an `EmptyDir` or PVC for `/workspace`.

2.  **Mutagen Session Initialization:**
    *   The CLI checks for a running Mutagen daemon and starts it if necessary.
    *   **File Sync Creation:**
        ```bash
        mutagen sync create \
          --name=scion-<agent-name> \
          --label=scion-agent=<agent-name> \
          --sync-mode=two-way-safe \
          --ignore-vcs \
          ./ <remote-url>
        ```
        *   **Remote URL:** `api://<kube-context>/namespaces/<ns>/pods/<pod>/containers/<container>:/workspace` (or standard Mutagen K8s syntax).
        *   **Ignore VCS:** We explicitly **ignore `.git/`**. This is critical. The agent operates on the *files* (the checkout), but the *repository history and index* remain exclusively under local user control. This prevents the agent from making commits that might conflict or corrupt the local repo state.
    *   **Network Forwarding (Optional/Auto):**
        *   If the agent template specifies ports (e.g., `3000:3000`), or if `scion-agent.json` has config:
        ```bash
        mutagen forward create \
          --name=scion-net-<agent-name> \
          --label=scion-agent=<agent-name> \
          tcp:localhost:3000 <remote-socket>
        ```

3.  **Bootstrap Wait:**
    *   The CLI pauses and monitors the Mutagen session status.
    *   It waits until the status reaches `Watching` (indicating initial sync is complete).
    *   *User Feedback:* "Syncing files to remote agent... (12/450 files)"

4.  **Agent Execution:**
    *   Once synced, the CLI triggers the actual agent command in the Pod (if it was waiting) or simply lets the pre-configured entrypoint proceed.

### 2. Development Phase

*   **File Editing:**
    *   **User:** Edits files locally in VS Code / Vim. Mutagen syncs changes to the Pod (~ms latency).
    *   **Agent:** Runs tools, modifies code, or generates files in the Pod. Mutagen syncs changes back to local.
*   **Conflict Resolution:**
    *   Mode `two-way-safe` (default) handles most cases.
    *   If a conflict occurs (both modify same file), Mutagen creates a "conflict" file.
    *   Since the User and Agent usually take turns (User prompts -> Agent works -> User reviews), conflicts are rare.
*   **Review Process:**
    *   The user runs `git status` and `git diff` **locally**.
    *   Since `.git` was ignored, the local git sees the changes synced from the remote as "unstaged changes".
    *   The user can then `git add` and `git commit` locally, ensuring full control over the commit history.

### 3. Service Access

*   **Dynamic Forwarding:**
    *   The CLI can offer a command `scion forward <agent-name> <local>:<remote>` which wraps `mutagen forward create`.
*   **Smooth Experience:**
    *   If the agent starts a web server, the user can immediately open `http://localhost:port`. No manual `kubectl port-forward` required. Mutagen handles the reconnection automatically if the tunnel drops.

### 4. Agent Stop (`scion stop`)

1.  **Terminate Sessions:**
    *   `mutagen sync terminate --label-selector=scion-agent=<agent-name>`
    *   `mutagen forward terminate --label-selector=scion-agent=<agent-name>`
2.  **Pod Cleanup:**
    *   Delete the Kubernetes Pod and associated Secrets.
3.  **Final State:**
    *   The local directory contains the final state of the files. The work tree is preserved.

## Configuration & Customization

Configuration is handled in `.scion/agents/<agent-name>/scion-agent.json` or global settings.

```json
{
  "kubernetes": {
    "sync": {
      "mode": "two-way-safe",
      "ignore": [
        "node_modules/",
        "target/",
        ".DS_Store"
      ],
      "maxEntryCount": 50000
    },
    "forward": [
      "tcp:localhost:8080:tcp:8080"
    ]
  }
}
```

## Implementation Details

### Dependency Management
*   **Mutagen Binary:** The Scion CLI should check for `mutagen` in `$PATH`.
*   **Auto-Install (Optional):** If not found, prompt to download/install a scoped binary to `~/.scion/bin/` to avoid system-wide conflicts.

### Local Daemon Management
The local Mutagen Daemon is a per-user background process.
*   **Lifecycle:** Scion does not "own" the daemon process directly but interacts with it via the `mutagen` CLI.
*   **Startup:** When `scion run` is executed, it runs `mutagen daemon start`. If the daemon is already running, this command is idempotent.
*   **Sharing:** A single daemon instance handles multiple sessions (multiple agents). This is efficient and allows for centralized monitoring.
*   **Monitoring:** Scion will periodically poll session status (`mutagen sync list --format=json`) to report health to the user.
*   **Restart:** If the daemon crashes, the next `scion` command will attempt to restart it. Mutagen automatically attempts to reconnect sessions upon restart.

### Client Resilience (Sleep/Wake)
Mutagen is designed to handle network interruptions, including laptop sleep/wake cycles.
*   **Behavior:** When the client machine sleeps, the TCP connection to the K8s API (port forward) drops. The daemon pauses the session.
*   **Recovery:** Upon waking, the daemon detects the network availability and attempts to re-establish the port-forward tunnel.
*   **Scion's Role:** Scion does not need to intervene actively. However, the CLI status display (if running) should reflect "Connecting..." or "Sync Paused" during this window to inform the user.

### Connection Resilience
Mutagen is robust against network interruptions. However, if the **Pod** is deleted (e.g., evicted), the sync session will fail.
*   **Monitoring:** The Scion CLI needs to monitor Pod status. If the Pod dies, Scion should pause/terminate the sync session and notify the user.

### Safety Mechanisms
*   **Git Safety:** Always defaulting `ignore-vcs: true` ensures the agent cannot mess up the git index.
*   **Large Files:** Default ignores for `node_modules` or `vendor` (if strictly read-only for agent) can significantly improve performance. However, usually agents need these to run. Mutagen's rsync-based diffing is efficient enough for `node_modules` after the initial sync.

## Summary of DX Improvements

| Feature | Old Way (Snapshot) | New Way (Mutagen) |
| :--- | :--- | :--- |
| **Startup** | Slow (tar/untar) | Fast (incremental sync) |
| **Feedback** | Pull on stop (blind) | Real-time updates |
| **Review** | Sync -> Review | `git diff` works instantly |
| **Services** | Manual `kubectl port-forward` | Auto-forwarding |
| **Commit** | Sync back -> Commit | Commit anytime locally |
