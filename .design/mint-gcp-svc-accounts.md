# Design: Hub-Minted GCP Service Accounts

**Status:** Draft  
**Date:** 2026-04-03  
**Related:** [sciontool-gcp-identity.md](hosted/sciontool-gcp-identity.md), [sciontool-gcp-identity-pt2.md](hosted/sciontool-gcp-identity-pt2.md)

## Problem

Today, grove administrators must pre-create GCP service accounts in their own GCP projects and register them with the Hub via `scion grove service-accounts add`. The Hub then verifies it can impersonate the SA (requiring the user to have already granted `roles/iam.serviceAccountTokenCreator` to the Hub's identity on that SA).

This workflow has friction:
1. Users need GCP IAM expertise to create SAs and configure cross-project impersonation.
2. Each user must own a GCP project to host their service accounts.
3. The permission grant is error-prone and hard to debug when it fails.

## Proposal

Allow the Hub to **mint** (create) new GCP service accounts in the Hub's own GCP project on behalf of users. These minted SAs:
- Are created with **no IAM permissions** — they are permissionless by default.
- Are automatically configured so the Hub SA has `roles/iam.serviceAccountTokenCreator` on them.
- Are stored and associated with groves using the existing `GCPServiceAccount` model.
- Can later be granted IAM permissions on the user's own projects by the user (outside of Scion).

This gives users a zero-setup path to GCP identity for their agents while preserving the existing BYOSA (bring-your-own-service-account) flow.

## Architecture

### Hub Prerequisites

The Hub's operating service account needs two IAM roles on the Hub's GCP project:

| Role | Purpose |
|------|---------|
| `roles/iam.serviceAccountCreator` | Create and delete service accounts in the Hub project |
| `roles/iam.serviceAccountTokenCreator` | Generate tokens for minted SAs (already required for BYOSA flow) |

The Hub must also know its own GCP project ID. This is either:
- Auto-detected from the metadata server (when running on GCE/Cloud Run).
- Configured explicitly via a new `GCPProjectID` field on `ServerConfig`.

### New API Endpoint

```
POST /api/v1/groves/{groveId}/gcp-service-accounts/mint
```

**Request Body:**
```json
{
  "display_name": "my-data-pipeline",       // optional, used for SA display name
  "description": "Agent SA for data work"   // optional, SA description
}
```

**Response:** Standard `GCPServiceAccount` object with additional fields:

```json
{
  "id": "uuid",
  "email": "scion-a1b2c3d4@hub-project.iam.gserviceaccount.com",
  "project_id": "hub-project",
  "display_name": "my-data-pipeline",
  "scope": "grove",
  "scope_id": "grove-uuid",
  "verified": true,
  "verification_status": "verified",
  "managed": true,
  "created_by": "user@example.com",
  "created_at": "2026-04-03T..."
}
```

### Service Account Naming

GCP SA account IDs must be 6-30 chars, `[a-z][a-z0-9-]*[a-z0-9]`. Proposed scheme:

```
scion-{8-char-random-hex}
```

Example: `scion-a1b2c3d4@hub-project.iam.gserviceaccount.com`

The display name is set from the request (or defaults to `"Scion agent ({grove-slug})"`) and the description includes the grove ID for traceability.

### Data Model Changes

Add a `managed` boolean to `GCPServiceAccount`:

```go
type GCPServiceAccount struct {
    // ... existing fields ...
    Managed   bool   `json:"managed"`              // true = created by Hub, false = BYOSA
    ManagedBy string `json:"managed_by,omitempty"` // Hub instance ID that created it
}
```

```sql
ALTER TABLE gcp_service_accounts ADD COLUMN managed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE gcp_service_accounts ADD COLUMN managed_by TEXT NOT NULL DEFAULT '';
```

This flag controls:
- Whether the Hub will **delete the underlying GCP SA** when the registration is removed (vs. just unlinking for BYOSA).
- Display in the UI (badge/label distinguishing "Hub-managed" vs. "User-provided").
- Preventing users from re-registering a Hub-minted SA email as a BYOSA (would conflict).

### Implementation Components

#### 1. GCP IAM Admin Client (`pkg/hub/gcp_iam_admin.go`)

New interface wrapping the GCP IAM Admin API (`google.golang.org/api/iam/v1`):

```go
type GCPServiceAccountAdmin interface {
    CreateServiceAccount(ctx context.Context, projectID, accountID, displayName, description string) (email string, uniqueID string, err error)
    DeleteServiceAccount(ctx context.Context, email string) error
    SetIAMPolicy(ctx context.Context, saEmail string, hubEmail string, role string) error
}
```

- `CreateServiceAccount` calls `iam.projects.serviceAccounts.create`.
- After creation, `SetIAMPolicy` grants `roles/iam.serviceAccountTokenCreator` to the Hub SA on the new SA.
- `DeleteServiceAccount` calls `iam.projects.serviceAccounts.delete`.

#### 2. Mint Handler (`pkg/hub/handlers_gcp_identity.go`)

New handler method on `Server`:

```go
func (s *Server) mintGCPServiceAccount(w http.ResponseWriter, r *http.Request) {
    // 1. Authorize: require grove admin or hub admin
    // 2. Validate request
    // 3. Generate account ID (scion-{random})
    // 4. Call GCPServiceAccountAdmin.CreateServiceAccount()
    // 5. Call GCPServiceAccountAdmin.SetIAMPolicy() to grant token creator
    // 6. Create GCPServiceAccount record with managed=true, verified=true
    // 7. Audit log the creation
    // 8. Return response
}
```

#### 3. Enhanced Delete Handler

When deleting a managed SA, also delete the underlying GCP service account:

```go
func (s *Server) deleteGCPServiceAccount(...) {
    sa, _ := s.store.GetGCPServiceAccount(ctx, id)
    // ... existing authz checks ...
    if sa.Managed {
        s.gcpSAAdmin.DeleteServiceAccount(ctx, sa.Email)
    }
    s.store.DeleteGCPServiceAccount(ctx, id)
}
```

#### 4. CLI Command

```bash
scion grove service-accounts mint                          # Mint with defaults
scion grove service-accounts mint --name "my-pipeline"     # Custom display name
```

#### 5. ServerConfig Addition

```go
type ServerConfig struct {
    // ... existing fields ...
    GCPProjectID string // Project ID for minting SAs (auto-detected if empty)
}
```

### Quota & Limits

GCP imposes a default limit of **100 service accounts per project**. At scale this becomes a concern.

**Mitigations:**
- Track count of minted SAs per grove (enforce a per-grove cap, e.g., 5).
- Enforce a global cap on total minted SAs (configurable on `ServerConfig`).
- Surface the current count on the Hub admin dashboard.
- GCP quota can be raised to 1000+ via support request if needed.

### Lifecycle & Cleanup

Minted SAs should be cleaned up when:
- Explicitly deleted by the user via `scion grove service-accounts remove`.
- A grove is hard-deleted (cascade delete managed SAs).
- A scheduled maintenance job detects orphaned SAs (no grove association).

The `Managed` + `ManagedBy` fields enable multi-hub scenarios where only the originating hub should delete the GCP resource.

## Alternatives Considered

### A. Workload Identity Federation Instead of Minted SAs

Rather than creating real SAs, use [Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation) to issue short-lived tokens tied to agent identity without persistent SAs.

**Pros:** No SA quota concerns; no persistent credentials to manage.  
**Cons:** Significantly more complex setup (WIF pool + provider per hub); users cannot grant IAM bindings to a WIF principal as intuitively as to an SA email; not all GCP services support WIF principals in IAM policies.

**Verdict:** Good long-term evolution but too complex for the initial feature. Can be added later as an alternative identity mode.

### B. Dedicated GCP Project per Grove

Each grove could have its own GCP project for minted SAs, isolating blast radius.

**Pros:** Perfect isolation; no quota sharing.  
**Cons:** Requires project creation permissions (much higher privilege); project quota limits; massive operational overhead.

**Verdict:** Over-engineered for the current scale. The per-grove cap on minted SAs in a shared project is sufficient.

### C. SA Key-Based Approach (Download JSON Keys)

Mint the SA and download a JSON key, storing it as a secret in the Hub.

**Pros:** Doesn't require the Hub to have ongoing token-creator permissions.  
**Cons:** Storing long-lived SA keys is a significant security risk; keys don't expire; contradicts the existing keyless impersonation architecture.

**Verdict:** Rejected. The impersonation-based approach is strictly better for security.

### D. Users Create SAs via Scion CLI in Their Own Projects

Wrap `gcloud iam service-accounts create` behind a `scion` CLI command that also sets up the impersonation grant.

**Pros:** SAs live in user projects; no shared quota.  
**Cons:** Requires users to have GCP projects and IAM admin permissions; still complex; doesn't solve the "zero GCP knowledge" use case.

**Verdict:** Useful as a complementary power-user flow but doesn't replace the hub-minted approach for simplicity.

## Security Considerations

1. **Blast radius of Hub SA compromise:** If the Hub SA is compromised, the attacker can impersonate all minted SAs. This is the same risk as the existing BYOSA model — the Hub SA is already a high-value target. Minting doesn't materially increase this risk since minted SAs are permissionless by default.

2. **Permissionless by default:** Minted SAs have no IAM roles. Users must explicitly grant permissions on their own projects. The Hub does not facilitate this — it is an out-of-band action.

3. **SA email as stable identifier:** Users will use minted SA emails in their own IAM policies. Deleting and re-creating a minted SA would create a new unique ID, so even if the email is reused, old IAM bindings are invalidated by GCP (30-day tombstone). The UI should warn before deletion of a managed SA that may have external bindings.

4. **Audit trail:** All mint and delete operations are recorded via the existing audit logging infrastructure.

5. **Multi-tenancy:** The `managed_by` field prevents cross-hub deletion in federated deployments.

## Open Questions

1. **Should minted SAs be re-assignable across groves?** Currently, SAs are scoped to a single grove. A minted SA could potentially be shared across groves owned by the same user. Should we support `scope: "user"` for minted SAs, or keep them strictly grove-scoped?

2. **Soft-delete for minted SAs?** GCP has a 30-day undelete window for deleted SAs. Should we implement a soft-delete in the Hub to match, or rely on GCP's native undelete?

3. **Should the Hub expose SA permissions?** Users need to grant IAM on their own projects. Should the Hub provide a "suggested gcloud command" or even a permission-granting flow (via user's OAuth token), or keep that entirely out-of-band?

4. **Naming convention flexibility:** Is `scion-{random}` sufficient, or should users be able to specify a custom account ID (subject to validation)? Custom IDs improve readability in GCP console but risk collisions.

5. **Quota monitoring:** Should the Hub proactively check remaining SA quota in the project before attempting to mint, or just handle the quota-exceeded error reactively?

6. **Grove deletion cascade:** When a grove is hard-deleted, should managed SAs be deleted immediately, or retained for a grace period (matching the soft-delete retention window)?

## Implementation Plan

### Phase 1: Core Minting
- [ ] Add `GCPProjectID` to `ServerConfig` with metadata-server auto-detection
- [ ] Implement `GCPServiceAccountAdmin` interface and IAM Admin API client
- [ ] Add `managed`/`managed_by` columns (new migration)
- [ ] Implement `POST .../mint` endpoint with authz, audit logging
- [ ] Update delete handler to cascade GCP SA deletion for managed SAs
- [ ] Add `scion grove service-accounts mint` CLI command
- [ ] Unit tests for admin client, handler, and store changes
- [ ] Integration test with IAM API (requires test project)

### Phase 2: Limits & Lifecycle
- [ ] Per-grove and global mint caps (configurable)
- [ ] Cascade delete managed SAs on grove hard-delete
- [ ] Orphan SA cleanup in maintenance scheduler
- [ ] Web UI: mint button, managed badge, deletion warning

### Phase 3: UX Enhancements
- [ ] "Suggested gcloud command" output after minting (for granting IAM)
- [ ] Quota visibility on admin dashboard
- [ ] Bulk operations (mint N SAs for a grove)
