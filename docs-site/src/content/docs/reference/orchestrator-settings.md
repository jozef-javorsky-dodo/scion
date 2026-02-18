---
title: Orchestrator Settings (settings.yaml)
description: Configuration reference for the Scion CLI and orchestrator.
---

This document describes the configuration for the Scion orchestrator, managed through `settings.yaml` files. These settings control the behavior of the CLI, local agent execution, and connections to the Scion Hub.

## File Locations

Scion loads settings from the following locations, merging them in order (later sources override earlier ones):

1.  **Global Settings**: `~/.scion/settings.yaml` (User-wide defaults)
2.  **Grove Settings**: `.scion/settings.yaml` (Project-specific overrides)
3.  **Environment Variables**: `SCION_*` overrides.

## Versioned Format

Settings files use a versioned format identified by the `schema_version` field. The current version is `1`.

```yaml
schema_version: "1"
active_profile: local
default_template: gemini
```

:::note[Legacy Format]
Files without `schema_version` are treated as legacy format. Run `scion config migrate` to automatically convert legacy files to the versioned format.
:::

## Top-Level Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `schema_version` | string | **Required**. Must be `"1"`. |
| `active_profile` | string | The name of the profile to use by default (e.g., `local`, `remote`). |
| `default_template` | string | The default template to use when creating agents (e.g., `gemini`, `claude`). |

## CLI Configuration (`cli`)

General behavior settings for the command-line interface.

```yaml
cli:
  autohelp: true
  interactive_disabled: false
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `autohelp` | bool | Whether to print usage help on every error. Default: `true`. |
| `interactive_disabled` | bool | If `true`, disables all interactive prompts (useful for scripts). |

## Hub Client Configuration (`hub`)

Settings for connecting the CLI to a Scion Hub.

```yaml
hub:
  enabled: true
  endpoint: "https://hub.example.com"
  grove_id: "uuid-or-slug"
  local_only: false
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `enabled` | bool | Whether to enable Hub integration for this grove. |
| `endpoint` | string | The Hub API endpoint URL. |
| `grove_id` | string | The unique identifier for this grove on the Hub. |
| `local_only` | bool | If `true`, forces local-only operation even if the Hub is configured. |

:::caution[Moved Fields]
Legacy fields like `token`, `apiKey`, and broker identity fields (`brokerId`) have been removed. 
- **Dev Auth** is now handled via `server.auth.dev_token` (or `SCION_DEV_TOKEN`).
- **Broker Identity** is now configured in the `server.broker` section (see [Server Configuration](/reference/server-config/)).
:::

## Runtimes (`runtimes`)

Defines the execution backends available to Scion.

```yaml
runtimes:
  docker:
    type: docker
    host: "unix:///var/run/docker.sock"

  podman:
    type: podman
    host: "unix:///run/user/1000/podman/podman.sock"
  
  remote-k8s:
    type: kubernetes
    context: "my-cluster"
    namespace: "scion-agents"
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `type` | string | The runtime type: `docker`, `podman`, `container` (Apple), or `kubernetes`. |
| `host` | string | (Docker/Podman) The daemon socket or TCP address. Optional for Podman (defaults to CLI). |
| `context` | string | (Kubernetes) The kubectl context name. |
| `namespace` | string | (Kubernetes) The target namespace. |
| `tmux` | bool | Whether to wrap agent processes in a tmux session. |
| `sync` | string | File sync strategy (e.g., `tar`, `mutagen`). |
| `env` | map | Environment variables to set for the runtime. |

## Harness Configs (`harness_configs`)

Named configurations for agent harnesses. This replaces the legacy `harnesses` map.

```yaml
harness_configs:
  gemini:
    harness: gemini
    image: "us-central1-docker.pkg.dev/.../scion-gemini:latest"
    user: scion
    model: "gemini-1.5-pro"
  
  claude-beta:
    harness: claude
    image: "custom-claude:beta"
    env:
      ANTHROPIC_BETA: "true"
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `harness` | string | **Required**. The harness type (e.g., `gemini`, `claude`, `opencode`). |
| `image` | string | Container image to use. |
| `user` | string | Unix username inside the container. |
| `model` | string | Default model identifier. |
| `args` | list | Additional CLI arguments for the harness. |
| `env` | map | Environment variables injected into the container. |
| `volumes` | list | Volume mounts. |
| `auth_selected_type` | string | Authentication method selection (harness-specific). |

## Profiles (`profiles`)

Profiles bind a Runtime to a set of Harness Configs and overrides. They allow you to switch between environments (e.g., "Local Docker" vs "Remote Kubernetes") easily.

```yaml
profiles:
  local:
    runtime: docker
    default_template: gemini
    default_harness_config: gemini
    tmux: true
    harness_overrides:
      gemini:
        image: "gemini:dev"
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `runtime` | string | **Required**. Name of a runtime defined in `runtimes`. |
| `default_template` | string | Default template for agents created under this profile. |
| `default_harness_config` | string | Default harness config to use. |
| `tmux` | bool | Override the runtime's tmux setting. |
| `env` | map | Environment variables merged into the runtime environment. |
| `harness_overrides` | map | Per-harness-config overrides. Keys match `harness_configs` names. |

## Server Configuration (`server`)

When running the `scion server` (Hub or Broker), configuration is read from the `server` section of `settings.yaml`. 

See the [Server Configuration Reference](/reference/server-config/) for details.

## Environment Variable Overrides

Settings can be overridden using environment variables with the `SCION_` prefix.

| Setting | Environment Variable |
| :--- | :--- |
| `active_profile` | `SCION_ACTIVE_PROFILE` |
| `default_template` | `SCION_DEFAULT_TEMPLATE` |
| `hub.endpoint` | `SCION_HUB_ENDPOINT` |
| `hub.grove_id` | `SCION_HUB_GROVE_ID` |
| `cli.autohelp` | `SCION_CLI_AUTOHELP` |

See [Local Governance](/guides/local-governance/) for more on variable substitution.
