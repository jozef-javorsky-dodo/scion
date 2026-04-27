# AMP Harness: Script-Only Third-Party Harness Example

## Motivation

The decoupled harness implementation (Phases 0–5) proved that provisioning logic can move from compiled Go code to Python scripts running inside the agent container. OpenCode and Codex were migrated from existing built-in harnesses. What has not been demonstrated yet is the primary goal of the entire effort: **a brand-new harness contributed with zero Go code changes.**

[Amp](https://ampcode.com) (Sourcegraph's terminal coding agent) is a good candidate for this:

- It is an actively-maintained third-party coding CLI, not one of the original built-in harnesses.
- It uses a straightforward API-key auth model (Anthropic key or Sourcegraph access token).
- It has a simple config layout (`~/.config/amp/`) with JSON settings.
- It supports system prompts via an `AGENT.md` file convention.
- Its container image is easy to build on `scion-base`.

This design describes an Amp harness delivered entirely through the `examples/` directory — a `config.yaml`, a `provision.py`, a Dockerfile, a template, and home directory files. No additions to `pkg/harness/`, no Go compilation, no binary changes. The example simultaneously serves as:

1. **A working Amp integration** for users who want to orchestrate Amp agents in scion.
2. **A reference implementation** for community harness authors following the same pattern.
3. **Validation of the Phase 7 goal** — proving the script provisioning contract is sufficient for a real, non-trivial harness without compiled support.

## Non-Goals

- Building a production-grade, officially-supported Amp harness embedded in the scion binary. If Amp grows significant enough to warrant that, the compiled path remains available.
- Covering every Amp auth method (OAuth, SSO, team accounts). The example targets the common path: API key and Sourcegraph access token.
- Hook dialect integration for turn/model counting. Amp's hook system (if any) is out of scope for the initial example; `max_turns` and `max_model_calls` are advertised as unsupported.

## Design

### Amp CLI Overview

Amp is a terminal-based coding agent from Sourcegraph. Key characteristics relevant to harness integration:

| Aspect | Detail |
|---|---|
| Binary | `amp` |
| Config directory | `~/.config/amp/` |
| Settings file | `~/.config/amp/settings.json` |
| Agent instructions | `AGENT.md` in the workspace root (convention) |
| System prompt | Via `--system-prompt` flag or prepended to `AGENT.md` |
| Auth | `AMP_API_KEY` env var |
| Resume | `threads continue` command (alias `t c`) |
| Task delivery | Interactive input or `-x "<task>"` for execute mode |
| Non-interactive flags | `-x` or `--execute` |

### Example Directory Structure

```
examples/amp/
├── README.md                          # Setup and usage guide
├── Dockerfile                         # Container image (based on scion-base)
├── provision.py                       # Container-side provisioning script
├── templates/
│   └── amp/
│       ├── scion-agent.yaml           # Template definition
│       ├── agents.md                  # Portable agent instructions
│       └── harness-configs/
│           └── amp/
│               ├── config.yaml        # Declarative harness metadata
│               └── home/              # Base home directory overlay
│                   ├── .bashrc        # Shell setup (activate PATH, etc.)
│                   └── .config/
│                       └── amp/
│                           └── settings.json  # Amp defaults (permissions, model)
```

This mirrors the ADK example structure: self-contained, copyable into any grove's `.scion/` directory.

### Harness Config (`config.yaml`)

The config uses `provisioner.type: container-script` from the start — there is no built-in fallback to migrate from.

```yaml
harness: amp
image: scion-amp:latest
user: scion

provisioner:
  type: container-script
  interface_version: 1
  command: ["python3", "/home/scion/.scion/harness/provision.py"]
  timeout: 30s
  lifecycle_events:
    - pre-start
  required_image_tools:
    - python3

config_dir: .config/amp
skills_dir: .config/amp/skills
interrupt_key: Escape
instructions_file: AGENT.md
system_prompt_file: AGENT.md
system_prompt_mode: prepend_to_instructions

command:
  base: ["amp"]
  resume_args: ["threads", "continue"]
  task_flag: "-x"
  task_position: after_base_args

env_template:
  SCION_AGENT_NAME: "{{ .AgentName }}"

capabilities:
  limits:
    max_turns: { support: "no", reason: "No hook dialect for turn events" }
    max_model_calls: { support: "no", reason: "No hook dialect for model events" }
    max_duration: { support: "yes" }
  telemetry:
    enabled: { support: "no", reason: "Amp has no native OTEL integration" }
    native_emitter: { support: "no" }
  prompts:
    system_prompt: { support: "partial", reason: "Downgraded into AGENT.md preamble" }
    agent_instructions: { support: "yes" }
  auth:
    api_key: { support: "yes" }
    auth_file: { support: "no", reason: "Uses OS keychain or env vars" }
    oauth_token: { support: "no" }
    vertex_ai: { support: "no" }

auth:
  default_type: api-key
  types:
    api-key:
      required_env:
        - any_of: ["AMP_API_KEY", "ANTHROPIC_API_KEY"]
  autodetect:
    env:
      AMP_API_KEY: api-key
      ANTHROPIC_API_KEY: api-key
```

Key design choices:

- **`system_prompt_mode: prepend_to_instructions`** — Amp reads agent instructions from `AGENT.md` but does not have a dedicated system prompt injection mechanism. The provisioning script prepends system prompt content to the instructions file, matching the pattern used by OpenCode.
- **`task_position: after_base_args`** — Amp expects `amp -x "task"`, with the task flag after base arguments.
- **`interrupt_key: Escape`** — Amp uses Escape to interrupt generation, similar to Claude Code.
- **Auth types** — API key via `AMP_API_KEY` or `ANTHROPIC_API_KEY` env var. (Amp does not support plaintext credential files directly). The Sourcegraph access token is delivered through `AMP_API_KEY`.

### Provisioning Script (`provision.py`)

The script follows the established contract from OpenCode/Codex provisioners. It runs inside the container during the `pre-start` lifecycle hook.

**Responsibilities:**

1. **Auth resolution** — Read `inputs/auth-candidates.json`, apply precedence (`AMP_API_KEY` > `ANTHROPIC_API_KEY`), write `outputs/resolved-auth.json`.
2. **Auth environment projection** — When using `api-key` auth, read the secret value from the staged `secrets/<NAME>` file and project it into `outputs/env.json` as `AMP_API_KEY` so Amp can pick it up.
3. **Settings reconciliation** — Merge any scion-managed settings (e.g., model selection, permission overrides) into `~/.config/amp/settings.json` without clobbering user-provided keys from the home overlay.

**What it does NOT do:**

- Telemetry reconciliation — Amp has no native OTEL integration, so `inputs/telemetry.json` is acknowledged but not acted on.
- Hook dialect setup — No hook events to wire.

**Script outline:**

```python
#!/usr/bin/env python3
"""Amp container-side provisioner.

Stdlib-only. Runs inside the agent container via sciontool harness provision.
"""

import argparse, json, os, sys
from typing import Any


AMP_SETTINGS_FILE = "~/.config/amp/settings.json"
VALID_AUTH_TYPES = ("api-key",)

EXIT_OK, EXIT_ERROR, EXIT_UNSUPPORTED = 0, 1, 2

def _provision(manifest: dict[str, Any]) -> int:
    bundle = os.path.expanduser(manifest.get("harness_bundle_dir", "$HOME/.scion/harness"))
    inputs_dir = os.path.join(bundle, "inputs")

    # 1. Load auth candidates
    candidates = _load_json_safe(os.path.join(inputs_dir, "auth-candidates.json"))
    explicit = (candidates.get("explicit_type") or "").strip()
    env_keys = {k for k in (candidates.get("env_vars") or []) if isinstance(k, str)}
    file_paths = [e["container_path"] for e in (candidates.get("files") or [])
                  if isinstance(e, dict) and e.get("container_path")]

    # 2. Select auth method (AMP_API_KEY > ANTHROPIC_API_KEY)
    method, env_key = _select_auth(explicit, env_keys, file_paths)

    # 3. Resolve API key
    if method == "api-key":
        secret_files = candidates.get("env_secret_files") or {}
        secret_path = secret_files.get(env_key)
        if secret_path and os.path.isfile(secret_path):
            api_key = open(secret_path).read().strip()

    # 4. Write outputs
    _write_json(manifest["outputs"]["resolved_auth"],
                {"schema_version": 1, "harness": "amp", "method": method,
                 "env_var": env_key if method == "api-key" else None})
    _write_json(manifest["outputs"]["env"], {"AMP_API_KEY": api_key})

    print(f"amp provision: method={method}", file=sys.stderr)
    return EXIT_OK
```

The full script follows the same patterns as `opencode/embeds/provision.py`: argparse for `--manifest`, `_load_json` / `_write_json` helpers with atomic rename, explicit error handling with actionable messages, and exit code 2 for unsupported commands.

### Container Image (`Dockerfile`)

```dockerfile
FROM scion-base:latest

# Install Amp CLI
RUN npm install -g @sourcegraph/amp@latest \
    && amp --version

# Ensure the provisioning script can run
RUN python3 --version

USER scion
WORKDIR /workspace
```

The image builds on `scion-base` which already provides `sciontool`, `tmux`, `git`, `python3`, and `node`. Amp is installed via npm. No additional dependencies are needed for the provisioning script (stdlib-only).

### Template Definition (`scion-agent.yaml`)

```yaml
schema_version: "1"
description: "Amp (Sourcegraph) coding assistant"
agent_instructions: agents.md
default_harness_config: amp
```

### Agent Instructions (`agents.md`)

Portable, harness-agnostic instructions that teach the agent about scion lifecycle integration:

```markdown
# Scion Agent Instructions

## Status Reporting

You are running inside a scion-managed container. Use `sciontool` to report
your status:

- `sciontool status ask_user "<question>"` — before asking the user a question
- `sciontool status blocked "<reason>"` — when waiting on external input
- `sciontool status task_completed "<summary>"` — when your task is finished

## Workspace

Your workspace is mounted at `/workspace`. This is a git worktree — you have
your own branch and can commit freely without affecting other agents.

## Important

If you see the exact message "System: Please Continue." — ignore it. This is
an automated artifact, not a real user message.
```

### Home Directory Files

**`.config/amp/settings.json`** — Sensible defaults for non-interactive container operation:

```json
{
  "amp.dangerouslyAllowAll": true,
  "amp.terminal.theme": "plain"
}
```

**`.bashrc`** — Minimal shell setup:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Deployment Flow

### User Setup

```bash
# 1. Build the container image
docker build -t scion-amp examples/amp/

# 2. Copy template into grove
cp -r examples/amp/templates/amp .scion/templates/amp

# 3. Copy harness-config (grove-level or global)
cp -r examples/amp/templates/amp/harness-configs/amp .scion/harness-configs/amp
# OR: cp -r ... ~/.scion/harness-configs/amp

# 4. Start an agent
scion start my-researcher --template amp

# With a task:
scion start my-researcher --template amp --task "Review the auth module for security issues"

# With explicit auth:
AMP_API_KEY=sk-... scion start my-researcher --template amp
```

### What Happens at Runtime

1. **`scion start`** resolves the `amp` template → finds `default_harness_config: amp` → loads `harness-configs/amp/config.yaml`.
2. **Harness resolution** sees `provisioner.type: container-script` → instantiates `ContainerScriptHarness` (the generic Go implementation). No Amp-specific Go code exists.
3. **Provision phase** stages the harness bundle into `agent_home/.scion/harness/`:
   - Copies `config.yaml` and `provision.py` from the harness-config directory.
   - Stages `inputs/auth-candidates.json` with available credentials.
   - Stages `inputs/instructions.md` from the template's `agents.md`.
   - Generates the lifecycle hook wrapper at `.scion/hooks/pre-start.d/20-harness-provision`.
4. **Container start** — `sciontool init` runs the pre-start hooks:
   - Executes `python3 /home/scion/.scion/harness/provision.py --manifest /home/scion/.scion/harness/manifest.json`.
   - Script resolves auth, sets AMP_API_KEY in the env overlay, writes outputs.
   - `sciontool init` loads `outputs/env.json` overlay into the child environment.
   - Launches `amp -x "task..."`.
5. **Attach/resume** — Pre-start hooks re-execute (idempotent), then `amp threads continue`.

## Key Differences from ADK Example

The ADK example uses `harness: generic` with custom Python agent code. The Amp example demonstrates a different pattern:

| Aspect | ADK Example | Amp Example |
|---|---|---|
| Harness type | `generic` (no provisioner) | `container-script` (full provisioner) |
| CLI binary | Custom Python (`python -m adk_scion_agent`) | Third-party CLI (`amp`) |
| Auth handling | Manual env bridging in Python code | Provisioning script via scion contract |
| Status reporting | Custom ADK callbacks + `sciontool_status` tool | Amp's native behavior (no custom code) |
| Provisioning | None (generic harness) | `provision.py` for auth + settings |
| Go code required | None | None |
| System prompt | Not supported | Downgraded into `AGENT.md` preamble |

The Amp example is closer to the built-in harness experience (auth resolution, settings management, system prompt injection) while still requiring zero compiled code.

## Validation Criteria

The example is considered successful if:

1. **No Go changes** — `git diff` against `pkg/` shows zero modifications. The entire harness is defined by files in `examples/amp/`.
2. **Auth works** — `AMP_API_KEY`, `ANTHROPIC_API_KEY` resolves correctly through the standard scion auth flow.
3. **Task delivery** — `scion start agent --template amp --task "..."` delivers the task to Amp and it begins working.
4. **Resume** — `scion start agent` (without task) resumes the previous Amp conversation.
5. **Attach** — `scion attach agent` connects to the running Amp session via tmux.
6. **Instructions** — Agent instructions from `agents.md` appear in `AGENT.md` in the workspace.
7. **System prompt** — If a system prompt is configured (template or grove level), it is prepended to `AGENT.md`.
8. **Idempotent provision** — Attaching or resuming re-runs `provision.py` without corrupting state.

## Future Extensions

These are out of scope for the initial example but document where the integration could grow:

- **Hook dialect** — If Amp exposes lifecycle events (tool use, model calls), a `dialect.yaml` could enable turn counting and model call limits.
- **Telemetry** — If Amp adds OTEL support, the provisioning script could reconcile `inputs/telemetry.json` into Amp's config.
- **OAuth / SSO** — Sourcegraph enterprise auth could be added as a new auth type in `config.yaml` with corresponding logic in `provision.py`.
- **Hub distribution** — The harness-config could be published as a Hub artifact, allowing teams to deploy Amp agents from the web UI without manual file copying.

## Resolved Amp CLI Research

The following details were validated via web research and local testing of the `@sourcegraph/amp` CLI to confirm the harness design:

| # | Detail | Answer |
|---|---|---|
| 1 | Headless flag | `-x` or `--execute`. Also implicitly enabled if stdout is redirected. |
| 2 | Initial task delivery | `-x "<task>"` for headless, or pipe via stdin (`echo "task" \| amp`) for interactive. |
| 3 | Resume conversation | `amp threads continue` (or alias `amp t c`). There is no `--continue` flag on the root command. |
| 4 | System prompt flag | No explicit `--system-prompt` flag. Controlled via mode flags (e.g. `-m deep`) and workspace instructions files. |
| 5 | Primary auth env var | `AMP_API_KEY`. |
| 6 | Credentials file | No plaintext credentials file. Amp uses the OS keychain (via `@napi-rs/keyring`) or the `AMP_API_KEY` env var. Containerized agents will rely strictly on the env var. |
| 7 | Settings file path | `~/.config/amp/settings.json` (with logs in `~/.cache/amp/logs/cli.log`). |
| 8 | Interrupt key | `Escape` or `Ctrl-C` (standard TUI). |
| 9 | Agent instructions file | `AGENT.md` is canonically supported, as well as `CLAUDE.md`. |
| 10 | NPM package | `npm install -g @sourcegraph/amp`. |
| 11 | Hook/Telemetry support | Yes! Amp supports `--stream-json` which emits events in a "Claude Code-compatible stream JSON format", opening the door for hook dialect integration. |

## Relationship to Decoupled Harness Design

This example is the capstone of the decoupled harness implementation effort:

| Phase | Status | Description |
|---|---|---|
| Phase 0–3 | Complete | Infrastructure (schema, staging, lifecycle, auth preflight) |
| Phase 4 | Complete | OpenCode migration (proof of concept) |
| Phase 5 | Complete | Codex migration (TOML complexity) |
| Phase 6 | Pending | Claude/Gemini evaluation |
| **Phase 7** | **This doc** | **Community harness template — Amp as first example** |

The Amp example validates the claim that started the entire effort: *a new harness can be added without Go expertise and without forking the scion binary.* If a community contributor can follow this example to integrate their preferred coding agent, the decoupled harness architecture has achieved its goal.
