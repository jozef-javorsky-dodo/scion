# Notifications: Event Subscriptions and Message Dispatch

## Status
**Design** | February 2026

## Problem

When one agent creates another (via `scion start`), the creating agent currently has no way to be notified when the spawned agent reaches a terminal or actionable state — `COMPLETED`, `WAITING_FOR_INPUT`, or `LIMITS_EXCEEDED`. The creating agent must either poll the Hub API or rely on ad-hoc coordination.

This is a critical gap for multi-agent orchestration workflows where a "lead" agent delegates tasks and needs to react when sub-agents finish or need intervention.

Beyond agent-to-agent notification, human users managing agents also need visibility into status changes — particularly when agents complete, stall, or require input. The notification system must support both audiences.

### Initial priority Use Case: Agent-to-Agent
```
# The creating agent spawns fooagent and opts into notifications
scion start --notify fooagent "Implement the auth module"

# When fooagent reaches COMPLETED:
# → notification is stored, then immediately dispatched as:
#   scion message <creating-agent> "fooagent has reached a state of COMPLETED"

# When fooagent reaches WAITING_FOR_INPUT with a question:
# → scion message <creating-agent> "fooagent is WAITING_FOR_INPUT: <question>"
```

The `--notify` flag is a binary flag. The notification target is always the agent that issued the `scion start` command (resolved from its JWT identity). It is not an arbitrary recipient selector.

### Near-Term Use Case: Human Notifications

Human users can query their accumulated notifications via the Hub CLI:

```bash
# List unacknowledged notifications
scion hub notifications

# Acknowledge a specific notification
scion hub notifications ack <notification-id>

# Acknowledge all notifications
scion hub notifications ack --all
```

Human notification delivery (web UI tray, email, Slack, push) will be covered in a separate design. The initial implementation stores notifications for all subscriber types, but only dispatches immediately for agent targets. Human-targeted notifications accumulate in the store for retrieval via the CLI (and eventually the web UI).

### Future Extensions

- **Web UI notification tray** — real-time display of accumulated notifications in the browser.
- **Stale/stalled detection** — notify when an agent hasn't produced an event within a configurable timeout. *(Deferred.)*
- **`scion get-notified <agent>`** — a separate command for additional actors to subscribe to an already-running agent's events.
- **Multiple subscribers per agent** — initial implementation stores subscribers as a list, even if only one is common at first.

---

## Current Architecture Context

### Event Flow Today

1. **Inside the container**: `sciontool` hooks intercept harness events (Claude Code, Gemini CLI) and translate them into status updates via the Hub API (`POST /api/v1/agents/{id}/status`). See `pkg/sciontool/hooks/handlers/hub.go`.
2. **Hub handler**: `updateAgentStatus()` in `pkg/hub/handlers.go:1212` persists the status change to the store and publishes an event via `EventPublisher`.
3. **EventPublisher** (`pkg/hub/events.go`): `ChannelEventPublisher` fans out to in-process subscribers using NATS-style subject matching. Subjects: `agent.{id}.status`, `grove.{groveId}.agent.status`.
4. **SSE endpoint** (`/events?sub=...`): Browser clients subscribe to events for real-time UI updates.

### Key Status Values (from `pkg/sciontool/hooks/types.go`)

| Status | Sticky? | Notification-Worthy |
|---|---|---|
| `WAITING_FOR_INPUT` | Yes | **Yes** — agent needs human/agent intervention |
| `COMPLETED` | Yes | **Yes** — agent finished its task |
| `LIMITS_EXCEEDED` | Yes | **Yes** — agent hit token/turn limits |
| `THINKING`, `EXECUTING`, `IDLE` | No | No — transient operational states |
| `ERROR` | No | Future consideration |
| `EXITED` | No | Future consideration |

### Messaging Today

`scion message <agent> <text>` sends a message to an agent via the Hub API (`POST /api/v1/agents/{id}/message`), which dispatches through the runtime broker's control channel to inject text into the agent's tmux session.

### Agent-to-Hub Access Today

Agents receive a JWT token (`SCION_HUB_TOKEN`) with scopes. Per the `agent-hub-access.md` design, agents with `grove:agent:create` and `grove:agent:lifecycle` scopes can already create and manage peer agents within their grove. The `scion` CLI inside containers reads `SCION_HUB_ENDPOINT` and `SCION_HUB_TOKEN` to communicate with the Hub.

---

## Design Approaches

### Approach A: Hub-Side Event Listener with Subscription Store *(Selected)*

**Summary**: The Hub maintains subscription and notification tables. When a status event is published via `EventPublisher`, a dedicated notification dispatcher goroutine checks for matching subscriptions, **stores a notification record**, and then dispatches it to the appropriate target. For agent subscribers, dispatch is immediate via the message API. For human subscribers, notifications accumulate in the store for retrieval.

#### Data Model

```sql
CREATE TABLE notification_subscriptions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,              -- agent being watched
    subscriber_type TEXT NOT NULL,       -- 'agent' | 'user'
    subscriber_id TEXT NOT NULL,         -- slug or ID of the subscriber
    grove_id TEXT NOT NULL,              -- grove scope for authorization
    trigger_statuses TEXT NOT NULL,      -- JSON array: ["COMPLETED", "WAITING_FOR_INPUT", "LIMITS_EXCEEDED"]
    created_at TIMESTAMP NOT NULL,
    created_by TEXT NOT NULL,            -- who created the subscription
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);

CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,       -- which subscription generated this
    agent_id TEXT NOT NULL,              -- agent that triggered the notification
    grove_id TEXT NOT NULL,
    subscriber_type TEXT NOT NULL,       -- 'agent' | 'user'
    subscriber_id TEXT NOT NULL,
    status TEXT NOT NULL,                -- the status that triggered it (UPPER CASE)
    message TEXT NOT NULL,               -- formatted notification message
    dispatched INTEGER NOT NULL DEFAULT 0, -- whether dispatch has been attempted
    acknowledged INTEGER NOT NULL DEFAULT 0, -- for human notifications: ack'd?
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (subscription_id) REFERENCES notification_subscriptions(id) ON DELETE CASCADE
);
```

#### Flow

1. **CLI**: `scion start --notify fooagent "task..."` → `CreateAgentRequest` includes `Notify: true`. The Hub resolves the issuing agent's identity from the JWT.
2. **Hub** `createAgent` handler: After creating the agent, inserts a subscription row with the creating agent as the subscriber.
3. **Hub** `NotificationDispatcher` goroutine: Subscribes to `grove.{groveId}.agent.status` via `ChannelEventPublisher.Subscribe(...)`. On each event:
   - Query `notification_subscriptions` for the agent ID.
   - If the status matches a trigger status and differs from the last notification for this subscription, **store a notification record** in the `notifications` table.
   - For agent subscribers: immediately dispatch the message via the message API.
   - For user subscribers: the notification is stored and available for retrieval (no immediate dispatch in the initial implementation).
4. **Cleanup**: Subscriptions are deleted via `ON DELETE CASCADE` when the watched agent is deleted. Notifications cascade from subscriptions.

#### Architecture Diagram

```
┌──────────────┐     status update     ┌──────────────┐
│  sciontool   │ ──────────────────────>│   Hub API    │
│  (in agent)  │   POST /agents/id/    │  handlers.go │
└──────────────┘      status           └──────┬───────┘
                                              │
                                    store.UpdateAgentStatus()
                                    events.PublishAgentStatus()
                                              │
                                              ▼
                                    ┌──────────────────┐
                                    │  EventPublisher   │
                                    │  (channels)       │
                                    └────┬─────────────┘
                                         │
                            ┌────────────┴────────────┐
                            ▼                         ▼
                   ┌────────────────┐       ┌──────────────────────┐
                   │  SSE endpoint  │       │ NotificationDispatcher│
                   │  (browsers)    │       │  (goroutine)          │
                   └────────────────┘       └──────────┬───────────┘
                                                       │
                                            query subscriptions table
                                            match status → triggers
                                                       │
                                              ┌────────┴────────┐
                                              ▼                 ▼
                                    ┌──────────────┐  ┌──────────────────┐
                                    │ Store in      │  │ Dispatch to      │
                                    │ notifications │  │ agent via        │
                                    │ table         │  │ message API      │
                                    └──────────────┘  └──────────────────┘
```

#### Pros

- **Leverages existing infrastructure**: EventPublisher, message API, and control channel all exist and work today.
- **Centralized logic**: All notification matching and dispatch happens in one place (the Hub), making it easy to add new subscriber types (users, webhooks) later.
- **Transactional subscription creation**: Subscriptions are created atomically with agent creation — no race condition between agent starting and subscription registration.
- **Clean lifecycle**: `ON DELETE CASCADE` ensures subscriptions don't outlive the watched agent.
- **Database-backed durability**: Subscriptions and notifications survive Hub restarts (the dispatcher re-subscribes to the EventPublisher on startup).
- **Store-then-dispatch pattern**: Separating storage from dispatch means human notifications naturally accumulate for later retrieval, and agent notifications have an audit trail.

#### Cons

- **Hub becomes message broker**: The Hub gains a new responsibility (dispatch), which adds complexity to what is primarily a state server.
- **Single-node limitation**: `ChannelEventPublisher` is in-process. In a multi-Hub deployment (future), subscriptions on one Hub wouldn't receive events processed by another. This mirrors the existing SSE limitation and would be resolved by the same `PostgresEventPublisher` migration path.
- **Agent message delivery is best-effort**: If the subscriber agent is stopped or the broker is disconnected, the notification is stored but the dispatch fails. The failure is logged at WARN level and the message is dropped (no retry).

> **Future consideration**: A lightweight bridge to the realtime web publisher (Approach C's decorator pattern) could be added later to push notifications to the SSE/WebSocket layer for browser clients, without replacing the core store-then-dispatch architecture.

---

### Approach B: Polling-Based Notification Service (Sidecar Goroutine) *(Not selected)*

**Summary**: Instead of subscribing to the in-process event stream, a background goroutine periodically polls the store for agents with active subscriptions whose status matches a trigger condition.

#### Flow

1. **Subscription creation**: Same as Approach A (table + API).
2. **Polling loop**: A `NotificationPoller` goroutine runs every N seconds (e.g., 5s):
   - `SELECT a.id, a.status, a.message, ns.* FROM agents a JOIN notification_subscriptions ns ON a.id = ns.agent_id WHERE a.status IN (trigger_statuses) AND ns.last_notified_status != a.status`
   - For each match, dispatch a message and update `ns.last_notified_status`.
3. **Deduplication**: The `last_notified_status` column prevents repeat notifications for the same status transition.

#### Pros

- **Multi-node safe**: Works correctly in multi-Hub deployments since it polls the shared database rather than relying on in-process events.
- **Simpler event integration**: No dependency on `EventPublisher` subscription machinery; just reads from the database.
- **Naturally idempotent**: The `last_notified_status` column prevents duplicate notifications.
- **Resilient to Hub restarts**: No event subscriptions to re-establish; the next poll cycle catches up.

#### Cons

- **Latency**: Notifications are delayed by up to the poll interval (5-10 seconds). For the use case of notifying an agent that its sub-agent completed, this is likely acceptable — but it's not instant.
- **Database load**: Joins across `agents` and `notification_subscriptions` on every poll interval. With a small number of subscriptions this is negligible; at scale it would need an index and possibly a materialized view.
- **Missed transient states**: If an agent transitions through a trigger status and back before the next poll (unlikely for sticky statuses, but possible), the notification could be missed.
- **Not extensible to real-time**: Can't easily support future real-time notification channels (WebSocket push to users) without adding the event-based path anyway.

---

### Approach C: EventPublisher Decorator (Middleware Pattern) *(Not selected; noted for future bridge)*

**Summary**: Wrap the existing `EventPublisher` with a `NotifyingEventPublisher` decorator that intercepts `PublishAgentStatus` calls, checks for matching subscriptions, and dispatches notifications inline before (or after) delegating to the underlying publisher.

#### Pros

- **Zero new goroutines**: Notification dispatch piggybacks on the existing event publish path (with async dispatch via goroutines for each notification).
- **Instant**: Notifications are dispatched at the exact moment the status event is published.
- **Transparent**: Existing code that calls `events.PublishAgentStatus()` automatically gains notification capability without changes.
- **Composable**: Additional decorators (logging, metrics, rate limiting) can be stacked.

#### Cons

- **Tight coupling**: Notification logic runs in the request path of status updates. Even with async dispatch, the subscription lookup adds latency to every status update.
- **Error isolation**: If the subscription lookup panics or blocks, it could affect the status update handler (mitigation: catch panics in the decorator).
- **DB call on hot path**: Every `PublishAgentStatus` call now hits the database to check for subscriptions, even when most agents have none. Mitigation: an in-memory cache of "agents with subscriptions" that's invalidated on subscription create/delete.
- **Same single-node limitation as Approach A**: Relies on `ChannelEventPublisher` which is in-process only.

> **Note**: While not selected as the primary approach, the decorator pattern may serve as a future lightweight bridge to push notifications into the realtime web publisher for browser clients.

---

## Comparison Matrix

| Criterion | A: Hub Dispatcher | B: Polling | C: Decorator |
|---|---|---|---|
| **Latency** | Near-instant (<100ms) | 5-10s poll interval | Near-instant (<100ms) |
| **Multi-node ready** | No (same as SSE) | Yes | No (same as SSE) |
| **Implementation complexity** | Medium | Low | Low-Medium |
| **New goroutines** | 1 (dispatcher loop) | 1 (poller loop) | 0 (async per notification) |
| **DB queries** | On event (subscriptions only) | On interval (join) | On every status publish |
| **Missed notifications** | No (event-driven) | Possible (transient states) | No (event-driven) |
| **Hub restart resilience** | Re-subscribe on startup | Automatic | Re-wrap on startup |
| **Future extensibility** | High (webhook, email, etc.) | Medium | High (composable) |
| **Separation of concerns** | Good (dedicated component) | Good (separate loop) | Lower (mixed into publisher) |

---

## Decision

**Approach A (Hub-Side Event Listener)** is approved for the initial implementation.

Rationale:

1. **The Hub already has all the pieces**: `EventPublisher` with subject-matching, the message API, and the dispatcher for routing to brokers. Approach A composes these existing components with minimal new code.

2. **Near-instant notification matters**: When a sub-agent completes, the lead agent should know immediately so it can integrate the result and continue its own work. A 5-10s polling delay (Approach B) is unnecessarily slow.

3. **Clean separation**: A `NotificationDispatcher` is a clearly-scoped component that can be tested independently, unlike Approach C which mixes notification logic into the publisher.

4. **The single-node limitation is acceptable**: The existing SSE system has the same constraint. The migration path to `PostgresEventPublisher` (described in `web-realtime.md`) will benefit notifications equally when it happens.

5. **Store-then-dispatch enables human notifications**: By persisting notifications before dispatching, agent notifications get an audit trail and human notifications naturally accumulate for CLI/web retrieval without additional infrastructure.

6. **Future bridge to realtime web publisher**: The Approach C decorator pattern remains an option for later bridging notifications into the SSE/WebSocket layer for browser clients.

---

## Detailed Design (Approach A)

### 1. Data Model

#### `notification_subscriptions` Table

```sql
CREATE TABLE notification_subscriptions (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,                -- Agent being watched
    subscriber_type TEXT NOT NULL DEFAULT 'agent',  -- 'agent' | 'user'
    subscriber_id TEXT NOT NULL,           -- Slug or ID of the subscriber entity
    grove_id TEXT NOT NULL,                -- Grove scope (authorization boundary)
    trigger_statuses TEXT NOT NULL,        -- JSON array, e.g. '["COMPLETED","WAITING_FOR_INPUT","LIMITS_EXCEEDED"]'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,              -- Principal that created the subscription
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);

CREATE INDEX idx_notification_subs_agent ON notification_subscriptions(agent_id);
CREATE INDEX idx_notification_subs_grove ON notification_subscriptions(grove_id);
```

#### `notifications` Table

```sql
CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    subscription_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,                -- Agent that triggered the notification
    grove_id TEXT NOT NULL,
    subscriber_type TEXT NOT NULL,         -- 'agent' | 'user'
    subscriber_id TEXT NOT NULL,
    status TEXT NOT NULL,                  -- Trigger status (UPPER CASE, e.g. "COMPLETED")
    message TEXT NOT NULL,                 -- Formatted notification message
    dispatched INTEGER NOT NULL DEFAULT 0, -- 1 if dispatch was attempted (agent targets)
    acknowledged INTEGER NOT NULL DEFAULT 0, -- 1 if acknowledged (human targets)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (subscription_id) REFERENCES notification_subscriptions(id) ON DELETE CASCADE
);

CREATE INDEX idx_notifications_subscriber ON notifications(subscriber_type, subscriber_id);
CREATE INDEX idx_notifications_grove ON notifications(grove_id);
```

#### Store Interface Extension

```go
// NotificationStore manages notification subscriptions and notification records.
type NotificationStore interface {
    // Subscriptions
    CreateNotificationSubscription(ctx context.Context, sub NotificationSubscription) error
    GetNotificationSubscriptions(ctx context.Context, agentID string) ([]NotificationSubscription, error)
    GetNotificationSubscriptionsByGrove(ctx context.Context, groveID string) ([]NotificationSubscription, error)
    DeleteNotificationSubscription(ctx context.Context, id string) error
    DeleteNotificationSubscriptionsForAgent(ctx context.Context, agentID string) error

    // Notifications
    CreateNotification(ctx context.Context, notif Notification) error
    GetNotifications(ctx context.Context, subscriberType, subscriberID string, onlyUnacknowledged bool) ([]Notification, error)
    AcknowledgeNotification(ctx context.Context, id string) error
    AcknowledgeAllNotifications(ctx context.Context, subscriberType, subscriberID string) error
    GetLastNotificationStatus(ctx context.Context, subscriptionID string) (string, error)
}
```

#### Models

```go
type NotificationSubscription struct {
    ID              string    `json:"id"`
    AgentID         string    `json:"agentId"`         // Agent being watched
    SubscriberType  string    `json:"subscriberType"`  // "agent" | "user"
    SubscriberID    string    `json:"subscriberId"`    // Slug or ID
    GroveID         string    `json:"groveId"`
    TriggerStatuses []string  `json:"triggerStatuses"` // e.g. ["COMPLETED", "WAITING_FOR_INPUT"]
    CreatedAt       time.Time `json:"createdAt"`
    CreatedBy       string    `json:"createdBy"`
}

type Notification struct {
    ID              string    `json:"id"`
    SubscriptionID  string    `json:"subscriptionId"`
    AgentID         string    `json:"agentId"`
    GroveID         string    `json:"groveId"`
    SubscriberType  string    `json:"subscriberType"`
    SubscriberID    string    `json:"subscriberId"`
    Status          string    `json:"status"`          // UPPER CASE
    Message         string    `json:"message"`
    Dispatched      bool      `json:"dispatched"`
    Acknowledged    bool      `json:"acknowledged"`
    CreatedAt       time.Time `json:"createdAt"`
}
```

### 2. CLI Changes

#### `--notify` Flag on `start` Command

The `--notify` flag is a **boolean flag** (not a string argument). When present, the creating agent (identified by its JWT identity) is automatically registered as the notification subscriber.

**File:** `cmd/start.go`

```go
var notify bool

func init() {
    startCmd.Flags().BoolVar(&notify, "notify", false,
        "Get notified when the spawned agent reaches COMPLETED, WAITING_FOR_INPUT, or LIMITS_EXCEEDED")
}
```

Usage:
```bash
scion start --notify fooagent "Do the thing"
```

#### Passing to Hub

**File:** `cmd/common.go` — `startAgentViaHub()`

The `--notify` flag is passed as a boolean on the `CreateAgentRequest`. The Hub resolves the subscriber identity from the authenticated principal (JWT).

```go
req := &hubclient.CreateAgentRequest{
    Name:    agentName,
    GroveID: groveID,
    // ... existing fields ...
    Notify:  notify,
}
```

**File:** `pkg/hubclient/agents.go`

```go
type CreateAgentRequest struct {
    // ... existing fields ...
    Notify bool `json:"notify,omitempty"` // Subscribe the creating agent to status notifications
}
```

### 3. Hub Handler Changes

**File:** `pkg/hub/handlers.go` — `createAgent()`

After successfully creating the agent, if `req.Notify` is true, the handler resolves the subscriber from the authenticated identity:

```go
if req.Notify {
    subscriberID := identity.PrincipalID() // The creating agent's slug or ID
    sub := store.NotificationSubscription{
        ID:              uuid.New().String(),
        AgentID:         agent.ID,
        SubscriberType:  identity.PrincipalType(), // "agent" or "user"
        SubscriberID:    subscriberID,
        GroveID:         agent.GroveID,
        TriggerStatuses: []string{"COMPLETED", "WAITING_FOR_INPUT", "LIMITS_EXCEEDED"},
        CreatedBy:       subscriberID,
    }
    if err := s.store.CreateNotificationSubscription(ctx, sub); err != nil {
        slog.Warn("Failed to create notification subscription",
            "agentID", agent.ID, "subscriber", subscriberID, "error", err)
    }
}
```

### 4. Notification Dispatcher

**New file:** `pkg/hub/notifications.go`

The dispatcher follows a **store-then-dispatch** pattern: every notification-worthy event is first persisted, then dispatched based on subscriber type.

```go
type NotificationDispatcher struct {
    store      store.Store
    events     *ChannelEventPublisher
    dispatcher AgentDispatcher
    stopCh     chan struct{}
}

func NewNotificationDispatcher(store store.Store, events *ChannelEventPublisher, dispatcher AgentDispatcher) *NotificationDispatcher {
    return &NotificationDispatcher{
        store:      store,
        events:     events,
        dispatcher: dispatcher,
        stopCh:     make(chan struct{}),
    }
}

func (n *NotificationDispatcher) Start() {
    // Subscribe to all agent status events across all groves
    ch, unsub := n.events.Subscribe("grove.>.agent.status")

    go func() {
        defer unsub()
        for {
            select {
            case evt, ok := <-ch:
                if !ok {
                    return
                }
                n.handleEvent(evt)
            case <-n.stopCh:
                return
            }
        }
    }()
}

func (n *NotificationDispatcher) handleEvent(evt Event) {
    var statusEvt AgentStatusEvent
    if err := json.Unmarshal(evt.Data, &statusEvt); err != nil {
        return
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    subs, err := n.store.GetNotificationSubscriptions(ctx, statusEvt.AgentID)
    if err != nil || len(subs) == 0 {
        return
    }

    for _, sub := range subs {
        if !sub.MatchesStatus(statusEvt.Status) {
            continue
        }

        // Dedup: check if we already notified for this status
        lastStatus, _ := n.store.GetLastNotificationStatus(ctx, sub.ID)
        if strings.EqualFold(lastStatus, statusEvt.Status) {
            continue
        }

        go n.storeAndDispatch(sub, statusEvt)
    }
}

func (n *NotificationDispatcher) storeAndDispatch(sub store.NotificationSubscription, evt AgentStatusEvent) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Build the notification message
    agent, err := n.store.GetAgent(ctx, evt.AgentID)
    if err != nil {
        slog.Warn("Failed to fetch agent for notification", "agentID", evt.AgentID, "error", err)
        return
    }

    message := formatNotificationMessage(agent, evt.Status)

    // Step 1: Store the notification
    notif := store.Notification{
        ID:             uuid.New().String(),
        SubscriptionID: sub.ID,
        AgentID:        evt.AgentID,
        GroveID:        sub.GroveID,
        SubscriberType: sub.SubscriberType,
        SubscriberID:   sub.SubscriberID,
        Status:         strings.ToUpper(evt.Status),
        Message:        message,
    }
    if err := n.store.CreateNotification(ctx, notif); err != nil {
        slog.Warn("Failed to store notification", "error", err)
        return
    }

    // Step 2: Dispatch based on subscriber type
    switch sub.SubscriberType {
    case "agent":
        n.dispatchToAgent(ctx, sub, notif)
    case "user":
        // Human notifications are stored only; retrieval is via CLI/web.
        slog.Info("Notification stored for user",
            "user", sub.SubscriberID, "agent", agent.Slug, "status", evt.Status)
    }
}

func (n *NotificationDispatcher) dispatchToAgent(ctx context.Context, sub store.NotificationSubscription, notif store.Notification) {
    // Resolve subscriber agent by slug within the grove
    subscriber, err := n.store.GetAgentBySlug(ctx, sub.GroveID, sub.SubscriberID)
    if err != nil {
        slog.Warn("Notification subscriber agent not found",
            "subscriber", sub.SubscriberID, "grove", sub.GroveID, "error", err)
        return
    }

    // Use the dispatcher to send the message via the broker (non-interrupt)
    if n.dispatcher != nil && subscriber.RuntimeBrokerID != "" {
        if err := n.dispatcher.DispatchAgentMessage(ctx, subscriber, notif.Message, false); err != nil {
            slog.Warn("Failed to dispatch notification to agent, dropping message",
                "subscriber", sub.SubscriberID, "error", err)
        } else {
            slog.Info("Notification dispatched to agent",
                "from", sub.AgentID, "to", sub.SubscriberID, "status", notif.Status)
        }
    } else {
        slog.Warn("Subscriber agent not reachable, dropping notification",
            "subscriber", sub.SubscriberID, "brokerID", subscriber.RuntimeBrokerID)
    }

    // Mark as dispatched regardless of success (best-effort)
    _ = n.store.MarkNotificationDispatched(ctx, notif.ID)
}

func formatNotificationMessage(agent *store.Agent, status string) string {
    normalized := strings.ToUpper(status)
    switch normalized {
    case "COMPLETED":
        msg := fmt.Sprintf("%s has reached a state of COMPLETED", agent.Slug)
        if agent.TaskSummary != "" {
            msg += ": " + agent.TaskSummary
        }
        return msg
    case "WAITING_FOR_INPUT":
        msg := fmt.Sprintf("%s is WAITING_FOR_INPUT", agent.Slug)
        if agent.Message != "" {
            msg += ": " + agent.Message
        }
        return msg
    case "LIMITS_EXCEEDED":
        msg := fmt.Sprintf("%s has reached a state of LIMITS_EXCEEDED", agent.Slug)
        if agent.Message != "" {
            msg += ": " + agent.Message
        }
        return msg
    default:
        return fmt.Sprintf("%s has reached status: %s", agent.Slug, normalized)
    }
}

func (n *NotificationDispatcher) Stop() {
    close(n.stopCh)
}
```

### 5. Hub Server Integration

**File:** `pkg/hub/server.go`

```go
func (s *Server) Start() error {
    // ... existing initialization ...

    // Start notification dispatcher
    if ep, ok := s.events.(*ChannelEventPublisher); ok {
        s.notificationDispatcher = NewNotificationDispatcher(s.store, ep, s.GetDispatcher())
        s.notificationDispatcher.Start()
    }

    // ... existing start logic ...
}
```

### 6. Status Normalization

Status values must be normalized to **UPPER CASE** throughout the notification system. The codebase currently uses both uppercase (`COMPLETED` from `hooks/types.go` `AgentState` constants) and lowercase (`completed` from `pkg/sciontool/hub/client.go` `AgentStatus` constants). The notification system normalizes all comparisons and storage to uppercase.

Longer term, the codebase should converge on uppercase status values and use strongly typed constants in models rather than bare strings.

The `MatchesStatus` method on `NotificationSubscription` normalizes case:

```go
func (s *NotificationSubscription) MatchesStatus(status string) bool {
    normalized := strings.ToUpper(status)
    for _, trigger := range s.TriggerStatuses {
        if strings.ToUpper(trigger) == normalized {
            return true
        }
    }
    return false
}
```

### 7. New Agent Token Scope

Add a scope for notification management:

```go
ScopeAgentNotify AgentTokenScope = "grove:agent:notify"
```

This scope allows an agent to create notification subscriptions for agents within its grove. It should be auto-granted alongside `grove:agent:create` and `grove:agent:lifecycle` (since the primary use case is agents that spawn sub-agents).

### 8. Local-Mode Considerations

The `--notify` flag is **Hub-only** in the initial implementation. In local mode (no Hub), the flag should produce a clear error:

```
Error: --notify requires Hub mode. Enable Hub integration or use --hub <endpoint>.
```

Future work could implement local-mode notifications via a lightweight file-based or socket-based mechanism, but this is out of scope.

### 9. Human Notification CLI

**New commands** for human users to retrieve and acknowledge notifications:

```bash
# List unacknowledged notifications for the current user
scion hub notifications

# Acknowledge a specific notification
scion hub notifications ack <notification-id>

# Acknowledge all notifications
scion hub notifications ack --all
```

These commands call Hub API endpoints that query the `notifications` table filtered by `subscriber_type = 'user'` and `subscriber_id = <current-user>`.

**Output format** (example):

```
ID          AGENT         STATUS              TIME                MESSAGE
a1b2c3d4    fooagent      COMPLETED           2026-02-24 14:30    fooagent has reached a state of COMPLETED: Auth module implemented
e5f6g7h8    bar-agent     WAITING_FOR_INPUT   2026-02-24 14:35    bar-agent is WAITING_FOR_INPUT: Which OAuth provider?
```

---

## Message Format

Notifications are delivered as plain-text messages via `scion message` (for agent targets) or stored for retrieval (for human targets). The format is designed to be parseable by LLMs (since the primary subscriber is another agent):

| Status | Message Format |
|---|---|
| `COMPLETED` | `{agent-slug} has reached a state of COMPLETED` or `{agent-slug} has reached a state of COMPLETED: {taskSummary}` |
| `WAITING_FOR_INPUT` | `{agent-slug} is WAITING_FOR_INPUT: {message/question}` |
| `LIMITS_EXCEEDED` | `{agent-slug} has reached a state of LIMITS_EXCEEDED: {message}` |

---

## Resolved Design Decisions

### 1. `--notify` flag semantics

**Decision**: `--notify` is a binary flag on `scion start`. The notification target is always the issuer of the command (resolved from JWT identity). It is not an arbitrary recipient selector.

Future work may add `scion get-notified <agent>` to allow additional actors to subscribe to an already-running agent.

### 2. Subscription expiration

**Decision**: Subscriptions are cleaned up via `ON DELETE CASCADE` when the watched agent is deleted. TTL-based expiration is a non-goal, as some use cases involve chained long-lived agent callback patterns where subscriptions must persist indefinitely.

### 3. Should notifications interrupt the subscriber agent?

**Decision**: No. Notifications are delivered as regular (non-interrupt) messages that the agent processes when it reaches its next prompt. A future `urgent` or `important` flag may be added to support interrupt-style delivery for critical notifications.

### 4. What happens if the subscriber agent is stopped?

**Decision**: The notification is stored in the `notifications` table. Dispatch to the agent is attempted but if it fails (broker disconnected, tmux session gone), the failure is logged at WARN level and the message is dropped. No retry or queuing. The notification remains in the store as a record.

### 5. REST API for managing subscriptions

**Decision**: Deferred. The initial implementation creates subscriptions implicitly via the `--notify` flag on `CreateAgentRequest`. An explicit REST API for subscription management will be added later to support `scion get-notified <agent>` and other use cases.

### 6. Cross-grove notifications

**Decision**: Not supported in the initial implementation. Both the watched agent and the subscriber must be in the same grove. This aligns with the grove isolation boundary enforced by agent JWT scopes. May be revised in the future.

### 7. Stale/stalled detection

**Decision**: Deferred. Can be implemented as a periodic check in the `NotificationDispatcher` comparing `agent.LastSeen` against a configurable threshold, but this is not part of the initial implementation.

---

## Implementation Plan

### Phase 1: Core Infrastructure ✓
1. ~~Add `notification_subscriptions` and `notifications` tables (new SQLite migration).~~
2. ~~Add `NotificationStore` interface and SQLite implementation.~~
3. ~~Add `NotificationSubscription` and `Notification` models to `pkg/store/models.go`.~~

### Phase 2: Notification Dispatcher ✓
4. ~~Implement `NotificationDispatcher` in `pkg/hub/notifications.go` with store-then-dispatch pattern.~~
5. ~~Wire dispatcher into Hub server startup/shutdown.~~
6. ~~Add unit tests for event matching, storage, dispatch, and deduplication.~~

### Phase 3: CLI and API Integration ✓
7. ~~Add `--notify` boolean flag to `cmd/start.go`.~~
8. ~~Add `Notify` field to `CreateAgentRequest` in `pkg/hubclient/agents.go`.~~
9. ~~Update `createAgent` handler to create subscriptions from request using JWT identity.~~
10. ~~Add `ScopeAgentNotify` scope and auto-grant alongside creation scopes.~~
11. ~~Error messaging for local-mode `--notify` usage.~~

### Phase 4: Human Notification CLI ✓
12. ~~Add `scion hub notifications` command to list unacknowledged notifications.~~
13. ~~Add `scion hub notifications ack <id>` and `scion hub notifications ack --all` commands.~~
14. ~~Add Hub API endpoints for notification retrieval and acknowledgment.~~

### Phase 5: Testing and Polish ✓
15. ~~Integration tests: agent-creates-agent-with-notify flow.~~
16. ~~Status normalization edge cases.~~
17. ~~Subscription cleanup on agent deletion verification.~~
18. ~~Human notification CLI end-to-end tests.~~

### Future Phases
- REST API for subscription management (`GET/POST/DELETE /subscriptions`).
- `scion get-notified <agent>` CLI command.
- Web UI notification tray (bridge to realtime web publisher).
- Human notification delivery sinks (email, Slack, web push).
- Stale/stalled detection.
- Notification `urgent`/`important` flag for interrupt delivery.
- Message retry/queuing for offline subscribers.

---

## Files Affected (Initial Implementation)

| File | Change |
|---|---|
| `pkg/store/models.go` | Add `NotificationSubscription` and `Notification` models |
| `pkg/store/store.go` | Add `NotificationStore` interface |
| `pkg/store/sqlite/sqlite.go` | New migration (two tables), interface implementation |
| `pkg/hub/notifications.go` | **New** — `NotificationDispatcher` with store-then-dispatch |
| `pkg/hub/notifications_test.go` | **New** — Unit tests |
| `pkg/hub/server.go` | Wire dispatcher into startup/shutdown |
| `pkg/hub/handlers.go` | Create subscriptions in `createAgent` |
| `pkg/hub/agenttoken.go` | Add `ScopeAgentNotify` |
| `pkg/hubclient/agents.go` | Add `Notify` bool to `CreateAgentRequest` |
| `cmd/start.go` | Add `--notify` boolean flag |
| `cmd/common.go` | Pass notify flag to Hub request |
| `cmd/hub_notifications.go` | **New** — `scion hub notifications` and `ack` commands |

---

## Related Documents

- [Agent-to-Hub Access](agent-hub-access.md) — Agent JWT scopes and sub-agent creation.
- [Web Realtime Events](web-realtime.md) — `ChannelEventPublisher` design and SSE.
- [Hub Messaging](hub-messaging.md) — CLI message routing through Hub.
- [Hub API](hub-api.md) — REST API specification.
- [Hosted Architecture](hosted-architecture.md) — System overview.
