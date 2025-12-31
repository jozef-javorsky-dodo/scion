# Settings & Preference Management

## Overview
As `scion-agent` grows in complexity, especially with the addition of multiple runtimes (Docker, Kubernetes), we need a stateful way to manage user preferences and defaults that are not tied to a specific agent's identity.

## Goals
- Provide a hierarchical configuration system (Global -> Grove).
- Support stateful defaults like `default_runtime`.
- Enable a `scion config` command for easy management.
- Keep agent definitions (`scion-agent.json`) separate from user preferences (`settings.json`).

## Hierarchy & Precedence
Settings are resolved in the following order (highest priority first):

1.  **Grove Settings:** `.scion/settings.json` (Specific to the current project/grove).
2.  **Global Settings:** `~/.scion/settings.json` (User-wide defaults).
3.  **Application Defaults:** Hardcoded values in the CLI.

## Schema: `settings.json`

The settings file will be a JSON document.

```json
{
  "default_runtime": "kubernetes",
  "kubernetes": {
    "default_context": "gke-prod",
    "default_namespace": "scion-agents"
  },
  "docker": {
    "host": "unix:///var/run/docker.sock"
  }
}
```

### Fields
- **`default_runtime`**: The runtime to use when creating a new agent if not specified by a flag or template. Options: `docker`, `kubernetes`.
- **`kubernetes.default_context`**: The default `kubeconfig` context for the Kubernetes runtime.
- **`kubernetes.default_namespace`**: The default namespace for agent Pods.

## The Resolver Logic

A new `pkg/config/settings.go` will handle the loading and merging of these files.

```go
type Settings struct {
    DefaultRuntime string           `json:"default_runtime"`
    Kubernetes     KubernetesSettings `json:"kubernetes"`
}

type KubernetesSettings struct {
    Context   string `json:"default_context"`
    Namespace string `json:"default_namespace"`
}

// LoadSettings loads and merges settings from the hierarchy
func LoadSettings(grovePath string) *Settings {
    // 1. Start with App Defaults
    settings := &Settings{
        DefaultRuntime: "docker",
    }
    
    // 2. Merge Global (~/.scion/settings.json)
    // 3. Merge Grove (.scion/settings.json)
    
    return settings
}
```

## CLI Management: `scion config`

We will introduce a `config` command to view and modify these settings without manual JSON editing.

### Usage Examples
- `scion config list`: Show the effective settings and where they come from.
- `scion config set default_runtime kubernetes`: Set the project-local default runtime.
- `scion config set default_runtime docker --global`: Set the user-wide default runtime.
- `scion config get kubernetes.default_context`: Retrieve a specific setting.

## Interaction with Templates & Agents
- **Templates:** If a template's `scion-agent.json` specifies a `runtime`, it **overrides** the `default_runtime` in `settings.json`.
- **Agents:** When an agent is created, the resolved runtime is **baked into** the agent's `scion-agent.json`. Changing `settings.json` later will not affect existing agents.

## Implementation Tasks
1.  Define `Settings` and `KubernetesSettings` structs.
2.  Implement `LoadSettings` with merging logic.
3.  Update `InitProject` and `InitGlobal` to seed an empty or commented `settings.json`.
4.  Add `cmd/config.go` for the CLI interface.
5.  Refactor `cmd/run.go` and `cmd/create.go` to use `LoadSettings` for runtime resolution.
