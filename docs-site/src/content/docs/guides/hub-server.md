---
title: Setting up the Scion Hub
description: Installation and configuration of the Scion Hub (State Server).
---

The **Scion Hub** is the central brain of a hosted Scion architecture. It maintains the state of all agents, groves, and runtime brokers, and provides the API used by the CLI and Web Dashboard.

## Core Responsibilities

- **Central Registry**: Maintains a record of all Groves (projects), Runtime Brokers, and Templates.
- **Identity Provider**: Manages user authentication (OAuth) and issues scoped JWTs for Agents and Brokers.
- **State Store**: Tracks the lifecycle, status, and metadata of all agents.
- **Task Dispatcher**: Routes agent commands from the CLI or Dashboard to the correct Runtime Broker via persistent WebSocket tunnels.

## Running the Hub

The Hub is part of the main `scion` binary. You can start it using the `server` command:

```bash
# Start the Hub and a local Runtime Broker
scion server

# Start ONLY the Hub
scion server --hub
```

### Hub vs. Broker Processes
While they can run in the same process (the default for `scion server`), they serve distinct roles:
- **The Hub** is the stateless control plane. It should be accessible via a public or internal URL.
- **The Broker** is the execution host. It registers with a Hub and executes agents. Brokers can run behind NAT or firewalls, as they establish outbound connections to the Hub.

## Configuration

The Hub is configured via the `server` section in `~/.scion/settings.yaml`.

### Basic Example
```yaml
schema_version: "1"
server:
  log_level: info
  hub:
    port: 9810
    host: 0.0.0.0
  database:
    driver: sqlite
    url: hub.db
  auth:
    dev_mode: true
```

See the [Server Configuration Reference](/reference/server-config) for all available fields.

## Authentication

The Hub supports multiple authentication modes to balance ease of development with production security.

### OAuth 2.0 (Production)
Scion supports Google and GitHub as identity providers. Configuration requires creating OAuth Apps in the respective provider consoles.
See the [Authentication Guide](/guides/auth) for detailed setup instructions.

### Dev Auth (Local Development)
For local testing, the Hub can auto-generate a development token:
```yaml
server:
  auth:
    dev_mode: true
```
The token is written to `~/.scion/dev-token` on startup. The CLI and Web Dashboard automatically detect this token when running on the same machine.

### API Keys (Programmatic)
The Hub supports long-lived API keys for CI/CD or other programmatic integrations.

## Persistence

The Hub requires a database to store its state.

### SQLite (Default)
Ideal for local development or single-node deployments. The database is a single file.
```yaml
server:
  database:
    driver: sqlite
    url: /path/to/your/hub.db
```

### PostgreSQL (Production)
Recommended for high-availability or multi-node deployments.
```yaml
server:
  database:
    driver: postgres
    url: "postgres://user:password@localhost:5432/scion?sslmode=disable"
```

## Storage Backends

The Hub stores agent templates and other artifacts.

- **Local File System**: Default. Stores files in `~/.scion/storage`.
- **Google Cloud Storage (GCS)**: Recommended for cloud deployments. Set the `SCION_SERVER_STORAGE_BUCKET` environment variable.

## Deployment

### Docker / Podman
The Hub is available as a Docker image.

```bash
docker run -p 9810:9810 \
  -e SCION_SERVER_HUB_PORT=9810 \
  -v ~/.scion:/root/.scion \
  ghcr.io/ptone/scion-hub:latest
```

(Or use `podman run` with the same arguments).

### Cloud Run (GCP)
The Hub is designed to be stateless and is highly compatible with Google Cloud Run. 
- Use **Cloud SQL** (PostgreSQL) for the database.
- Use **Cloud Storage** for template persistence.
- Connect the Hub to Cloud SQL using the Cloud SQL Auth Proxy or a VPC connector.

## Observability

The Hub supports structured logging and can forward its internal logs and traces to an OpenTelemetry-compatible backend (like Google Cloud Logging/Trace).

To enable log forwarding, set `SCION_OTEL_LOG_ENABLED=true` and `SCION_OTEL_ENDPOINT`. See the [Observability Guide](/guides/observability) for full details on centralizing system logs and agent metrics.

## Monitoring

The Hub exposes health check endpoints:
- `/healthz`: Basic liveness check.
- `/readyz`: Readiness check (verifies database connectivity).

Logs are output to `stdout` in either `text` (default) or `json` format, suitable for collection by systems like Fluentd, Cloud Logging, or Prometheus.
