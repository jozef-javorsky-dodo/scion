# Design: Scion Agent Hook Processor

## Goal
Implement a default hook processor for Scion agents to provide high-level status visibility and detailed logging. The processor will be written in Python to ensure it is cross-platform (within the container) and easily customizable.

## Functionality
1. **Status Tracking**: Updates the `agent.status` field in the agent's `scion-agent.json` file.
2. **Detailed Logging**: Creates and maintains an `agent.log` file in the agent's home directory (`/home/gemini`), capturing hook events, tool inputs/outputs, and model responses.

## Architecture

### 1. Hook Configuration
The default `settings.json` in the agent template will be updated to include hooks for various lifecycle events. All events will point to the same Python processor script.

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py",
            "description": "Update agent status and log session start"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ],
    "BeforeAgent": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ],
    "AfterAgent": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ],
    "BeforeTool": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ],
    "AfterTool": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "ToolPermission",
        "hooks": [
          {
            "name": "scion-status",
            "type": "command",
            "command": "python3 /home/gemini/scion_hook.py"
          }
        ]
      }
    ]
  }
}
```

### 2. State Mapping
The hook processor will map Gemini CLI hook events to high-level Scion agent states:

| Hook Event | Agent State | Description |
|------------|-------------|-------------|
| `SessionStart` | `STARTING` | Agent is initializing. |
| `BeforeAgent` | `THINKING` | Agent received a prompt and is planning. |
| `BeforeTool` | `EXECUTING` | Agent is running a tool (e.g., `EXECUTING (read_file)`). |
| `AfterTool` | `THINKING` | Tool finished; agent is processing results. |
| `Notification` | `WAITING` | Agent is blocked (e.g., waiting for tool permission). |
| `AfterAgent` | `IDLE` | Agent turn is complete, waiting for user input. |
| `SessionEnd` | `EXITED` | Session terminated. |

### 3. Filesystem Impact

#### `scion-agent.json` Update
The processor will update the `agent` section of `scion-agent.json`. To prevent file corruption during concurrent access, the processor must use an **atomic write strategy** (write to a temporary file, then rename/move to `scion-agent.json`).

```json
{
  "image": "...",
  "agent": {
    "grove": "my-grove",
    "name": "my-agent",
    "status": "EXECUTING (run_shell_command)"
  }
}
```

#### `agent.log`
A human-readable log file will be maintained:

```text
2025-12-23 10:00:00 [STARTING] Session started (startup)
2025-12-23 10:00:05 [THINKING] User: "Fix the bug in main.go"
2025-12-23 10:00:10 [EXECUTING] Tool: read_file {"file_path": "main.go"}
2025-12-23 10:00:12 [THINKING] Tool read_file completed.
2025-12-23 10:00:15 [IDLE] Response sent to user.
```

### 4. Hook Processor Implementation (Python)
The script will follow these steps:
1. Read hook input from `stdin`.
2. Determine the new state based on `hook_event_name`.
3. Load `scion-agent.json`, update the status, and save.
4. Append a timestamped entry to `agent.log` with relevant event details (tool name, prompt snippet, etc.).
5. Exit with 0.

### 5. Grove Path Tracking
To enable `scion list --all` to display high-level status for agents across different groves, the `scion` CLI will add a `scion.grove_path` label to the container at startup. This allows the CLI to locate the agent's host-side `scion-agent.json` file regardless of the current working directory.

## Implementation Tasks

### 1. Update Go Code
- Update `AgentConfig` struct in `pkg/config/templates.go` to include `Status` field.
- Update `InitProject` and `InitGlobal` in `pkg/config/init.go` to include the hook processor in the default template and configure `settings.json`.
- Update `scion start` logic to include the `scion.grove_path` label.

### 2. Create Python Script
- Write `scion_hook.py` and include it in the `default` template.

### 3. Update `scion list`
- Enhance the `list` command to:
    1. Read the `scion.grove_path` label from the agent info.
    2. Locate and read the host-side `scion-agent.json`.
    3. Display the high-level `status` in the output table.

## Observability Enhancement
With this design, `scion list` can show real-time progress of agents, and users can tail `agent.log` on the host side to see exactly what an agent is doing without attaching to it.
