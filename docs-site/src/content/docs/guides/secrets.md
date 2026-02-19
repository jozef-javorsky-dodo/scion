---
title: Secret Management
description: Managing secrets and sensitive credentials via the Scion Hub.
---

Scion provides a typed secret management system for securely storing and projecting sensitive data (API keys, credentials, certificates) into agent containers. Secrets are managed centrally through the Hub and resolved at agent startup.

## Secrets Backend (Required)

Scion requires a production secrets backend to store secret values. The recommended backend is **GCP Secret Manager**.

:::caution[No Plaintext Storage]
The Hub does not store secret values in its database. Attempting to create or update secrets without a configured backend (e.g., using the default `local` backend) will return an error. You must configure GCP Secret Manager to use secret management features.
:::

### Configuring GCP Secret Manager

Set the backend in your `settings.yaml`:

```yaml
server:
  secrets:
    backend: gcpsm
    gcp_project_id: "my-gcp-project"
    gcp_credentials: "/path/to/service-account.json"  # Optional if using ADC
```

Or via environment variables:

```bash
export SCION_SERVER_SECRETS_BACKEND=gcpsm
export SCION_SERVER_SECRETS_GCP_PROJECT_ID=my-gcp-project
export SCION_SERVER_SECRETS_GCP_CREDENTIALS=/path/to/service-account.json
```

When GCP Secret Manager is configured, Scion uses a **hybrid storage** model:
- **Metadata** (name, type, scope) is stored in the Hub database.
- **Secret values** are stored in GCP Secret Manager with automatic versioning.

## Secret Types

Secrets are typed to control how they are projected into agent containers:

| Type | Projection | Description |
| :--- | :--- | :--- |
| `environment` | Environment variable | Injected as an env var (default). |
| `variable` | `~/.scion/secrets.json` | Written to a JSON file for programmatic access. |
| `file` | Filesystem path | Written to a specific file path (e.g., TLS certs). Max 64 KiB. |

## Secret Scopes

Secrets are scoped to control their visibility. When an agent starts, secrets are resolved from all applicable scopes and merged, with higher-priority scopes overriding lower ones.

| Scope | Priority | Description |
| :--- | :--- | :--- |
| `user` | Lowest | Personal secrets for a specific user. Applied to all agents owned by that user. |
| `grove` | Medium | Project-level secrets. Applied to all agents in the grove. |
| `runtime_broker` | Highest | Infrastructure-level secrets. Applied to all agents on a specific broker. |

**Override example:** If a user defines `API_KEY=user-key` and the grove defines `API_KEY=grove-key`, agents in that grove will receive `grove-key`.

## Managing Secrets

### Via the CLI

```bash
# Set a user-scoped secret
scion hub secret set API_KEY --value "sk-live-..."

# Set a grove-scoped secret
scion hub secret set DB_PASSWORD --scope grove --scope-id <grove-id> --value "..."

# Set a file-type secret (e.g., TLS certificate)
scion hub secret set TLS_CERT --type file --target /etc/ssl/cert.pem --value "$(cat cert.pem)"

# List secrets (metadata only, values are never exposed)
scion hub secret list

# Delete a secret
scion hub secret delete API_KEY
```

### Via the API

```bash
# Set a secret
curl -X PUT https://hub.example.com/api/v1/secrets/API_KEY \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "sk-live-...", "type": "environment", "scope": "grove", "scopeId": "grove-123"}'

# List secrets for a grove
curl https://hub.example.com/api/v1/groves/grove-123/secrets \
  -H "Authorization: Bearer $TOKEN"
```

## How Secrets Reach Agents

When an agent is dispatched to a Runtime Broker:

1. The Hub resolves all applicable secrets (merging user, grove, and broker scopes).
2. Resolved secrets are included in the `CreateAgent` command sent to the broker over the TLS-secured control channel.
3. The broker projects secrets into the agent container based on their type:
   - `environment` secrets become environment variables.
   - `variable` secrets are written to `~/.scion/secrets.json`.
   - `file` secrets are written to the specified target path.
4. When the agent is deleted, all projected secrets are purged.

Brokers never persist agent secrets to disk. Secrets exist only in the agent's container memory for the duration of its lifecycle.

## Security Considerations

- **Values are never returned** by the Hub API. Only metadata (name, type, scope, version) is exposed.
- **Transport security**: Secrets are transmitted over TLS between the Hub and Runtime Brokers.
- **API keys** (used for programmatic Hub access) are stored as SHA-256 hashes only -- the original key value cannot be recovered.
- **Broker join tokens** are hashed before storage and expire after one hour.
- **Broker shared secrets** (for HMAC authentication) are stored as binary blobs in the Hub database and are established during the broker registration flow.

For a detailed overview of the security architecture, see the [Security Architecture Reference](/reference/security).
