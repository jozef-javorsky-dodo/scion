# Scion

Scion is a container-based orchestration tool for managing concurrent LLM-based code agents. It enables parallel execution of specialized sub-agents with isolated identities, credentials, and workspaces.

## Installation

You can install Scion directly from the source using Go:

```bash
go install github.com/ptone/scion-agent@latest
```

Ensure that your `$GOPATH/bin` is in your system `$PATH`.

## Concepts

- **Agent**: An isolated container running an LLM-driven task. Each agent has its own home directory, workspace, and credentials.
- **Grove**: A "grove" is a project workspace where agents live. It corresponds to a `.scion` directory which contains agent configurations and templates. When initialized, it includes default templates for supported harnesses (`gemini`, `claude`).
- **Template**: A blueprint for an agent, defining its base configuration, system prompt, and tools.

## Quick Start

### 1. Initialize a Grove

Navigate to your project root and initialize a new Scion grove. This creates the `.scion` directory structure and seeds default templates for supported harnesses (e.g., Gemini and Claude).

```bash
cd my-project
scion grove init
```

### 2. Provision and Start Agents

You can launch an agent immediately using `start` (or its alias `run`), or provision it first using `create` to customize it before execution.

#### Option A: Quick Start (Immediate Execution)

Launch a new agent to perform a specific task. By default, this runs in the background using the `gemini` template.

```bash
# Start a gemini agent named "coder" (uses gemini by default)
scion start coder "Refactor the authentication middleware in pkg/auth"

# Start a Claude-based agent
scion run auditor "Audit the user input validation" --type claude

# Start and immediately attach to the session
scion start debug "Help me debug this error" --attach
```

#### Option B: Create-Then-Start (Customization Workflow)

The `create` command allows you to provision an agent's directory structure without launching a container. This is useful for customizing an agent's environment or testing its behavior before it starts its task.

1. **Create the agent:**
   ```bash
   scion create my-agent --type research-specialist
   ```

2. **Customize the agent:**
   Navigate to the agent's home directory to edit its configuration, system prompt, or provided tools:
   ```bash
   cd .scion/agents/my-agent/home
   # Edit scion-agent.json, .gemini/system_prompt.md, etc.
   ```

3. **Start the agent:**
   When you are ready, use `start` (or `run`) with the agent's name. Scion will detect the existing directory and use your customizations.
   ```bash
   scion start my-agent "Analyze the latest trends in quantum computing"
   ```

### 3. List Running Agents

View all active agents in the current grove.

```bash
scion list
```

To see agents across all groves:

```bash
scion list --all
```

### 4. Interact with an Agent

You can attach to a running agent's interactive session (TTY).

```bash
scion attach coder
```

Use `Ctrl+P, Ctrl+Q` to detach without stopping the container (if using Docker/default runtime behavior), or simply exit the shell to stop the agent.

### 5. View Logs

Check the logs of a background agent.

```bash
scion logs coder
```

### 6. Stop and Cleanup

Stop a running agent:

```bash
scion stop coder
```

Delete an agent (removes the container, agent directory, and git worktree):

```bash
scion delete coder
```

### 7. Manage Templates

Templates serve as blueprints for new agents. You can manage them using the `templates` subcommand.

- **List templates**:
  ```bash
  scion templates list
  ```
- **Create a new template**:
  ```bash
  scion templates create my-special-tpl
  ```
- **Delete a template**:
  ```bash
  scion templates delete my-special-tpl
  ```
- **Update default templates**:
  ```bash
  scion templates update-default
  ```
  *(Note: This restores or syncs the default `gemini` and `claude` templates with the latest defaults from the Scion binary.)*

Use the `--global` flag with these commands to target the global template store in `~/.scion/templates`.

## Configuration

Scion uses a `settings.json` file located in `~/.gemini/settings.json` for global configuration, such as API keys and auth preferences.

## License

This project is licensed under the Apache License, Version 2.0. See the [LICENSE](LICENSE) file for details.