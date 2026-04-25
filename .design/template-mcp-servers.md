# Universal MCP Server Configuration in Scion Templates

## Motivation

Today, configuring MCP servers for a scion template requires duplicating harness-specific configuration across every harness-config variant. The `web-dev` template illustrates this clearly: to give agents access to a Chrome DevTools MCP server, the template author must:

1. Define a `chromium` service in `scion-agent.yaml` (harness-agnostic)
2. Add `mcpServers.chrome-devtools` to `harness-configs/claude-web/home/.claude.json` (Claude-specific JSON)
3. Add `mcpServers.chrome-devtools` to `harness-configs/gemini-web/home/.gemini/settings.json` (Gemini-specific JSON)
4. Know that OpenCode and Codex have different (or no) MCP configuration mechanisms

The MCP server definition itself is identical across harnesses — the same `command`, `args`, and `env` — but must be expressed in each harness's native config format and written to the correct file path. This creates several problems:

- **Duplication**: The same logical MCP server is defined N times for N harness variants.
- **Drift**: A template author updates the Claude config but forgets the Gemini one. The harness variants silently diverge.
- **Expertise barrier**: Template authors need to understand each harness's native config format, file locations, and JSON/YAML structure to add an MCP server.
- **Incomplete coverage**: Harnesses added later (or community harness-configs) don't automatically inherit MCP servers defined in the template.
- **No validation**: MCP server definitions embedded in raw JSON home files bypass scion's config validation and schema enforcement.

### What We Want

A single, harness-agnostic `mcp_servers` block in `scion-agent.yaml` that:

1. Captures the full MCP server specification once, at the template level
2. Is validated against a schema at config load time
3. Is provisioned into each harness's native configuration format during agent creation
4. Works with the existing `services` block for MCP servers that need sidecar processes
5. Supports all common MCP transport types (stdio, SSE, streamable HTTP)

## Proposed Schema

### `scion-agent.yaml` Extension

```yaml
# Existing fields
default_harness_config: claude-web

# Existing: sidecar process definitions
services:
  - name: chromium
    command: ["chromium", "--headless", "--no-sandbox", "--remote-debugging-port=9222"]
    restart: always
    ready_check:
      type: tcp
      target: "localhost:9222"
      timeout: "10s"

# NEW: Universal MCP server configuration
mcp_servers:
  chrome-devtools:
    transport: stdio
    command: chrome-devtools-mcp
    args: ["--headless", "--browser-url", "http://localhost:9222"]
    env:
      DEBUG: "false"

  filesystem:
    transport: stdio
    command: npx
    args: ["-y", "@anthropic/mcp-filesystem", "/workspace"]

  remote-api:
    transport: sse
    url: "http://localhost:8080/mcp/sse"
    headers:
      Authorization: "Bearer ${MCP_API_TOKEN}"

  streaming-service:
    transport: streamable-http
    url: "http://localhost:9090/mcp"
    headers:
      X-API-Key: "${SERVICE_API_KEY}"
```

### MCP Server Configuration Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `transport` | enum | Yes | Transport protocol: `stdio`, `sse`, or `streamable-http` |
| `command` | string | stdio only | Executable to launch |
| `args` | []string | No | Command-line arguments |
| `env` | map[string]string | No | Environment variables passed to the MCP process |
| `url` | string | sse/http only | Server endpoint URL |
| `headers` | map[string]string | No | HTTP headers (sse/http only) |
| `scope` | enum | No | Where to register: `global` (default) or `project` |

#### Transport Types

**`stdio`** — The MCP server runs as a child process of the harness. The harness launches the command and communicates via stdin/stdout JSON-RPC.

- Requires: `command`
- Optional: `args`, `env`
- This is the most common transport for locally-installed MCP servers (e.g., filesystem, git, browser tools).

**`sse`** — The MCP server is an HTTP service that uses Server-Sent Events for server-to-client messages and HTTP POST for client-to-server messages.

- Requires: `url`
- Optional: `headers`
- Suitable for MCP servers running as sidecar services (defined in `services`) or external endpoints.

**`streamable-http`** — The newer HTTP-based transport where all communication uses HTTP POST with optional SSE streaming for server responses.

- Requires: `url`
- Optional: `headers`
- The emerging standard for HTTP-based MCP servers.

#### Scope

- **`global`** (default): The MCP server is registered at the harness's global/user-level configuration. It is available to all projects within the agent session.
- **`project`**: The MCP server is registered only for the agent's workspace project. Useful when the server is workspace-specific (e.g., a project-scoped database tool).

Not all harnesses distinguish between global and project scope. For harnesses that do not, `project`-scoped servers are treated as `global`.

#### Environment Variable Interpolation

String values in `url`, `headers`, `args`, and `env` support `${VAR_NAME}` interpolation from the agent's resolved environment. This allows MCP server configs to reference:

- Secrets injected via `scion-agent.yaml` `secrets` definitions
- Environment variables set via `env` in `scion-agent.yaml` or harness-config
- Runtime-provided variables (e.g., `${AGENT_WORKSPACE}`)

Unresolvable variables are left as literal strings (not an error), allowing harness-native variable expansion to handle them at runtime.

### Go Type Definitions

```go
// pkg/api/types.go

type MCPTransport string

const (
    MCPTransportStdio          MCPTransport = "stdio"
    MCPTransportSSE            MCPTransport = "sse"
    MCPTransportStreamableHTTP MCPTransport = "streamable-http"
)

type MCPScope string

const (
    MCPScopeGlobal  MCPScope = "global"
    MCPScopeProject MCPScope = "project"
)

type MCPServerConfig struct {
    Transport MCPTransport      `json:"transport" yaml:"transport"`
    Command   string            `json:"command,omitempty" yaml:"command,omitempty"`
    Args      []string          `json:"args,omitempty" yaml:"args,omitempty"`
    Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
    URL       string            `json:"url,omitempty" yaml:"url,omitempty"`
    Headers   map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
    Scope     MCPScope          `json:"scope,omitempty" yaml:"scope,omitempty"`
}
```

Add to `ScionConfig`:

```go
type ScionConfig struct {
    // ... existing fields ...
    Services   []ServiceSpec              `json:"services,omitempty" yaml:"services,omitempty"`
    MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty" yaml:"mcp_servers,omitempty"` // NEW
    // ... existing fields ...
}
```

### Validation Rules

Added to `ValidateScionConfig()` in `pkg/config/validate.go`:

1. `transport` is required and must be one of `stdio`, `sse`, `streamable-http`
2. `command` is required when `transport` is `stdio`; disallowed otherwise
3. `args` is only valid when `transport` is `stdio`
4. `url` is required when `transport` is `sse` or `streamable-http`; disallowed for `stdio`
5. `headers` is only valid when `transport` is `sse` or `streamable-http`
6. `scope` defaults to `global` if omitted; must be one of `global`, `project`
7. MCP server names follow the same slug rules as service names (alphanumeric, hyphens, underscores)
8. MCP server names must not conflict with service names (warning, not error — they are different namespaces but confusion is likely)

### Config Merging

`MCPServers` follows the same merge semantics as other map fields in `MergeScionConfig()`: entries from higher-priority layers override entries from lower-priority layers by key. An entry can be explicitly removed by setting it to a zero-value marker (TBD: `null` in YAML, or an `enabled: false` field).

Harness-config `config.yaml` overrides use the **same universal `MCPServerConfig` format**, not the harness's native config format. Merging happens entirely at the scion-schema level before any translation to native format. The provisioning layer translates the final merged map once. This keeps translation logic in one place and means harness-config scripts only receive an already-merged universal config.

## Relationship to Services

MCP servers and services are related but distinct concepts:

| Concern | `services` | `mcp_servers` |
|---|---|---|
| **What it defines** | A sidecar process to run inside the container | An MCP server the harness should connect to |
| **Lifecycle** | Managed by sciontool (start, health check, restart) | Managed by the harness (or sciontool for stdio) |
| **Where it runs** | Inside the container, supervised by sciontool | Inside the container (stdio) or external (sse/http) |
| **Config target** | `scion-services.yaml` consumed by sciontool | Harness-native config files (`.claude.json`, `.gemini/settings.json`, etc.) |

A common pattern combines both: a `service` runs a dependency (e.g., Chromium), and an `mcp_server` configures the harness to connect to it via an MCP bridge:

```yaml
services:
  - name: chromium
    command: ["chromium", "--headless", "--remote-debugging-port=9222"]
    restart: always
    ready_check:
      type: tcp
      target: "localhost:9222"
      timeout: "10s"

mcp_servers:
  chrome-devtools:
    transport: stdio
    command: chrome-devtools-mcp
    args: ["--headless", "--browser-url", "http://localhost:9222"]
```

There is no automatic linkage between a service and an MCP server — they are independently defined and provisioned. A future enhancement could add a `depends_on` field to `MCPServerConfig` referencing a service name, ensuring the service is healthy before the MCP server is started. This is deferred.

## Harness Provisioning: How MCP Configs Reach Native Formats

Each harness has its own native configuration format and file path for MCP servers. The provisioning layer must translate the universal `MCPServerConfig` into the harness's expected structure.

### Current Native Formats

**Claude Code** (`.claude.json` or `.claude.json` projects section):
```json
{
  "mcpServers": {
    "server-name": {
      "type": "stdio",
      "command": "executable",
      "args": ["--flag", "value"],
      "env": {"KEY": "value"}
    }
  }
}
```
- Scope `global` → top-level `mcpServers`
- Scope `project` → `projects[workspace_path].mcpServers`
- Supports: `stdio` natively. SSE/HTTP support varies by Claude Code version.

**Gemini CLI** (`.gemini/settings.json`):
```json
{
  "mcpServers": {
    "server-name": {
      "type": "stdio",
      "command": "executable",
      "args": ["--flag", "value"],
      "env": {"KEY": "value"}
    }
  }
}
```
- Scope: `global` only (Gemini does not distinguish project-scoped MCP)
- Supports: `stdio` natively. SSE/HTTP support varies.

**Codex** (`config.toml` or equivalent):
- MCP support: Not currently documented. May need a shim or skip.

**OpenCode** (`opencode.json` or equivalent):
- MCP support: TBD. Config format differs from Claude/Gemini.

### Provisioning Flow

```
scion-agent.yaml              (template author defines mcp_servers)
       │
       ▼
ScionConfig.MCPServers         (parsed and validated)
       │
       ▼
provision.go                   (during agent creation)
       │
       ├──► harness.ProvisionMCPServers(agentHome, workspace, mcpServers)
       │         │
       │         ├── Claude:  merge into .claude.json mcpServers
       │         ├── Gemini:  merge into .gemini/settings.json mcpServers
       │         ├── Codex:   write to codex-specific location (TBD)
       │         └── Generic: no-op or best-effort
       │
       └──► (existing) write scion-services.yaml for sidecar services
```

### Harness Interface Extension

A new method on the `Harness` interface (or a new optional interface):

```go
// MCPProvisioner is implemented by harnesses that support MCP server configuration.
type MCPProvisioner interface {
    ProvisionMCPServers(agentHome string, workspacePath string, servers map[string]MCPServerConfig) error
}
```

Using an optional interface (rather than extending the base `Harness` interface) allows harnesses to opt-in. Harnesses that don't implement `MCPProvisioner` simply skip MCP provisioning — the template's `mcp_servers` are ignored for that harness. This should emit a warning during provisioning.

### Capability Advertisement

Extend `HarnessAdvancedCapabilities` to declare MCP support:

```go
type HarnessAdvancedCapabilities struct {
    // ... existing fields ...
    MCP HarnessMCPCapabilities `json:"mcp"`
}

type HarnessMCPCapabilities struct {
    Stdio          CapabilityField `json:"stdio"`
    SSE            CapabilityField `json:"sse"`
    StreamableHTTP CapabilityField `json:"streamable_http"`
    ProjectScope   CapabilityField `json:"project_scope"`
}
```

This allows the UI and CLI to show which MCP transports a harness supports, and to warn when a template defines MCP servers using transports the selected harness cannot handle.

## Integration with Decoupled Harness Implementation

> **Note:** Implementation of MCP provisioning is deferred until the completion of the [Decoupled Harness Implementation](decoupled-harness-implementation.md) refactor.

The decoupled harness design introduces `provision.py` scripts that handle harness-specific file manipulation. MCP server provisioning is a natural fit for this model:

### New Script Command: `provision-mcp`

```bash
echo '$MANIFEST' | python3 provision.py provision-mcp
```

Manifest:
```json
{
  "command": "provision-mcp",
  "agent_home": "/path/to/home",
  "workspace_path": "/workspace",
  "mcp_servers": {
    "chrome-devtools": {
      "transport": "stdio",
      "command": "chrome-devtools-mcp",
      "args": ["--headless", "--browser-url", "http://localhost:9222"],
      "env": {},
      "scope": "global"
    }
  }
}
```

The script translates universal `MCPServerConfig` entries into the harness's native format and writes them to the appropriate file(s). For most harnesses, this is a JSON merge operation — exactly the kind of file manipulation that `provision.py` excels at.

### Extended `config.yaml` for Declarative MCP Mapping

For harnesses whose MCP config format is a straightforward mapping (which is most of them), the harness `config.yaml` can declare the mapping declaratively, eliminating the need for a custom script:

```yaml
# In harness-config config.yaml
mcp:
  config_file: .claude.json                    # File to write MCP config into
  config_path: mcpServers                      # JSON path for global-scope servers
  project_config_path: projects.{workspace}.mcpServers  # JSON path for project-scope
  transport_field: type                         # Field name for transport type
  transport_map:                                # Map scion transport names to native names
    stdio: stdio
    sse: sse
    streamable-http: streamable-http
```

The `ScriptHarness` Go implementation can handle the common case (JSON merge at a known path) without invoking `provision.py` at all. Only harnesses with exotic config formats need a custom `provision-mcp` script handler.

### Migration Path

1. **Phase 1 (now):** Define the `MCPServerConfig` type and add `mcp_servers` to `ScionConfig`. Add validation. No provisioning yet — the field is parsed and stored but not acted upon.
2. **Phase 2 (decoupled harness):** Implement `provision-mcp` in the `ScriptHarness` and add the `MCPProvisioner` interface. Migrate existing harnesses.
3. **Phase 3 (cleanup):** Remove inline MCP config from harness-config home files in templates. Templates use `mcp_servers` exclusively.

## Impact on Existing Templates

### `web-dev` Template: Before and After

**Before** — MCP servers defined in each harness-config's home files:

```
.scion/templates/web-dev/
  scion-agent.yaml                        # services only
  harness-configs/
    claude-web/
      home/.claude.json                   # mcpServers: chrome-devtools
    gemini-web/
      home/.gemini/settings.json          # mcpServers: chrome-devtools
    opencode/
      home/.config/opencode/opencode.json # no MCP config (no support)
```

**After** — MCP servers defined once in `scion-agent.yaml`:

```
.scion/templates/web-dev/
  scion-agent.yaml                        # services + mcp_servers
  harness-configs/
    claude-web/
      home/.claude.json                   # no mcpServers (provisioned from template)
    gemini-web/
      home/.gemini/settings.json          # no mcpServers (provisioned from template)
    opencode/
      home/.config/opencode/opencode.json # no MCP config (harness warns)
```

Updated `scion-agent.yaml`:
```yaml
default_harness_config: claude-web

services:
  - name: chromium
    command: ["chromium", "--headless", "--no-sandbox", "--remote-debugging-port=9222", "--remote-debugging-address=0.0.0.0"]
    restart: always
    ready_check:
      type: tcp
      target: "localhost:9222"
      timeout: "10s"

mcp_servers:
  chrome-devtools:
    transport: stdio
    command: chrome-devtools-mcp
    args: ["--headless", "--browser-url", "http://localhost:9222"]
```

## Open Questions

### Q1: Harness-Config Level MCP Overrides

Should harness-configs be able to override or extend the template-level `mcp_servers`? For example, a harness-config might need to add harness-specific arguments or environment variables to an MCP server.

**Options:**
- **A**: No overrides. Template `mcp_servers` are authoritative. Harness-specific adjustments go in `provision.py`.
- **B**: Harness-config `config.yaml` can define its own `mcp_servers` block that merges with (and overrides) the template's. Follows existing config merge precedence.
- **C**: Harness-config can only *remove* servers (via an exclude list), not add or modify.

**Recommendation:** Option B. It follows the existing merge pattern where harness-config specializes template-level config, and it keeps the door open for harness-specific MCP arguments without requiring a script.

### Q2: MCP Server Dependencies on Services

Should there be a formal mechanism to declare that an MCP server depends on a service being healthy before it starts?

**Options:**
- **A**: No formal mechanism. Template authors document the dependency. The service `ready_check` ensures it's running; the MCP server may fail and retry.
- **B**: Add `depends_on: <service-name>` to `MCPServerConfig`. Provisioning validates the referenced service exists. Runtime could delay MCP server registration until the service is healthy (if supported by the harness).

**Decision:** No formal mechanism. Template authors document the dependency. The service `ready_check` ensures the sidecar is running before the agent starts; harnesses handle MCP connection errors gracefully.

### Q3: MCP Servers That Are Also Services

Some MCP servers run as long-lived HTTP services (SSE or streamable-http transport). Should these be implicitly added to the `services` list for lifecycle management by sciontool?

**Options:**
- **A**: Keep services and MCP servers fully separate. If an MCP server needs lifecycle management, define it in both places.
- **B**: SSE/HTTP MCP servers are automatically registered as sciontool services with sensible defaults (restart: on-failure, ready_check based on URL).
- **C**: Add an optional `service` sub-block to `MCPServerConfig` for SSE/HTTP servers that opts into sciontool management.

**Decision:** Keep them separate. SSE/HTTP MCP servers that need lifecycle management are defined in the `services` block explicitly. No implicit registration.

### Q4: Handling Unsupported Transports

When a template defines an MCP server with a transport the active harness doesn't support, what should happen?

**Options:**
- **A**: Warn at provisioning time. The MCP server is silently skipped.
- **B**: Error at provisioning time. Fail the agent creation.
- **C**: Warn at provisioning time but still create the agent. Log which MCP servers were skipped and why.

**Decision:** Delegated to the harness `provision.py` script. Since MCP provisioning runs as part of the decoupled harness configuration (which may occur after agent creation), agent creation failure is not an option. The `provision.py` script is responsible for handling unsupported transports — it can warn, skip, or error at its discretion. This is a natural consequence of the decoupled harness execution model.

### Q5: Runtime MCP Server Management

Should scion provide any runtime management of MCP servers (start, stop, health check) beyond what the harness natively provides?

**Decision:** Config only. Scion writes the MCP server configuration into the harness's native format and stops there. The harness owns MCP server lifecycle. Sidecar process management goes through the `services` block.

## Summary

This design introduces a universal `mcp_servers` block in `scion-agent.yaml` that lets template authors define MCP server configurations once, in a harness-agnostic format. The provisioning layer translates these definitions into each harness's native config format. The schema supports stdio, SSE, and streamable-http transports, covers the common configuration surface area (command, args, env, URL, headers, scope), and integrates cleanly with the existing `services` block for sidecar dependencies.

Implementation is deferred until the [decoupled harness implementation](decoupled-harness-implementation.md) is complete, at which point MCP provisioning becomes a natural `provision-mcp` command in `provision.py` scripts — or, for the common case, a declarative mapping in `config.yaml`.
