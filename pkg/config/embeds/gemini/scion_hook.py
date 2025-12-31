import json
import sys
import os
import tempfile
from datetime import datetime

HOME = os.path.expanduser("~")
SCION_JSON_PATH = os.path.join(HOME, "agent-info.json")
AGENT_LOG_PATH = os.path.join(HOME, "agent.log")

def log_event(state, message):
    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    with open(AGENT_LOG_PATH, "a") as f:
        f.write(f"{timestamp} [{state}] {message}\n")

def update_status(status):
    if not os.path.exists(SCION_JSON_PATH):
        return
    try:
        with open(SCION_JSON_PATH, "r") as f:
            data = json.load(f)
        
        # Handle different potential structures
        if "info" in data and isinstance(data["info"], dict):
            data["info"]["status"] = status
        elif "agent" in data and isinstance(data["agent"], dict):
            data["agent"]["status"] = status
        else:
            # Assume it's AgentInfo directly or we just set it at top level
            data["status"] = status
        
        # Atomic write
        fd, temp_path = tempfile.mkstemp(dir=os.path.dirname(SCION_JSON_PATH))
        with os.fdopen(fd, 'w') as f:
            json.dump(data, f, indent=2)
        os.replace(temp_path, SCION_JSON_PATH)
    except Exception as e:
        log_event("ERROR", f"Failed to update {os.path.basename(SCION_JSON_PATH)}: {e}")

def main():
    try:
        input_data = json.load(sys.stdin)
    except Exception:
        # Non-JSON input, skip
        return

    event = input_data.get("hook_event_name")
    
    state = "IDLE"
    log_msg = f"Event: {event}"

    if event == "SessionStart":
        state = "STARTING"
        log_msg = f"Session started (source: {input_data.get('source')})"
    elif event == "BeforeAgent":
        state = "THINKING"
        prompt = input_data.get("prompt", "")
        log_msg = f"User prompt: {prompt[:100]}..." if prompt else "Planning turn"
    elif event == "BeforeModel":
        state = "THINKING"
        log_msg = "LLM call started"
    elif event == "AfterModel":
        state = "IDLE"
        log_msg = "LLM call completed"
    elif event == "BeforeTool":
        tool_name = input_data.get("tool_name")
        state = f"EXECUTING ({tool_name})"
        log_msg = f"Running tool: {tool_name}"
    elif event == "AfterTool":
        state = "IDLE"
        tool_name = input_data.get("tool_name")
        log_msg = f"Tool {tool_name} completed"
    elif event == "Notification":
        state = "WAITING_FOR_INPUT"
        log_msg = f"Notification: {input_data.get('message')}"
    elif event == "AfterAgent":
        state = "IDLE"
        log_msg = "Agent turn completed"
    elif event == "SessionEnd":
        state = "EXITED"
        log_msg = f"Session ended (reason: {input_data.get('reason')})"

    update_status(state)
    log_event(state, log_msg)

if __name__ == "__main__":
    main()
