# Design: Harness Interface Cleanup

## Objective
Decouple harness-specific logic from the core `agent` and `config` packages by extending the `Harness` interface to cover the full agent lifecycle, particularly the provisioning phase. Consolidate configuration and behavior decisions into the template structure (`scion-agent.json`) so that harness implementations are minimized and the system is more data-driven.

## Proposed Changes

### 1. Relocate Harness Interface
To avoid circular dependencies (e.g., `pkg/config` needing harness details that depend on config), move the `Harness` interface definition from `pkg/harness/harness.go` to **`pkg/api/harness.go`**.

### 2. Extend Harness Interface
Add the following methods to the `Harness` interface in `pkg/api/harness.go`:

```go
type Harness interface {
    // Existing methods...
    Name() string
    DiscoverAuth(agentHome string) AuthConfig
    GetEnv(agentName, unixUsername, model string, auth AuthConfig) map[string]string
    GetCommand(task string, resume bool) []string
    PropagateFiles(homeDir, unixUsername string, auth AuthConfig) error
    GetVolumes(unixUsername string, auth AuthConfig) []VolumeMount
    DefaultConfigDir() string

    // New methods
    
    // Provision performs harness-specific setup during agent creation.
    // This is called after templates are copied and scion-agent.json is written.
    Provision(ctx context.Context, agentName, agentHome, agentWorkspace string) error

    // GetEmbedDir returns the name of the directory in pkg/config/embeds/ 
    // that contains template files for this harness (e.g., "claude", "gemini").
    GetEmbedDir() string
}
```

### 3. Refactor `pkg/agent/provision.go`
*   **Remove `UpdateClaudeJSON`**: Move the logic of this function into `pkg/harness/claude_code.go` as the `Provision` method.
*   **Update `ProvisionAgent`**:
    *   Remove the direct call to `UpdateClaudeJSON`.
    *   Instantiate the harness: `h := harness.New(finalScionCfg.Harness)`.
    *   Call `h.Provision(ctx, agentName, agentHome, agentWorkspace)` at the end of the function (before returning).

### 4. Refactor `pkg/config/init.go` and `SeedTemplateDir`
*   **Update `SeedTemplateDir` Signature**:
    *   Change signature to: `SeedTemplateDir(templateDir, templateName, harness, embedDir, configDirName string, force bool) error`.
    *   Remove the hardcoded `if harness == "claude"` logic for determining `embedDir` and `configDirName`. Use the passed arguments instead.
*   **Update Callers**:
    *   `InitProject` and `InitGlobal`: Update calls to pass specific strings for default templates (e.g., for Claude: `embedDir="claude"`, `configDirName=".claude"`).
    *   `CreateTemplate`: Requires updates to fetch these values from the harness (see below).

### 5. Refactor `pkg/config/templates.go`
*   **Update `CreateTemplate`**:
    *   This function needs to know the `embedDir` and `configDirName` for the requested harness.
    *   Since `pkg/config` cannot import `pkg/harness`, we must pass this information in from the CLI layer (`cmd/create.go`), OR `CreateTemplate` accepts a `HarnessMetadata` struct (defined in `pkg/api`).
    *   **Decision**: Update `CreateTemplate` to accept `embedDir` and `configDirName`.

### 6. Refactor `cmd/create.go` (CLI Layer)
*   When creating a template, use `harness.New(harness)` to get the harness instance.
*   Extract `h.GetEmbedDir()` and `h.DefaultConfigDir()`.
*   Pass these values to `config.CreateTemplate`.

### 7. Update Harness Implementations
*   **`GeminiCLI` (`pkg/harness/gemini_cli.go`)**:
    *   `Provision`: Return `nil` (no-op).
    *   `GetEmbedDir`: Return `"gemini"`.
*   **`ClaudeCode` (`pkg/harness/claude_code.go`)**:
    *   `Provision`: Implement the logic from `UpdateClaudeJSON` (updating `.claude.json` with workspace paths).
    *   `GetEmbedDir`: Return `"claude"`.

### 8. Update `pkg/api/types.go`
*   Add `CommandArgs []string `json:"command_args,omitempty"` to `ScionConfig`.

### 9. Simplify `scion-agent.json` Logic (Data-Driven)

Standardize how the harness consumes `scion-agent.json` to reduce code in `GetEnv`, `GetVolumes`, etc.

*   **Environment Variables**:
    *   If `env` in `scion-agent.json` has a key with an empty value (e.g., `"MY_KEY": ""`), it implies "inherit from host environment".
    *   If value is set, use it.
    *   Harness `GetEnv` implementations should merge this logic with their specific requirements.

*   **Volume Mounts**:
    *   Support `~` expansion in `Source` (host user home) and `Target` (container user home).
    *   Example: `{ "source": "~/.config/gcloud", "target": "~/.config/gcloud" }`.
    *   Harness `GetVolumes` should process the `ScionConfig.Volumes` list, expand paths, and append harness-specific volumes (like credentials).

*   **Command Args**:
    *   Harness `GetCommand` should accept the base args from `ScionConfig.CommandArgs`.
    *   It can prepend/append harness-specific flags (e.g., `--yolo`).

## Implementation Plan

1.  **Move Interface**: Create `pkg/api/harness.go` with the updated interface.
2.  **Update Structs**: Add `CommandArgs` to `ScionConfig` in `pkg/api/types.go`.
3.  **Update Harnesses**: Implement `Provision` and `GetEmbedDir` in `pkg/harness/*.go`. Move `UpdateClaudeJSON` logic.
4.  **Update Config Logic**: Modify `SeedTemplateDir` in `pkg/config/init.go` to accept `embedDir`/`configDir` args.
5.  **Update CLI**: Update `cmd/create.go` to resolve harness details and pass them to config.
6.  **Refactor ProvisionAgent**: Switch to `h.Provision`.
7.  **Clean Up**: Remove `UpdateClaudeJSON` from `pkg/agent`.