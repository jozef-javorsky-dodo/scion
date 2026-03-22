# Agent Visualizer — Design Proposal

**Status:** Draft (Revised)
**Location:** `extras/agent-viz/`

## Overview

A standalone 2D graph visualization tool that replays agent activity from Google Cloud Logging exports. A Go binary processes log files and serves a web-based visualizer over WebSocket, enabling playback at variable speeds.

The visualization shows:

- **File graph** — force-directed graph of the project's file/directory tree as the central element
- **Agent ring** — agents distributed radially around the file graph
- **Messaging** — transient directional pulse lines between agents on the ring, fading after ~0.5s
- **File edits** — particles traveling from agent to file node; new files materialize from the particle
- **Agent state** — Bootstrap Icons (matching the web UI status badge) inside each agent circle

## Architecture

```
extras/agent-viz/
├── cmd/
│   └── agent-viz/
│       └── main.go             # CLI entry point, flag parsing
├── internal/
│   ├── logparser/
│   │   └── parser.go           # GCP log JSON → playback events
│   ├── playback/
│   │   └── engine.go           # Timing, speed control, time-range windowing
│   └── server/
│       └── server.go           # HTTP static file server + WebSocket handler
├── web/                        # Frontend (built assets embedded in binary or served from disk)
│   ├── index.html
│   ├── package.json            # Vite + force-graph (2D) + d3
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── src/
│       ├── main.ts             # Init, connect WebSocket, bootstrap visualization
│       ├── graph.ts            # Force-graph setup, file tree layout
│       ├── agents.ts           # Radial agent ring rendering and state icons
│       ├── messages.ts         # Transient message pulse lines between agents
│       ├── files.ts            # File edit particles, new file materialization
│       ├── playback.ts         # Transport controls, speed, scrubber, filters
│       ├── ws.ts               # WebSocket client, event dispatch
│       ├── types.ts            # TypeScript interfaces for playback events
│       └── icons.ts            # Bootstrap Icon SVG references for agent state
├── go.mod
├── go.sum
└── README.md
```

### Usage

```bash
agent-viz --log-file /path/to/logs.json [--port 8080]
# Opens browser to http://localhost:8080
```

## Data Source: GCP Log Exports

The input is JSON log files exported from Google Cloud Logging (as in `.scratch/downloaded-logs-*.json`). The Go binary parses these and converts them to a normalized playback event stream.

### Log Streams Consumed

| Log Name | Event Types | Visualizer Use |
|---|---|---|
| `scion-agents` | `agent.session.start/end`, `agent.turn.start/end`, `agent.tool.call/result`, `agent.lifecycle.*` | Agent state changes, file edit detection |
| `scion-messages` | `message dispatched`, `message accepted (buffered)`, `notification message dispatched` | Message flow between agents |
| `scion-server` | Server-side events | Context (grove setup, broker registration) |

### Key Log Fields

**Agent events** (from `scion-agents`):
- `labels.agent_id` — agent UUID
- `labels.scion.harness` — harness type (gemini, claude)
- `labels.grove_id` — grove context
- `jsonPayload.event.name` — event type (session-start, tool-start, agent-end, etc.)
- `jsonPayload.tool_name` — tool being called (for file edit detection)
- `timestamp` — event time for playback ordering

**Message events** (from `scion-messages`):
- `jsonPayload.sender` — e.g., `agent:green-agent`
- `jsonPayload.recipient` — e.g., `agent:orchestrator`
- `jsonPayload.msg_type` — `instruction`, `state-change`, `input-needed`
- `jsonPayload.message_content` — message text
- `jsonPayload.broadcasted` — whether it was a broadcast

## Playback Event Format

The Go log processor normalizes GCP log entries into a stream of typed events sent over WebSocket:

```typescript
// Sent once at connection start
interface PlaybackManifest {
  type: "manifest"
  timeRange: { start: string; end: string }   // ISO 8601
  agents: AgentInfo[]                           // All agents seen in logs
  files: FileNode[]                             // File tree (up to depth cutoff)
  groveId: string
  groveName: string
}

interface AgentInfo {
  id: string
  name: string        // slug used as label
  harness: string     // gemini, claude, generic
  color: string       // assigned by processor
}

interface FileNode {
  id: string          // relative path
  name: string        // basename
  parent: string      // parent directory path
  isDir: boolean
}

// Streamed during playback
interface PlaybackEvent {
  type: "agent_state" | "message" | "file_edit" | "agent_create" | "agent_destroy"
  timestamp: string   // original log timestamp
  data: AgentStateEvent | MessageEvent | FileEditEvent | AgentLifecycleEvent
}

interface AgentStateEvent {
  agentId: string
  phase?: string       // created, running, stopped, error
  activity?: string    // idle, thinking, executing, waiting_for_input, completed, etc.
  toolName?: string    // when activity=executing
}

interface MessageEvent {
  sender: string       // agent slug or "user:<name>" or "system"
  recipient: string    // agent slug
  msgType: string      // instruction, state-change, input-needed
  content?: string     // message text (for tooltip/detail)
  broadcasted: boolean
}

interface FileEditEvent {
  agentId: string
  filePath: string     // relative path to file
  action: "create" | "edit"
}

interface AgentLifecycleEvent {
  agentId: string
  name: string
  action: "create" | "destroy"
}
```

### WebSocket Control Messages (Browser → Server)

```typescript
interface PlaybackCommand {
  type: "play" | "pause" | "seek" | "speed" | "filter"
  // seek
  timestamp?: string
  // speed
  multiplier?: number          // 1, 2, 5, 10, 20, 50, 100
  // filter
  agents?: string[]            // agent IDs to include (empty = all)
  eventTypes?: string[]        // event types to include (empty = all)
  timeRange?: { start: string; end: string }
}
```

## Visualization Layout

### Spatial Structure

```
                    ┌─ Agent A (90°)
                    │
        ┌───────────┼───────────┐
        │           │           │
Agent D ┤     Force-directed    ├ Agent B
(180°)  │      file/dir graph   │  (0°)
        │     (center mass)     │
        └───────────┼───────────┘
                    │
                    └─ Agent C (270°)
```

- **Center:** Force-directed graph of the project file tree (directories and files up to a configurable depth cutoff)
- **Ring:** Agents fixed on a circle around the file graph, evenly spaced (N agents → 360/N degrees apart)
- **Messages:** Transient colored line from sender to recipient on the ring, directionally pulsed, fades over ~0.5s
- **File edits:** Small particle travels from agent on the ring inward to the target file node
- **File creates:** Particle travels to the position where the new file node will appear, then grows/expands into the file node

### File Graph

- Rendered at startup from the manifest's file tree
- Force-directed layout using `force-graph` (2D) or `d3-force`
- Directories shown as larger nodes, files as smaller nodes
- Connected by parent-child links
- Depth cutoff configurable (default: 3-4 levels) — files beyond cutoff roll up to their parent directory
- All files visible from the start; they highlight/glow when touched during playback

### Agent Nodes

- Fixed position on radial ring (not part of force simulation)
- Circle with agent color fill + label (slug) below
- **State icon** inside the circle uses Bootstrap Icons matching the web frontend status badge:

| Activity | Icon | Variant Color |
|---|---|---|
| `idle` | `circle-fill` | green |
| `thinking` | `lightning-charge` | blue (pulsing) |
| `executing` | `gear` | blue (pulsing) |
| `waiting_for_input` | `chat-dots` | amber |
| `blocked` | `clock-history` | gray |
| `completed` | `check-circle` | green |
| `limits_exceeded` | `exclamation-octagon` | red |
| `stalled` | `hourglass-bottom` | amber |
| `offline` | `wifi-off` | gray |

Phase icons (for lifecycle states before running):

| Phase | Icon | Variant Color |
|---|---|---|
| `created` | `circle` | gray |
| `provisioning` / `cloning` / `starting` | `hourglass-split` / `arrow-down-circle` / `arrow-repeat` | amber (pulsing) |
| `running` | `play-circle` | green |
| `stopping` | `arrow-repeat` | amber (pulsing) |
| `stopped` | `stop-circle` | gray |
| `error` | `exclamation-triangle` | red |

### Message Lines

- Drawn as a straight line between two agent positions on the ring
- Directional pulse animation (bright dot travels from sender to recipient)
- Line color matches sender's agent color
- Entire line fades to transparent over ~0.5s after the pulse completes
- Message type affects pulse style:
  - `instruction` — bright, fast
  - `state-change` — slower, pulsing
  - `input-needed` — amber, attention-grabbing

## Playback Controls

### Transport Bar (bottom of viewport)

```
[|◄] [►/❚❚] [►|]   ──●──────────────────  1x [▾]   16:33:06 / 16:37:43
 rew  play   fwd        time scrubber       speed      current / total
```

- **Play/Pause** — toggle playback
- **Speed multiplier** — dropdown: 1x, 2x, 5x, 10x, 20x, 50x, 100x
- **Time scrubber** — drag to seek to any point in the log timeline
- **Time range filter** — set start/end bounds (server-side windowing)

### Filter Panel (sidebar or dropdown)

- **Agent filter** — checkboxes to show/hide specific agents
- **Event type filter** — toggle visibility of: messages, file edits, state changes, lifecycle events
- Filters sent to Go server so it skips irrelevant events (reduces WebSocket traffic)

## Technology Stack

### Go Backend

| Component | Purpose |
|---|---|
| `logparser` | Parse GCP JSON log format, normalize to playback events, build file tree and agent list |
| `playback` | Maintain sorted event timeline, handle seek/speed/filter commands, emit events at correct pace |
| `server` | Serve static web assets, upgrade to WebSocket, dispatch playback events |

### Web Frontend

| Library | Purpose |
|---|---|
| `force-graph` (vasturiano) | 2D force-directed graph for file tree — Canvas2D based, same API family as 3d-force-graph |
| `d3-force` | Underlying force simulation (bundled with force-graph) |
| Canvas2D / SVG overlay | Agent ring, message pulse lines, file edit particles |
| Bootstrap Icons (SVG) | Agent state icons inside circles |
| Vite | Build tool |
| TypeScript | Type safety |

### Why `force-graph` (2D)

- Same author/API as `3d-force-graph` but renders to Canvas2D — simpler, faster, no WebGL required
- `emitParticle(link)` API available for file edit particles on the file tree links
- Custom node rendering via `nodeCanvasObject` callback
- Framework-agnostic (no React/Lit dependency)
- Handles hundreds of nodes comfortably

## File Edit Detection

File-related tool calls are identified by `tool_name` in `agent.tool.call` log events:

| Tool Name | Action |
|---|---|
| `write_file`, `create_file`, `Write` | `create` (if file is new) or `edit` |
| `edit_file`, `Edit`, `patch_file` | `edit` |
| `read_file`, `Read` | Ignored (read-only, no visual) |
| `run_shell_command`, `Bash` | Ignored (can't reliably determine file ops) |

The log processor extracts the file path from the tool call payload when available.

## Decisions Log

Captured from design review:

| # | Question | Decision |
|---|---|---|
| 1 | Live SSE vs replay? | **Replay from GCP logs** — no live SSE for MVP. Go server can tail logs for future live mode. |
| 2 | File activity scope? | **File-edit tool calls only** (write_file, edit_file, etc.) — ignore shell commands. |
| 3 | Link/edge discovery? | **N/A** — agents on radial ring, not in force graph. Message lines are transient, not persistent edges. |
| 4 | Layout strategy? | **Hybrid** — force-directed file tree in center, agents on fixed radial ring around it. |
| 5 | Scale target? | **Up to 50 agents, hundreds of files.** Depth cutoff on file tree to manage complexity. |
| 6 | 2D vs 3D? | **2D.** Simpler, clearer, no camera confusion. |
| 7 | Authentication? | **None needed** — Go binary reads local log files, serves locally. |
| 8 | Embedding? | **Standalone binary** in `extras/agent-viz/`. |
| 9 | Replay priority? | **Primary mode** — replay is the MVP, not a stretch goal. |
| 10 | Audio? | **Not addressed** — defer. |
| 11 | Deployment? | **Standalone Go binary** (`agent-viz`) in `extras/agent-viz/`. Not part of scion CLI. |
| 12 | Configuration? | **Playback controls:** play/pause, speed multiplier, time scrubber, time range, agent filter, event type filter. |

## MVP Scope

1. Go binary reads GCP log JSON, parses into playback event stream
2. Serves web visualizer on localhost, streams events over WebSocket
3. 2D force-directed file graph (center) with configurable depth cutoff
4. Agents on radial ring with color + label + state icons
5. Message pulse lines between agents (transient, fading)
6. File edit particles from agent to file node; new files materialize
7. Playback controls: play/pause, speed (1x-100x), time scrubber
8. Agent and event type filters

**Deferred:**
- Live mode (tail log stream or connect to hub SSE)
- Audio cues
- Embedding in main web UI
- Click-to-inspect detail overlays
