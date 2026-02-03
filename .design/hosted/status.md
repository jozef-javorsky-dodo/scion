# Hosted Architecture Implementation Status

**Generated:** 2026-01-26
**Status:** Living Document

This document provides an analysis of the hosted architecture implementation, comparing design specifications against actual code, identifying gaps and edge cases, and proposing next milestones.

---

## 1. Implementation Status

### 1.1 Core Infrastructure

| Component | Design Doc | Implementation | Status |
|-----------|------------|----------------|--------|
| **Hub API Server** | `hub-api.md` | `pkg/hub/server.go` | ✅ Complete |
| **Runtime Host API** | `runtime-host-api.md` | `pkg/runtimehost/server.go` | ✅ Complete |
| **Store Layer** | `server-implementation-design.md` | `pkg/store/` | ✅ Complete |
| **SQLite Store** | `server-implementation-design.md` | `pkg/store/sqlite/` | ✅ Complete |
| **Hub Client Library** | `client-design.md` | `pkg/hubclient/` | ✅ Complete |
| **Host Client Library** | `client-design.md` | `pkg/hostclient/` | ✅ Complete |
| **Dev Auth** | `dev-auth.md` | `pkg/apiclient/devauth.go` | ✅ Complete |
| **Server CLI** | - | `cmd/server.go` | ✅ Complete |
| **Hub CLI** | - | `cmd/hub.go` | ✅ Complete |

### 1.2 API Endpoints

#### Hub API (`pkg/hub/`)

| Endpoint | Handlers | Notes |
|----------|----------|-------|
| `GET /healthz` | ✅ | Health check |
| `GET /api/v1/agents` | ✅ | List agents with filtering |
| `GET /api/v1/agents/:id` | ✅ | Get single agent |
| `POST /api/v1/agents` | ✅ | Create agent |
| `PATCH /api/v1/agents/:id` | ✅ | Update agent |
| `DELETE /api/v1/agents/:id` | ✅ | Delete agent |
| `GET /api/v1/groves` | ✅ | List groves |
| `POST /api/v1/groves` | ✅ | Create grove |
| `GET /api/v1/groves/:id` | ✅ | Get grove |
| `PATCH /api/v1/groves/:id` | ✅ | Update grove |
| `DELETE /api/v1/groves/:id` | ✅ | Delete grove |
| `GET /api/v1/runtime-hosts` | ✅ | List hosts |
| `POST /api/v1/runtime-hosts` | ✅ | Register host |
| `GET /api/v1/runtime-hosts/:id` | ✅ | Get host |
| `PATCH /api/v1/runtime-hosts/:id` | ✅ | Update host |
| `DELETE /api/v1/runtime-hosts/:id` | ✅ | Deregister host |
| `GET /api/v1/templates` | ✅ | List templates |
| `POST /api/v1/templates` | ✅ | Create template |
| `GET /api/v1/templates/:id` | ✅ | Get template |
| `PATCH /api/v1/templates/:id` | ✅ | Update template |
| `DELETE /api/v1/templates/:id` | ✅ | Delete template |
| `GET /api/v1/users` | ✅ | List users |
| `GET /api/v1/users/:id` | ✅ | Get user |
| `GET /api/v1/env` | ✅ | List env vars |
| `POST /api/v1/env` | ✅ | Set env var |
| `DELETE /api/v1/env/:key` | ✅ | Delete env var |
| `GET /api/v1/secrets` | ✅ | List secrets (metadata only) |
| `POST /api/v1/secrets` | ✅ | Set secret |
| `DELETE /api/v1/secrets/:key` | ✅ | Delete secret |

#### Runtime Host API (`pkg/runtimehost/`)

| Endpoint | Handlers | Notes |
|----------|----------|-------|
| `GET /healthz` | ✅ | Health check |
| `GET /api/v1/info` | ✅ | Host info |
| `GET /api/v1/agents` | ✅ | List local agents |
| `POST /api/v1/agents` | ✅ | Create agent |
| `GET /api/v1/agents/:id` | ✅ | Get agent |
| `DELETE /api/v1/agents/:id` | ✅ | Delete agent |
| `POST /api/v1/agents/:id/start` | ✅ | Start agent |
| `POST /api/v1/agents/:id/stop` | ✅ | Stop agent |
| `POST /api/v1/agents/:id/attach` | ✅ | PTY attach via WebSocket |

### 1.3 Store Models

All store interfaces defined in `pkg/store/store.go`:

| Store | Interface | SQLite Impl | Notes |
|-------|-----------|-------------|-------|
| `AgentStore` | ✅ | ✅ | CRUD + filtering by grove/host/status |
| `GroveStore` | ✅ | ✅ | CRUD + unique remote URL |
| `RuntimeHostStore` | ✅ | ✅ | CRUD + filtering by grove |
| `TemplateStore` | ✅ | ✅ | CRUD with unique name |
| `UserStore` | ✅ | ✅ | CRUD + lookup by external ID |
| `GroveContributorStore` | ✅ | ✅ | Many-to-many grove/user |
| `EnvVarStore` | ✅ | ✅ | Scoped by user/grove/host |
| `SecretStore` | ✅ | ✅ | Scoped by user/grove/host |

### 1.4 Client Libraries

#### Hub Client (`pkg/hubclient/`)

| Service | Implementation | Notes |
|---------|----------------|-------|
| `AgentsService` | ✅ | Full CRUD |
| `GrovesService` | ✅ | Full CRUD |
| `RuntimeHostsService` | ✅ | Full CRUD |
| `TemplatesService` | ✅ | Full CRUD |
| `UsersService` | ✅ | Read-only |
| `EnvService` | ✅ | List, Set, Delete with scope |
| `SecretService` | ✅ | List, Set, Delete with scope |
| `AuthService` | ✅ | WhoAmI |

#### Host Client (`pkg/hostclient/`)

| Service | Implementation | Notes |
|---------|----------------|-------|
| `AgentsService` | ✅ | CRUD + Start/Stop/Attach |

### 1.5 Web Frontend

| Milestone | Description | Status |
|-----------|-------------|--------|
| M1 | Project Setup (Koa, Vite, TypeScript) | ✅ Complete |
| M2 | Core Shell & Routing | ✅ Complete |
| M3 | Grove Management | ❌ Not Started |
| M4 | Runtime Host Management | ❌ Not Started |
| M5 | Agent List & Overview | ❌ Not Started |
| M6 | Agent Creation Wizard | ❌ Not Started |
| M7 | Real-time Agent Monitoring | ❌ Not Started |
| M8 | Terminal Integration | ❌ Not Started |
| M9 | Dashboard & Search | ❌ Not Started |
| M10 | Settings & Preferences | ❌ Not Started |
| M11 | Notifications & Polish | ❌ Not Started |

**Current Frontend State:**
- Koa server with middleware (dev-auth, error-handler, logger, security)
- Lit SSR rendering pipeline
- Basic components: app-shell, header, nav, breadcrumb, status-badge
- Stub pages: home, agents, groves, not-found
- Shoelace component library integrated
- Vaadin router for client-side routing

### 1.6 Not Yet Implemented

| Feature | Design Doc | Notes |
|---------|------------|-------|
| OAuth Authentication | `dev-auth.md` | Only dev auth implemented |
| WebSocket Control Channel | `runtimehost-websocket.md` | ✅ **Implemented** - See Section 9 |
| PTY Relay | `runtimehost-websocket.md` | ✅ **Implemented** - CLI attach via Hub |
| NATS Integration | `web-frontend-design.md` | For real-time updates |
| SSE Snapshot+Delta | `web-frontend-design.md` | For UI state sync |
| xterm.js Terminal | `frontend-milestones.md` | M8 milestone (PTY backend ready) |
| Agent Creation Wizard | `frontend-milestones.md` | M6 milestone |
| Production Auth Flow | `hosted-architecture.md` | Session management |

---

## 2. Inconsistencies and Edge Cases

### 2.1 Design-to-Implementation Gaps

#### 2.1.1 Agent State Machine
**Design:** `hosted-architecture.md` defines agent states: `pending`, `provisioning`, `running`, `paused`, `stopping`, `stopped`, `failed`, `terminated`

**Implementation:** `pkg/store/models.go` defines: `pending`, `provisioning`, `running`, `paused`, `stopping`, `stopped`, `failed`

**Gap:** Missing `terminated` state. Need to clarify the distinction between `stopped` and `terminated` (permanent vs. restartable).

**Recommendation:** Add `terminated` state for agents that have been explicitly deleted or reached a non-recoverable end state.

#### 2.1.2 Control Channel
**Design:** `hosted-architecture.md` specifies WebSocket-based control channel for Hub-to-Host communication, essential for NAT traversal.

**Implementation:** No WebSocket handlers in `pkg/hub/` or `pkg/runtimehost/`.

**Gap:** Remote runtime hosts behind NAT cannot receive commands from Hub.

**Recommendation:** Implement control channel as high priority for multi-host deployments.

#### 2.1.3 Agent Dispatcher
**Design:** Hub API should dispatch create/start/stop requests to appropriate Runtime Host.

**Implementation:** `pkg/hub/server.go` defines `AgentDispatcher` interface with `DispatchAgentCreate` method. `cmd/server.go` implements a local dispatcher adapter.

**Gap:** Dispatcher only works for co-located Hub+Host. No remote dispatch capability.

**Recommendation:** Implement HTTP-based dispatcher for direct reachability, WebSocket for NAT traversal.

### 2.2 Edge Case Concerns

#### 2.2.1 Concurrent Agent Operations
**Risk:** Multiple clients issuing start/stop commands simultaneously.

**Current State:** No explicit locking or transaction isolation for agent state transitions.

**Recommendation:**
- Add optimistic concurrency control via version/ETag on agent records
- Implement state transition validation (e.g., can't start an already-running agent)
- Return 409 Conflict for invalid state transitions

#### 2.2.2 Grove Registration Race Conditions
**Risk:** Multiple hosts registering the same grove URL simultaneously.

**Current State:** SQLite unique constraint will reject duplicates, but error handling may be inconsistent.

**Recommendation:**
- Implement upsert semantics for grove registration
- Return existing grove ID if already registered
- Add contributor relationship atomically

#### 2.2.3 Orphaned Agents on Host Crash
**Risk:** Runtime host crashes with running agents; Hub still shows them as `running`.

**Current State:** No heartbeat or health check mechanism.

**Recommendation:**
- Implement host heartbeat (periodic ping to Hub)
- Mark agents as `unknown` after heartbeat timeout
- Provide reconciliation endpoint for host restart

#### 2.2.4 Secret Scope Resolution
**Risk:** Ambiguous precedence when same secret key exists at multiple scopes.

**Current State:** Design specifies User > Grove > RuntimeHost precedence but implementation unclear.

**Recommendation:**
- Document scope resolution order explicitly in API
- Implement `GetResolvedSecrets(userID, groveID, hostID)` method
- Return scope source in response for debugging

#### 2.2.5 Environment Variable Expansion
**Risk:** Circular references in environment variables (e.g., `A=$B`, `B=$A`).

**Current State:** No expansion logic implemented.

**Recommendation:**
- Implement expansion at injection time (agent start)
- Detect cycles and fail with clear error
- Limit expansion depth

#### 2.2.6 Template Validation
**Risk:** Invalid container image references in templates.

**Current State:** Templates store image names without validation.

**Recommendation:**
- Validate image name format on create/update
- Consider optional image pull verification
- Add template testing endpoint

#### 2.2.7 Database Connection Limits
**Risk:** SQLite connection exhaustion under load.

**Current State:** Single SQLite file with default connection handling.

**Recommendation:**
- Configure connection pool limits
- Add database health check endpoint
- Consider WAL mode for better concurrency

### 2.3 Security Considerations

#### 2.3.1 Dev Auth in Production
**Risk:** Dev auth token accepted in production.

**Current State:** `dev-auth.md` acknowledges this, but no environment-based disable.

**Recommendation:**
- Add explicit `SCION_DISABLE_DEV_AUTH=true` for production
- Log warnings when dev auth is active
- Document production auth requirements

#### 2.3.2 Secret Storage
**Risk:** Secrets stored in plaintext in SQLite.

**Current State:** Secrets stored as-is in database.

**Recommendation:**
- Implement at-rest encryption for secrets
- Consider external secret store integration (Vault, cloud KMS)
- Add secret rotation support

#### 2.3.3 API Rate Limiting
**Risk:** DoS via excessive API requests.

**Current State:** No rate limiting implemented.

**Recommendation:**
- Add per-user rate limiting
- Implement backoff for failed auth attempts
- Add circuit breaker for downstream services

---

## 3. Next Milestones

### Phase 1: Core Reliability (Foundation)

#### M1.1: Agent State Machine Hardening
- Add `terminated` state to model
- Implement state transition validation in handlers
- Add optimistic concurrency (ETag/version)
- Return proper 409 Conflict for invalid transitions

#### M1.2: Host Health Monitoring
- Implement heartbeat endpoint on Runtime Host
- Add heartbeat tracking in Hub store
- Mark agents as `unknown` on heartbeat timeout
- Add host reconciliation on reconnect

#### M1.3: Error Handling Standardization
- Define standard error response format
- Implement consistent error codes across APIs
- Add request ID for tracing
- Improve error messages for common failures

### Phase 2: Multi-Host Support

#### M2.1: Remote Agent Dispatch
- Implement HTTP-based dispatcher for reachable hosts
- Add host connectivity check before dispatch
- Handle dispatch failures with proper error responses

#### M2.2: Control Channel (WebSocket) ✅ COMPLETE
- ✅ Implement WebSocket upgrade on Hub `/api/v1/runtime-hosts/connect`
- ✅ Add host authentication on connect (HMAC)
- ✅ Implement HTTP tunneling protocol
- ✅ Handle reconnection with exponential backoff
- See `runtimehost-websocket.md` Section 9 for implementation details

#### M2.3: NAT Traversal ✅ COMPLETE
- ✅ Host initiates connection to Hub
- ✅ Hub tunnels HTTP requests through control channel
- ✅ Stream multiplexing for PTY sessions

### Phase 3: Agent Lifecycle Completion

#### M3.1: Agent Creation Flow
- Frontend wizard (template selection, config)
- API validation of agent parameters
- Workspace provisioning (git clone/worktree)
- Container image pull verification

#### M3.2: Agent Execution
- Container start with environment injection
- Secret resolution and injection
- PTY allocation and management
- Output streaming setup

#### M3.3: PTY Relay Implementation ✅ COMPLETE
- ✅ WebSocket endpoint for terminal attach (`/api/v1/agents/{id}/pty`)
- ✅ Bidirectional I/O relay via control channel streams
- ⚠️ Window resize handling (events captured, tmux resize pending)
- ✅ Disconnection handling (graceful close)
- See `runtimehost-websocket.md` Section 9 for implementation details

#### M3.4: Agent Monitoring
- Real-time status updates (SSE or WebSocket)
- Resource usage metrics (CPU, memory)
- Log streaming
- Event history

### Phase 4: User Experience

#### M4.1: Web Frontend Grove Management (M3)
- Grove list with status indicators
- Grove registration flow
- Contributor management
- Grove settings

#### M4.2: Web Frontend Agent Views (M5-M6)
- Agent list with filtering/sorting
- Agent detail view with status
- Agent creation wizard
- Start/stop controls

#### M4.3: Terminal Integration (M8)
- xterm.js component
- WebSocket connection to PTY relay
- Copy/paste support
- Session persistence

#### M4.4: Real-time Updates (M7)
- SSE connection for state changes
- Snapshot + delta protocol
- Optimistic UI updates
- Offline indicator

### Phase 5: Production Readiness

#### M5.1: Authentication
- OAuth provider integration
- Session management
- Token refresh
- Logout and session invalidation

#### M5.2: Security Hardening
- Secret encryption at rest
- Disable dev auth in production
- Rate limiting
- Audit logging

#### M5.3: Observability
- Structured logging
- Metrics export (Prometheus)
- Distributed tracing
- Health dashboard

#### M5.4: Deployment
- Container images
- Kubernetes manifests
- Configuration management
- Backup/restore procedures

---

## 4. Recommended Immediate Actions

1. **Agent State Transitions** - Add validation to prevent invalid state changes (e.g., starting already-running agent).

2. **Host Heartbeat** - Implement basic heartbeat to detect host disconnection and stale agent states.

3. **Error Response Consistency** - Standardize error format across all API endpoints.

4. **Frontend M3** - Complete Grove Management to enable basic registration workflow.

5. **PTY Attach Handler** - Implement actual PTY relay to enable agent interaction.

These five items address the most critical gaps between "system that compiles" and "system that's usable for agent lifecycle management."

---

## 5. Architecture Validation Notes

### What's Working Well

1. **Grove-Centric Design** - The decision to register groves (projects) rather than hosts simplifies the mental model and matches developer workflows.

2. **Store Abstraction** - Clean separation between store interface and SQLite implementation allows future database swaps.

3. **Client Libraries** - Well-structured clients with service pattern makes CLI and frontend development straightforward.

4. **Dev Auth** - Practical approach for local development without auth complexity.

5. **Middleware Architecture** - Both Go servers and Koa frontend use composable middleware.

### Areas Needing Attention

1. **Real-time Updates** - The design mentions NATS and SSE but neither is implemented. This is critical for responsive UI.

2. **Agent Lifecycle Events** - No event sourcing or history. Consider adding an events table for debugging.

3. **Multi-tenancy** - User scoping exists but enforcement in queries needs review.

4. **Testing** - Testing walkthroughs exist but automated test coverage needs expansion.
