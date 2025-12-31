# Scion LLM-Agnostic Design

This document outlines the strategy for making `scion` more LLM-agnostic, allowing it to support multiple types of agents (e.g., Gemini, Claude) beyond its initial Gemini CLI focus.

## Current Gemini-Specific Coupling

- **Configuration Path**: Hardcoded use of `.gemini` directory in agent home.
- **Settings Format**: Hardcoded `settings.json` structure derived from Gemini CLI.
- **Environment Variables**: Use of `GEMINI_API_KEY` and `GEMINI_SANDBOX`.
- **Default Images**: Defaulting to `gemini-cli-sandbox`.
- **Status Files**: Hardcoded `.gemini-status.json`.
- **Aliases**: Default `.bashrc` includes `alias g="gemini"`.

## Overall proposed approach

Use a similar approach as being used for the runtime variations, but refer to the choice as the harness. Initial choices will be gemini-cli and claude-code.

scion-agent.json will include a top level 'harness' key.

## Proposed Prioritized Areas

### 1. Generalize Container Environment (High Priority)

Instead of hardcoding environment variables like `GEMINI_API_KEY`, `scion` should support a more flexible environment propagation system.

- **Action**: Move tool-specific environment variable names into `scion-agent.json` or template metadata.

### 2. Abstract Config and Status Paths (High Priority)

Different agents expect configuration and write status to different locations.

- **Action**: In `scion-agent.json`, allow specifying the path for the harness's main configuration directory (relative to the Agent Home directory).
- **Action**: Use the `status` field in `scion-agent.json` (within the `agent` object) as the primary way to track agent state, removing the need for a separate `.gemini-status.json` file.

### 3. Template Refactoring (Done)

The current `default` template has been replaced by harness-specific defaults.

- **Action**: Renamed `default` to `gemini-default`.
- **Action**: Created a `claude-default` template structure.
- **Action**: Updated `InitProject` and `InitGlobal` to seed multiple harness-specific templates.
- **Action**: Template create command takes a harness type.



### 4. Image and Command Abstraction (Medium Priority)

- **Action**: The `scion start` command currently assumes the task is passed as arguments that the container entrypoint knows how to handle. This should be more explicit.
- **Action**: Allow `scion-agent.json` to define the `entrypoint` or `cmd` wrapper if the image doesn't handle it.
- **Action**: A number of the default args, such as --yolo are harness specific. When they are always used by a harness, they should be hard coded into that harnesss implemenation. But scion-agent.json should support adding and overriding these for per-template purposes.
- **Action**: Will need a hook processor for claude-code.

### 5. Authentication Discovery (Low Priority)

- **Action**: Generalize `pkg/config/auth.go` to support multiple authentication types (e.g., `ANTHROPIC_API_KEY`).
- **Action**: Allow templates to define their own auth discovery logic or required environment variables. Initially, this can be handled via environment variables or mounting existing authentication directories (like `~/.config/gcloud`).

## Implementation Phases

### Phase 1: Core Decoupling
- Update `scion-agent.json` schema to include `env`, `harness`, and `config_dir`.
- Modify `pkg/config` and `pkg/runtime` to respect these new settings.
- Factor harness-specific logic into a new `pkg/harness` package.
- Rename generic "Gemini" terms in internal code (e.g., `GetGeminiSettings` -> `GetAgentSettings`).

### Phase 2: Multi-Harness Templates (Done)
- Update `scion grove init` to provide default per harness.


### Phase 3: Enhanced Human-in-the-Loop
- Generalize the status polling to support different status formats if necessary (though keeping a common `scion-status.json` format is preferred).

## Implementation Summary (WIP)

The core decoupling of agent logic has been implemented with the following changes:

1.  **Centralized Types (`pkg/api`)**: Core configuration structures (`ScionConfig`, `AgentConfig`, `AuthConfig`) were moved to a new `api` package to resolve circular dependencies between configuration management and agent harnesses.
2.  **Harness Abstraction (`pkg/harness`)**: Introduced a `Harness` interface that encapsulates harness-specific logic for environment variables, command construction, authentication discovery, and file/volume propagation.
    *   Implemented `GeminiCLI` harness with all existing Gemini-specific logic.
    *   Added a placeholder `ClaudeCode` harness to verify the multi-harness architecture.
3.  **Runtime Generalization**: The `pkg/runtime` package now uses the `Harness` interface to configure containers, removing hardcoded references to Gemini-specific environment variables and command-line arguments.
4.  **Template Flexibility**: The template seeding system was updated to support harness-specific configuration directories (e.g., `.claude-code` vs `.gemini`) and automatically includes the `harness` in the generated `scion-agent.json`.
5.  **CLI Enhancements**:
    *   `scion grove init` now supports a `--harness` flag to seed the default template for a specific agent type.
    *   `scion templates create` now supports a `--harness` flag to initialize templates with the correct directory structure.
    *   `scion start` automatically detects and employs the correct harness based on the agent's configuration.
6.  **Refactoring**: Renamed internal structures and files (e.g., `GeminiSettings` -> `AgentSettings`) to reflect their generalized purpose.
7.  **Generic Volume Mounts**: Added support for declarative volume mounts in `scion-agent.json` via a new `volumes` field, which are propagated to the container runtime alongside harness-specific mounts.
8.  **Unified Template Management**: Reorganized embedded template assets into harness-specific directories and refactored `SeedTemplateDir` to use `embed.FS` for dynamic, harness-aware template initialization.
9.  **Robust Configuration Merging**: Enhanced `MergeScionConfig` to perform deep copies of nested maps and slices (`Env`, `Volumes`, `Agent`), preventing accidental mutation of base templates.
10. **Harness Path Consistency**: Updated harnesses to dogfood their own `DefaultConfigDir()` for all internal path resolution (settings, credentials, system prompts), ensuring consistency across discovery and propagation.

## Refinement investigations

Q: Does the auth and AuthConfig need to be so strongly typed, or will we be able to get away with just what is in the agentHome mount and env variables?
**A**: While strong typing is safe, a `map[string]string` for "Secrets" or "Credentials" within `AuthConfig` would be more extensible. The `Harness` should continue to handle the *discovery* of these secrets (whether from env or host files) but could return them in a more generic structure.
Decision: postpone

Q: How useful would it be to have a generic approach in the template for specifying volume mounts in a general way, how involved would that be? Could that move harness specific logic from the harness package to the templates?
**A**: This is highly recommended. Adding a `volumes` array to `scion-agent.json` (e.g., `{"source": "...", "target": "...", "read_only": true}`) would make templates much more powerful. The `Harness.GetVolumes` method could then be simplified to return these generic structures, and users could override or add mounts without changing Go code.
Decision: **Done**

Q: I see embedded template files for gemini in the config package, but not for claude-code, do we have a common and unified approach for managing these templated files?
**A**: We should reorganize `pkg/config/embeds/` into harness-specific subdirectories (e.g., `embeds/gemini-cli/`, `embeds/claude-code/`). `SeedTemplateDir` should be updated to dispatch to the correct source directory based on the `harness`.
Decision: **Done**