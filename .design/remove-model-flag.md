# Remove Model Flag Plan

## Objective

Remove the explicit `--model` CLI flag and the top-level `model` field from configuration structures. Model selection will be handled uniformly via environment variables or provider-specific configuration files, consistent with the "Relational" settings refactor.

## Rationale

The `model` parameter has been a "special citizen" in the configuration, duplicated across CLI flags, top-level struct fields, and environment variables. This creates ambiguity (which one takes precedence?) and tight coupling (the CLI knows about "models" which are a harness-specific concept).

By removing the explicit flag:
1.  **Simplification**: The CLI interface becomes cleaner (`scion start my-agent`).
2.  **Decoupling**: The core logic doesn't need to know if a harness uses a "model" or something else.
3.  **Consistency**: Configuration moves to `scion-agent.json` (under `env`) or provider-specific settings (e.g., `.gemini/settings.json`), which is where other harness-specific settings reside.

## Impact Analysis

### 1. Command Line Interface (`cmd/`)
- **Files**: `cmd/start.go`, `cmd/resume.go`, `cmd/common.go`.
- **Change**: 
    - Remove the global `model` string variable (if shared) or local variables.
    - Remove `cmd.Flags().StringVarP(&model, "model", ...)` calls.
    - Remove `Model` field initialization in `api.StartOptions` within `RunAgent`.

### 2. API Structs (`pkg/api/`)
- **Files**: `pkg/api/types.go` (Already updated, but verifying usages).
- **Change**: Ensure `StartOptions` and `ScionConfig` definitely do not have `Model`. (Current state: `StartOptions` lost it, causing build breaks).

### 3. Documentation (`docs/`)
- **Files**: `docs/scion-config-reference.md`.
- **Change**: Remove the section on the `model` configuration option. Add a note or example on how to set the model via `env` variables (e.g., `GEMINI_MODEL`).

### 4. Templates & Defaults
- **Files**: `pkg/config/embeds/` (templates).
- **Change**: Verify that default templates utilize `env` for specifying models if a default is required.
    - Example: `pkg/config/embeds/claude/settings.json` already uses `ANTHROPIC_MODEL`.
    - Check `pkg/config/embeds/gemini/scion-agent.json` or similar to ensure a default is set or documented.

## Migration Path

Users who relied on `scion start --model "gemini-1.5-pro"` will need to:
1.  Set it in `scion-agent.json`:
    ```json
    {
      "env": {
        "GEMINI_MODEL": "gemini-1.5-pro"
      }
    }
    ```
2.  Or pass it as an environment variable override (if/when CLI supports generic env overrides, though `scion start` currently takes task args).

## Implementation Steps

1.  **Fix Build**: Remove `Model: model` from `cmd/common.go`.
2.  **Clean CLI**: Remove flag definitions in `start.go` and `resume.go`.
3.  **Verify Templates**: Check `gemini` and `claude` templates to ensure they have sensible defaults via `env` or internal settings files.
4.  **Update Docs**: edit `docs/scion-config-reference.md`.
5.  **Verify**: Run `go build ./...` and ensure success.
