# Design: Tmux Support

## Goal
Improve interactivity and persistence by optionally running the Gemini CLI within a `tmux` session inside the agent container. This allows users to detach and re-attach to the same interactive session without interrupting the agent's work. This is particularly required for the Apple Container runtime which does not support a native attach.

## Configuration
Add `use_tmux` field to `scion-agent.json`.

```json
{
  "image": "gemini-cli-sandbox",
  "use_tmux": true
}
```

This will be mapped to the `ScionConfig` struct in `pkg/config/templates.go`.

## Image Selection
If `use_tmux` is enabled:
1. The system should check if a version of the configured image with a `:tmux` tag exists.
2. For example, if the image is `gemini-cli-sandbox`, it should check for `gemini-cli-sandbox:tmux`.
3. If it exists, use that image instead of the base image.
4. If it does not, an error early on should be printed to the user and then the scion CLI should exit.

## Runtime Implementation

### `RunConfig` Changes
Add `UseTmux bool` to `runtime.RunConfig`.

### `Run` command
When `UseTmux` is true, the container should be started with `tmux`.

For Docker/Apple Container:
- The command passed to the container should be: `tmux new-session -s "scion" 'gemini'`
- Note: The session name could be "scion" or the agent name. Using a fixed name like "scion" might be easier for the `attach` command if we only ever have one session per container.

### `Attach` command
When `UseTmux` is true, the `attach` implementation should use `exec` to attach to the tmux session instead of using the native container attach. A native container attach does not exist for Apple Container tool.

Command: `container exec -it <name> tmux attach -t scion`

## Implementation Tasks

### 1. Config Update
- Update `ScionConfig` in `pkg/config/templates.go` to include `UseTmux`.
- Update `DefaultScionJSON` in `pkg/config/init.go` (optional, maybe keep it false by default).

### 2. Runtime Interface & RunConfig
- Update `RunConfig` in `pkg/runtime/runtime.go`.
- Add a method to `Runtime` or a helper to check for image tag existence.

### 3. Docker Runtime
- Implement `:tmux` tag check.
- Update `Run` to support `tmux` command.
- Update `Attach` to use `tmux attach` if the container is using tmux (we might need to store this state or detect it).

### 4. Apple Container Runtime
- Implement `:tmux` tag check.
- Update `Run` to support `tmux` command.
- Update `Attach` to use `tmux attach`.
- Attach is not supported without tmux

### 5. CLI Start Command
- Load `use_tmux` from templates and pass it to `RunConfig`.

### 6. CLI Attach Command
- Implement a new `attach` command that finds the agent container and calls `rt.Attach`.
- The `rt.Attach` implementation will handle whether to use `tmux attach` or native attach based on container state (e.g. by checking if tmux is running or using a label).
