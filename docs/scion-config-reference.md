# scion-agent.json Configuration Reference

The `scion-agent.json` file is used within templates and agent directories to configure how a Scion agent is provisioned and executed.

## Fields

### `template` (string)
The name of the template this configuration belongs to.
- **Example**: `"template": "gemini"`

### `harness` (string)

The agent harness to use. Currently supported values are `gemini` and `claude`.

- **Example**: `"harness": "claude"`

### `config_dir` (string)

The directory within the agent's home that contains harness-specific configuration files. Defaults to `.gemini` or `.claude` depending on the harness.

- **Example**: `"config_dir": ".gemini"`

### `unix_username` (string)
The username used for the primary user inside the container.
- **Default**: `node`
- **Example**: `"unix_username": "developer"`

### `image` (string)
The container image to use for the agent.
- **Default**: `gemini-cli-sandbox`
- **Example**: `"image": "my-custom-gemini-agent:latest"`

### `env` (object)
A map of environment variables to set in the agent container.
- **Example**: `{"MY_VAR": "my-value"}`

### `volumes` (array)
A list of volume mounts to add to the agent container.
- **Fields**: `source`, `target`, `read_only` (bool)
- **Example**: `[{"source": "/tmp/data", "target": "/data", "read_only": true}]`

### `detached` (boolean)
Whether the agent should run in detached mode by default.
- **Default**: `true`
- **Example**: `"detached": false`

### `use_tmux` (boolean)
If set to `true`, the agent's main process will be wrapped in a `tmux` session. This enables persistent interactive sessions that can be detached and re-attached using the `scion attach` command.
- **Default**: `false`
- **Details**: When enabled, the image must have `tmux` installed. If `tmux` is not found in the image, the container will fail to start.
- **Example**: `"use_tmux": true`

### `model` (string)
The model ID to use for the agent.
- **Default**: `"flash"` for Gemini
- **Details**: This value is propagated to the agent container as an environment variable (e.g., `GEMINI_MODEL` for Gemini CLI).
- **Example**: `"model": "pro"`

### `agent` (object)
*Internal usage*: This object is typically populated by the Scion CLI during provisioning to track instance-specific state.
- **Fields**: `grove`, `name`, `status`.

## Inheritance
`scion` uses a template inheritance system. Configuration fields are merged from the specified template type and finally any overrides in the agent's own directory. The last value defined for a field takes precedence. Unlike earlier versions, there is no longer a single global `default` template that all templates inherit from; instead, agents start from a specific harness default like `gemini` or `claude`.