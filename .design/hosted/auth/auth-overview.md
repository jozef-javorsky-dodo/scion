# Scion Authentication Overview

## Status
**Proposed**

## 1. Overview

This document specifies the authentication mechanisms for Scion's hosted mode. Authentication establishes user identity across multiple client types while maintaining security and usability.

### Authentication Contexts

| Context | Client Type | Auth Method | Token Storage |
|---------|-------------|-------------|---------------|
| Web Dashboard | Browser | OAuth + Session Cookie | HTTP-only cookie |
| CLI (Hub Commands) | Terminal | OAuth + Device Flow | Local file (`~/.scion/credentials.json`) |
| Agent (sciontool) | Container | Hub-issued JWT | Environment Variable (`SCION_HUB_TOKEN`) |
| API Direct | Programmatic | API Key or JWT | Client-managed |
| **Development** | Any | Dev Token (Bearer) | Local file (`~/.scion/dev-token`) |

### Goals

1. **Unified Identity** - Single user identity across all client types
2. **Secure Token Management** - Appropriate storage for each context
3. **Developer Experience** - Minimal friction for CLI authentication
4. **Standard Protocols** - OAuth 2.0 / OpenID Connect compliance

### Non-Goals

- Runtime host authentication (addressed in separate design - see [runtime-host-auth.md](runtime-host-auth.md))
- Service-to-service authentication between Hub components
- Multi-tenant Hub federation

---

## 2. Identity Model

### 2.1 User Identity

A user is identified by their email address, which serves as the canonical identifier across OAuth providers.

```go
type User struct {
    ID           string    `json:"id"`           // UUID primary key
    Email        string    `json:"email"`        // Canonical identifier
    DisplayName  string    `json:"displayName"`
    AvatarURL    string    `json:"avatarUrl,omitempty"`

    // OAuth provider info
    Provider     string    `json:"provider"`     // "google", "github", etc.
    ProviderID   string    `json:"providerId"`   // Provider's user ID

    // Status
    Role         string    `json:"role"`         // "admin", "member", "viewer"
    Status       string    `json:"status"`       // "active", "suspended", "pending"

    // Timestamps
    Created      time.Time `json:"created"`
    LastLogin    time.Time `json:"lastLogin"`
}
```

### 2.2 Authentication Tokens

The Hub issues JWT tokens for authenticated sessions:

```go
type TokenClaims struct {
    jwt.RegisteredClaims

    UserID      string   `json:"uid"`
    Email       string   `json:"email"`
    Role        string   `json:"role"`
    TokenType   string   `json:"type"`    // "access", "refresh", "cli"
    ClientType  string   `json:"client"`  // "web", "cli", "api"
}
```

**Token Types:**

| Type | Lifetime | Purpose |
|------|----------|---------|
| `access` | 15 minutes | Short-lived API access |
| `refresh` | 7 days | Token renewal |
| `cli` | 30 days | CLI session (longer-lived for developer convenience) |

---

## Related Documents

- [Web Authentication](web-auth.md) - OAuth flows for web dashboard
- [CLI Authentication](cli-auth.md) - Terminal-based authentication
- [Agent Authentication](sciontool-auth.md) - Agent-to-Hub secure communication
- [Server Auth Setup](server-auth-setup.md) - API keys, dev auth, and security
- [Runtime Host Auth](runtime-host-auth.md) - Host registration and mutual TLS
- [Implementation Milestones](auth-milestones.md) - Phased implementation plan

## References

- **Permissions System:** `permissions-design.md`
- **Web Frontend:** `web-frontend-design.md`
- **Hub API:** `hub-api.md`
- **OAuth 2.0 RFC:** https://datatracker.ietf.org/doc/html/rfc6749
- **PKCE RFC:** https://datatracker.ietf.org/doc/html/rfc7636
