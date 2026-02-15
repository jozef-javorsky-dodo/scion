# Web TTY Manual Testing Guide

## Prerequisites

You need all three components running and connected:

1. **Hub** — the central API server (`:9810`)
2. **Runtime Broker** — registered with Hub, with at least one running agent
3. **Web Frontend** — the Koa server (`:8080`)

The broker must have an active control channel to the Hub (the Hub logs `Control channel connected` when this happens).

## Build & Start the Web Frontend

```bash
cd web
npm install
npm run build
npm start
```

Or for development with auto-reload:

```bash
npm run dev
```

Ensure `HUB_API_URL` points to your Hub (defaults to `http://localhost:9810`).

## Auth Setup

The WebSocket proxy resolves auth in this order:

1. **Dev token** — set `SCION_DEV_TOKEN` env var, or ensure `~/.scion/dev-token` exists (Hub generates this on startup). This is the easiest path for local testing.
2. **Session cookie** — log in via OAuth at `/auth/login`, which stores a Hub access token in the session.

Verify auth is working by checking that `/api/agents` returns data (not 401).

## Test Cases

### 1. SSR Rendering

**What**: The terminal route serves HTML without the app shell (full-screen layout).

```bash
curl -s http://localhost:8080/agents/test-agent/terminal | grep scion-page-terminal
```

**Expected**: Response contains `<scion-page-terminal>` tag, no `<scion-app>` wrapper.

### 2. Agent Detail → Terminal Link

Navigate to `/agents/{agentId}` in the browser. The "Open Terminal" quick action card should link to `/agents/{agentId}/terminal`.

### 3. Terminal — Agent Not Running

Navigate to `/agents/{agentId}/terminal` for a stopped agent.

**Expected**: Dark page with toolbar showing agent name, error message: "Agent is stopped. Terminal is only available when the agent is running.", and a Retry button.

### 4. Terminal — Successful Connection

Navigate to `/agents/{agentId}/terminal` for a running agent.

**Expected**:
- Loading spinner appears briefly ("Connecting to agent...")
- xterm.js terminal fills the viewport below the toolbar
- Toolbar shows agent name, green dot, "Connected"
- tmux session content appears in the terminal (you should see the agent's shell/prompt)

**Check the Koa server logs** for:
```
[WS-PTY] Session established for agent {agentId}
```

### 5. Keyboard Input

Click into the terminal and type commands (e.g., `ls`, `echo hello`).

**Expected**: Characters appear in the terminal, commands execute, output displays. This exercises the `data` message type (base64-encoded keystrokes sent over WebSocket).

### 6. Terminal Resize

Resize the browser window or drag a split pane.

**Expected**: Terminal content reflows to match the new dimensions. The component sends a `resize` message with updated `cols`/`rows` to the Hub, which forwards it to the broker's PTY.

### 7. Disconnect & Reconnect

Kill the Hub process (or the broker) while the terminal is open.

**Expected**:
- Status dot turns red, text changes to "Disconnected"
- A "Reconnect" button appears in the toolbar
- An error banner may appear below the toolbar with the close code
- Clicking "Reconnect" tears down the terminal and re-initializes (fetches agent info, opens new WebSocket)

### 8. Navigate Away

Click the "Back to Agent" link in the toolbar.

**Expected**: Navigates to `/agents/{agentId}`. The WebSocket should close cleanly (check browser DevTools Network tab — the WS connection should show close code 1000 or 1001).

### 9. Invalid Agent ID

Navigate to `/agents/nonexistent-id/terminal`.

**Expected**: Error state with "Failed to Load Agent" or the API error message, plus a Retry button.

### 10. No Auth Available

Start the web frontend without a dev token and without logging in via OAuth. Navigate to a terminal URL.

**Expected**: The page may redirect to login (handled by the auth middleware for the initial page load), or if you somehow reach the page, the WebSocket connection will fail with a 401 and show a connection error.

## WebSocket Proxy Verification

You can test the WebSocket proxy directly with `wscat` (install via `npm i -g wscat`):

```bash
# With dev token in a cookie (or if dev-auth auto-resolves):
wscat -c "ws://localhost:8080/api/agents/{agentId}/pty?cols=80&rows=24"
```

If auth is via session cookie, pass it explicitly:
```bash
wscat -c "ws://localhost:8080/api/agents/{agentId}/pty?cols=80&rows=24" \
  -H "Cookie: scion_sess=...; scion_sess.sig=..."
```

**Expected**: Connection establishes, you receive `{"type":"data","data":"..."}` messages containing the tmux session output.

Send input:
```json
{"type":"data","data":"bHMK"}
```
(`bHMK` is base64 for `ls\n`)

## Browser DevTools Checklist

Open DevTools while using the terminal:

- **Network → WS**: You should see a WebSocket connection to `/api/agents/{id}/pty?cols=...&rows=...`. Click it to inspect frames — you'll see JSON messages with `type: "data"` (I/O) and `type: "resize"` (on window resize).
- **Console**: No errors. Look for `[Terminal] Could not load xterm CSS` — if present, the terminal may render without proper styling.
- **Elements**: The `<scion-page-terminal>` shadow root should contain a `.toolbar` div and a `.terminal-container` div with xterm.js canvas elements inside it.

## Troubleshooting

| Symptom | Likely Cause |
|---------|-------------|
| 401 on WebSocket | No dev token and not logged in. Check `~/.scion/dev-token` exists. |
| 502 on WebSocket | Hub is not reachable. Check `HUB_API_URL` and that Hub is running. |
| Terminal connects but shows nothing | Broker not connected to Hub, or agent container not running. Check Hub logs for control channel status. |
| Terminal text is garbled/unstyled | xterm.css failed to load into shadow DOM. Check console for the CSS warning. |
| Resize doesn't work | ResizeObserver may not fire if the container has no explicit dimensions. Check that `.terminal-container` has `flex: 1` applied. |
