# Kubernetes SCM Integration Design

## Overview

This document explores and proposes how Scion agent workloads running in Kubernetes will use `git clone` on startup instead of the git worktree approach used in local development. This design addresses the fundamental architectural difference between local container execution (where the host filesystem can be bind-mounted) and remote Kubernetes execution (where the source code must be fetched independently).

## Problem Statement

### Current Local Approach: Git Worktrees

In the local Docker and Apple Container runtimes, Scion uses git worktrees to provide each agent with an isolated view of the repository:

1. **Worktree Creation**: `git worktree add --relative-paths "./workspace" "branch-name"`
2. **Volume Mounting**: The worktree directory and `.git` reference are mounted into the container
3. **Benefits**:
   - Instant availability of source code (no network transfer)
   - Shared git object store (disk efficiency)
   - Easy synchronization with host filesystem changes
   - Native git operations work seamlessly

### Kubernetes Challenge

When agents run as Kubernetes Pods in remote clusters, the worktree approach breaks down:

1. **No Shared Filesystem**: The Pod runs on a remote node without access to the developer's local filesystem
2. **Network Isolation**: The Pod cannot access the host's git repository
3. **Ephemeral Storage**: Pods start with empty volumes that must be populated
4. **Distribution**: Multiple pods may be on different nodes, each needing independent access to source code

### Proposed Solution: Git Clone on Start

For Kubernetes-based agents, we will use `git clone` to fetch source code at Pod initialization time using a Kubernetes **Init Container**.

#### Architecture

1. **Volume Strategy**: A shared `emptyDir` volume (e.g., named `workspace`) will be mounted to both the Init Container and the Main Agent Container.
2. **Init Container**: Authenticates, clones the repo, and checks out the specific branch/commit.
3. **Main Container**: Starts with the populated workspace and uses pre-configured credentials for operations.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: scion-agent-xyz
spec:
  initContainers:
  - name: git-clone
    image: alpine/git:latest
    command:
    - sh
    - -c
    - |
      git clone --depth=1 --branch=${GIT_BRANCH} \
        ${GIT_REPO_URL} /workspace
      
      # Configure for main container
      cd /workspace
      git config user.name "Scion Agent"
      git config user.email "agent@scion.dev"
    env:
    - name: GIT_REPO_URL
      value: "https://github.com/org/repo.git"
    - name: GIT_BRANCH
      value: "main"
    volumeMounts:
    - name: workspace
      mountPath: /workspace
  containers:
  - name: agent
    image: gemini-cli-sandbox:latest
    workingDir: /workspace
    volumeMounts:
    - name: workspace
      mountPath: /workspace
  volumes:
  - name: workspace
    emptyDir: {}
```

## Repository URL Configuration

### Configuration Hierarchy

The repository URL will be resolved using the following precedence:

1. **Explicit Configuration**: `.scion/kubernetes-config.json`:
   ```json
   {
     "scm": {
       "repoUrl": "https://github.com/org/repo.git",
       "defaultBranch": "main"
     }
   }
   ```

2. **Git Remote Detection**: Automatically infer from local git config:
   ```bash
   git remote get-url origin
   # Returns: git@github.com:org/repo.git or https://github.com/org/repo.git
   ```

3. **Grove Settings**: Fall back to `.scion/settings.json` if specified

4. **Prompt User**: If no URL is detected, prompt during `scion start --runtime kubernetes`

### URL Normalization

Since different auth methods require different URL formats, Scion will normalize URLs to support both HTTPS and SSH formats tailored to the selected authentication harness.

## Authentication Strategies

Git authentication in Kubernetes requires mounting credentials as Secrets and configuring the git client to use them.

### 1. GitHub Authentication (Primary)

#### A. GitHub App (Recommended for Production)

**Use Case**: Production, team environments, fine-grained permissions.

**Advantages**:
- Fine-grained repository permissions
- Token auto-rotation
- Audit trails
- No user account dependency
- Rate limit isolation

**Architecture**:
The Scion CLI or Controller exchanges the App Private Key for a short-lived **Installation Access Token**. This token is injected into the Pod environment.

1. **GitHub App Setup**: Install app on target repositories and download private key.
2. **Token Generation**: Scion generates a JWT, exchanges it for an installation token (valid for 1 hour).
3. **Secret Injection**:
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: scion-git-credentials
   type: Opaque
   stringData:
     token: "ghs_xxxxx" # Short-lived installation token
   ```

#### B. Personal Access Token (PAT)

**Use Case**: Individual developer testing, simple automation.

**Implementation**: User provides PAT (via `scion config`), which is stored in a Kubernetes Secret. The init container configures git to use this token via basic auth in the URL or a credential helper.

### 2. Cloud-Native & Enterprise Considerations

#### GKE & GCP Secret Manager
When running on Google Kubernetes Engine (GKE), we can leverage GCP services for enhanced security:

*   **Secret Manager Integration**: Instead of manually creating Kubernetes Secrets, use the **Secret Manager add-on for GKE** (Secrets Store CSI Driver).
    *   *Mechanism*: Define a `SecretProviderClass` that maps a GCP Secret Manager secret to a file mounted in the pod.
*   **Workload Identity**: Use GKE Workload Identity to authorize the Pod (via its Service Account) to access the Secret Manager API without managing long-lived service account keys.

#### Other Harnesss
- **GitLab**: Support for PATs (`oauth2:${TOKEN}`) and Deploy Tokens.
- **Bitbucket**: App Passwords.
- **Azure DevOps**: Personal Access Tokens.
- **SSH**: Support for mounting SSH keys (`~/.ssh/id_rsa`) and `known_hosts` config map.

## Implementation Approach

### Phase 1: Basic Git Clone Support

1. **Detect Repository URL**: Implement logic to resolve URL from config or git remote.
2. **Credential Management**:
   - Implement `EnsureGitCredentials` to check/refresh secrets.
   - For GitHub Apps, implement the token exchange flow.
3. **Init Container Spec**:
   - Generate the `git-clone` init container definition.
   - Inject `GIT_TOKEN` from the appropriate secret.
   - Configure git credential helpers.

### Phase 2: Branch and Ref Handling

Support creating agents on specific branches or commits:
```bash
scion start --runtime kubernetes --branch feature/new-auth coder
```

The runtime will resolve the current branch (if not specified) or specific commit SHA and pass it to the init container.

**Automatic Branch Creation**:
If the agent is requested to start on the default branch (e.g., `main` or `master`), the initialization logic **must automatically create and checkout a new branch** named after the agent (e.g., `scion/<agent-name>`). This ensures that multiple agents do not conflict on the same branch and that the default branch remains protected.

### Phase 3: Workspace Synchronization

Since the workspace is ephemeral, we need mechanisms to sync code changes:

#### Push / PR Workflow
For standard operations, the agent acts like a developer:
1. Agent modifies files.
2. `git add .` && `git commit`.
3. `git push origin HEAD` (using the pre-configured credentials).
   > **Constraint**: The agent **must never push directly to protected branches** (like `main` or `master`). It should always push to its dedicated feature branch.
4. Agent calls Harness API to open a PR.

#### Sync-Back (Development Mode)
To pull changes from the remote pod to the local machine (for "live" feel):
- Implement `scion sync from <agent>`: Streams a tarball of the `/workspace` from the pod to the local path.

#### Sync-To (Iterative Mode)
- Implement `scion sync to <agent>`: Streams local changes to the pod.

## Security Considerations

1. **Credential Storage**: Never commit secrets. Use K8s Secrets or External Secret Stores (e.g., GCP Secret Manager).
2. **Token Lifecycle**:
   - GitHub App tokens expire after 1 hour.
   - **V1**: Assume tasks complete within this window.
   - **V2**: Implement a sidecar or refresh mechanism for long-running agents.
3. **Network Policies**: Restrict egress to allowed SCM harnesss (e.g., github.com:443).
4. **Image Security**: Use minimal, verified images (e.g., `alpine/git` or distroless).
5. **Audit Logging**: Track all git operations and clone events.

## Migration Path

1. **Detection**: Check if agent is running on Kubernetes runtime.
2. **Configuration Migration**: Convert worktree config to clone config.
3. **First Start Experience**:
   - Detect and prompt for Repo URL if missing.
   - Wizard for setting up Auth (PAT or GitHub App).

## Future Enhancements

1. **Pre-warmed Repository Cache**: Use a shared volume or caching service to speed up clones for large repos.
2. **Delta Sync**: Sync only changed files for efficiency.
3. **Git LFS Support**: Handle large files explicitly.
4. **Multi-Source Support**: Support monorepos or multiple repository mounts.
