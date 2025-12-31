# Settings Refactor Design

## Motivation

Current configuration management suffers from coupled concerns and "tangled" logic.
- **Deep Nesting**: Previous designs nested runtimes or harnesses deeply, making it hard to reuse configurations across different environments.
- **Runtime vs Container Intersection**: Specific container images are often needed for specific runtimes (e.g., signed images for prod), but hardcoding them is inflexible.
- **Feature Flags**: Settings like `use_tmux` should be properties of the *environment* (profile), not the runtime definition itself.
- **Duplication**: Defining the same harness multiple times for different runtimes leads to redundancy and maintenance burden.

## Proposed Structure: The "Flat Registry" Model

We propose a "Relational" approach where `Runtimes`, `Harnesses`, and `Profiles` are top-level, independent entities. A `Profile` acts as the "glue" that binds a specific Runtime to specific Harness overrides.

### JSON Schema Draft

```json
{
  "active_profile": "local-dev",

  "runtimes": {
    "docker-local": { "type": "docker", "host": "unix:///var/run/docker.sock" },
    "k8s-prod": { "type": "kubernetes", "context": "gke_my-project_us-central1_my-cluster" }
  },

  "harnesses": {
    "gemini": { "image": "gemini-cli:base", "user": "root" },
    "claude": { "image": "claude-code:base", "user": "node" }
  },

  "profiles": {
    "local-dev": {
      "runtime": "docker-local",
      "tmux": true,
      "overrides": {
        "gemini": { "image": "gemini-cli:dev" }
      }
    },
    "k8s-prod": {
      "runtime": "k8s-prod",
      "tmux": false,
      "overrides": {
        "gemini": { "image": "gemini-cli:signed-prod" }
      }
    }
  }
}
```

## Key Concepts

### 1. Flat Registries
`runtimes` and `harnesses` are top-level maps. They define **what** is available, not **how** it is used in a specific context. This normalization allows defining "Gemini" or "Docker Local" once and referencing them by name.

### 2. Profiles as "Glue"
A `profile` binds a specific runtime to a set of behavior flags (like `tmux`) and harness overrides. It represents a coherent "environment" (e.g., "Local Development", "Production K8s").

### 3. Overrides
Profiles can override specific settings of a harness (like the image tag) without redefining the whole harness. This handles the "intersection" logic cleanly.

## Impact on Codebase

### `Settings` Struct
The Go struct will change to reflect the relational model.

```go
type RuntimeConfig struct {
    Type string `json:"type"`
    // Additional fields (host, context, etc.) are flattened in the JSON
    // and handled via custom unmarshaling or mapstructure.
}

type HarnessConfig struct {
    Image string `json:"image"`
    User  string `json:"user"`
}

type HarnessOverride struct {
    Image string `json:"image,omitempty"`
    User  string `json:"user,omitempty"`
}

type ProfileConfig struct {
    Runtime   string                     `json:"runtime"` // Name of the runtime in "runtimes"
    Tmux      bool                       `json:"tmux"`
    Overrides map[string]HarnessOverride `json:"overrides,omitempty"` // Key is harness name
}

type Settings struct {
    ActiveProfile string                   `json:"active_profile"`
    Runtimes      map[string]RuntimeConfig `json:"runtimes"`
    Harnesses     map[string]HarnessConfig `json:"harnesses"`
    Profiles      map[string]ProfileConfig `json:"profiles"`
}
```

### Resolution Logic
When starting an agent:
1.  **Determine Active Profile**: Check CLI arg (`--profile`), then `active_profile` in JSON, then default.
2.  **Load Profile**: Look up the profile in `profiles`.
3.  **Resolve Runtime**: Look up the referenced runtime name in `runtimes` to get the base runtime config.
4.  **Resolve Harness**: Look up the requested harness in `harnesses` to get the base harness config.
5.  **Apply Overrides**: Apply any `overrides` found in the `ProfileConfig` to the `HarnessConfig`.
6.  **Construct RunConfig**: Combine the resolved Runtime, Harness, and Profile settings (like `tmux`).

## Benefits
- **Clean Separation**: Runtimes and Harnesses are independent. Adding a new runtime doesn't require touching harness configs.
- **Normalization**: "Gemini" is defined once. "K8s Prod" is defined once. They are mixed and matched via Profiles.
- **Flexibility**: Profiles allow "patching" logic (overrides) without deep nesting or duplication.
- **Clarity**: It is easy to see the "base" state of a harness and exactly how it changes per profile.

## Additional refactor work



### 1. Configuration File Renaming

- `scion-agent.json` as a file has been renamed to `scion-agent.json`. Its purpose is the primary location for agent-specific information that is not sufficiently defined in a template or provided by the indicated runtime and harness.

### 2. Data Structure Unification: `AgentInfo`

- **Primary State Store**: `AgentInfo` becomes the primary data structure that is written to by the agent (to persist state) and is read-only by the Scion CLI tool.

- **Consolidation**: The `AgentConfig` should be fully merged into `AgentInfo`.

- **Field Relocation**: Fields that describe the agent's identity and environment should reside here, including:

    - `Template` (moved from `ScionConfig`)

    - `Runtime` (explicitly added to track which runtime launched the agent)



### 3. `ScionConfig` (Agent Override) Cleanup

To achieve the "Relational" model, `scion-agent.json` should be stripped of fields that can be resolved from registries or profiles:

- **Registry Resolution**: `UnixUsername` and `Image` should be pulled from the resolved Harness settings at runtime rather than being hardcoded in the agent config.

- **Behavioral Flags**: `UseTmux` should be resolved from the Profile/Environment settings.

- **Specialization**: `Model` should be removed from the top-level config and instead be passed via environment variables or managed within the harness-specific config directory (e.g., `.gemini/settings.json`).

- **Simplification**: Remove the nested `Agent` block entirely in favor of the flat `AgentInfo` structure for persisted metadata.

## Implementation Summary

The refactor was completed on December 30, 2025, with the following key changes:

### 1. Relational Settings Model
- Updated `pkg/config/settings.go` and `pkg/config/embeds/default_settings.json` to implement the registry-based model.
- Added `ResolveRuntime` and `ResolveHarness` methods to the `Settings` struct to handle profile-based resolution and overrides.

### 2. Configuration Unification
- Renamed all instances of agent-specific `scion.json` to `scion-agent.json`.
- Merged `api.AgentConfig` into `api.AgentInfo` in `pkg/api/types.go`.
- Relocated metadata fields like `Template`, `Image`, and `Runtime` into the `api.AgentInfo` struct within `api.ScionConfig`.

### 3. CLI and Runtime Refactoring
- Introduced a global `--profile` (`-p`) flag in `cmd/root.go`.
- Updated `RunAgent` (in `cmd/common.go`) and all related commands (`start`, `create`, `list`, `logs`, etc.) to utilize the new profile-based resolution.
- Refactored `pkg/runtime/factory.go` to use `ResolveRuntime` for initializing runtime implementations with proper configuration (host, namespace, etc.).

### 4. Resolution Logic
- Updated `pkg/agent/run.go` to resolve harness settings (Image, User) and environmental flags (Tmux) at agent launch time by querying the registries via the active profile.

## Post-Implementation Code Review Findings

### 1. Template Initialization Bug (`pkg/config/init.go`)
- **Issue**: `SeedTemplateDir` fails to correctly update the `template` field in `scion-agent.json` when creating from harness-specific defaults (`gemini`, `claude`) because it only looks for the literal string `"template": "default"`.
- **Severity**: MEDIUM
- **Recommendation**: Update replacement logic to support harness-specific placeholder names.

### 2. Root Home Directory Pathing (`pkg/runtime/common.go` & Harnesses)
- **Issue**: Hardcoded `/home/%s` paths fail for the `root` user, whose home directory is `/root`. This affects environment variable propagation (e.g., `GEMINI_SYSTEM_MD`, `GOOGLE_APPLICATION_CREDENTIALS`) and volume mounts.
- **Severity**: HIGH
- **Recommendation**: Implement a helper function to resolve the correct container home directory based on the `UnixUsername`.

### 3. Status Field Ambiguity (`pkg/api/types.go`)
- **Issue**: Overlapping use of the `Status` field. `AgentInfo.Status` is used for container state (e.g., "Up 2 minutes"), while `AgentInfo.AgentStatus` is for high-level state (e.g., "running"). However, the persisted JSON uses `status` for the high-level state, leading to confusion during list operations.
- **Severity**: MEDIUM
- **Recommendation**: Rename `Status` to `ContainerStatus` and ensure consistent mapping between persisted state and runtime state.

### 4. Tmux Image Tagging (`pkg/agent/run.go`)
- **Issue**: Blind replacement of image tags with `:tmux` strips version information (e.g., `my-agent:v1.2.3` becomes `my-agent:tmux` instead of `my-agent:v1.2.3-tmux`).
- **Severity**: LOW
- **Recommendation**: Append `-tmux` to existing tags or default to `:tmux` if no tag is present.

### 5. Redundant Runtime Fallback (`pkg/runtime/factory.go`)
- **Issue**: Redundant `LookPath` calls and fallback logic for Darwin in `GetRuntime`.
- **Severity**: LOW
- **Recommendation**: Simplify the resolution hierarchy to remove duplicate detection logic.
