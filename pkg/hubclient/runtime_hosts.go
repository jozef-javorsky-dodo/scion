package hubclient

import (
	"context"
	"net/url"

	"github.com/ptone/scion-agent/pkg/apiclient"
)

// RuntimeHostService handles runtime host operations.
type RuntimeHostService interface {
	// Create creates a new host registration and returns a join token.
	// The join token must be used with Join() to complete registration.
	Create(ctx context.Context, req *CreateHostRequest) (*CreateHostResponse, error)

	// Join completes host registration using a join token.
	// Returns the HMAC secret key for future authentication.
	Join(ctx context.Context, req *JoinHostRequest) (*JoinHostResponse, error)

	// List returns runtime hosts matching the filter criteria.
	List(ctx context.Context, opts *ListHostsOptions) (*ListHostsResponse, error)

	// Get returns a single runtime host by ID.
	Get(ctx context.Context, hostID string) (*RuntimeHost, error)

	// Update updates host metadata.
	Update(ctx context.Context, hostID string, req *UpdateHostRequest) (*RuntimeHost, error)

	// Delete removes a host from all groves.
	Delete(ctx context.Context, hostID string) error

	// ListGroves returns groves this host contributes to.
	ListGroves(ctx context.Context, hostID string) (*ListHostGrovesResponse, error)

	// Heartbeat sends a heartbeat for a host.
	Heartbeat(ctx context.Context, hostID string, status *HostHeartbeat) error
}

// runtimeHostService is the implementation of RuntimeHostService.
type runtimeHostService struct {
	c *client
}

// ListHostsOptions configures runtime host list filtering.
type ListHostsOptions struct {
	Status  string // Filter by status (online, offline)
	Mode    string // Filter by mode (connected, read-only)
	GroveID string // Filter by grove contribution
	Page    apiclient.PageOptions
}

// ListHostsResponse is the response from listing runtime hosts.
type ListHostsResponse struct {
	Hosts []RuntimeHost
	Page  apiclient.PageResult
}

// UpdateHostRequest is the request for updating a runtime host.
type UpdateHostRequest struct {
	Name        string            `json:"name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ListHostGrovesResponse is the response from listing host groves.
type ListHostGrovesResponse struct {
	Groves []HostGroveInfo `json:"groves"`
}

// HostHeartbeat is the heartbeat payload.
type HostHeartbeat struct {
	Status string           `json:"status"`
	Groves []GroveHeartbeat `json:"groves,omitempty"`
}

// GroveHeartbeat is per-grove status in a heartbeat.
type GroveHeartbeat struct {
	GroveID    string           `json:"groveId"`
	AgentCount int              `json:"agentCount"`
	Agents     []AgentHeartbeat `json:"agents,omitempty"`
}

// AgentHeartbeat is per-agent status in a heartbeat.
type AgentHeartbeat struct {
	AgentID         string `json:"agentId"`
	Status          string `json:"status"`
	ContainerStatus string `json:"containerStatus,omitempty"`
}

// CreateHostRequest is the request to create a new host registration.
type CreateHostRequest struct {
	Name         string            `json:"name"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// CreateHostResponse is returned when creating a new host.
type CreateHostResponse struct {
	HostID    string `json:"hostId"`
	JoinToken string `json:"joinToken"`
	ExpiresAt string `json:"expiresAt"`
}

// JoinHostRequest is the request to complete host registration.
type JoinHostRequest struct {
	HostID       string   `json:"hostId"`
	JoinToken    string   `json:"joinToken"`
	Hostname     string   `json:"hostname"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// JoinHostResponse is returned after completing host registration.
type JoinHostResponse struct {
	SecretKey   string `json:"secretKey"` // Base64-encoded HMAC secret
	HubEndpoint string `json:"hubEndpoint"`
	HostID      string `json:"hostId"`
}

// Create creates a new host registration and returns a join token.
func (s *runtimeHostService) Create(ctx context.Context, req *CreateHostRequest) (*CreateHostResponse, error) {
	resp, err := s.c.transport.Post(ctx, "/api/v1/hosts", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[CreateHostResponse](resp)
}

// Join completes host registration using a join token.
func (s *runtimeHostService) Join(ctx context.Context, req *JoinHostRequest) (*JoinHostResponse, error) {
	resp, err := s.c.transport.Post(ctx, "/api/v1/hosts/join", req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[JoinHostResponse](resp)
}

// List returns runtime hosts matching the filter criteria.
func (s *runtimeHostService) List(ctx context.Context, opts *ListHostsOptions) (*ListHostsResponse, error) {
	query := url.Values{}
	if opts != nil {
		if opts.Status != "" {
			query.Set("status", opts.Status)
		}
		if opts.Mode != "" {
			query.Set("mode", opts.Mode)
		}
		if opts.GroveID != "" {
			query.Set("groveId", opts.GroveID)
		}
		opts.Page.ToQuery(query)
	}

	resp, err := s.c.transport.GetWithQuery(ctx, "/api/v1/runtime-hosts", query, nil)
	if err != nil {
		return nil, err
	}

	type listResponse struct {
		Hosts      []RuntimeHost `json:"hosts"`
		NextCursor string        `json:"nextCursor,omitempty"`
		TotalCount int           `json:"totalCount,omitempty"`
	}

	result, err := apiclient.DecodeResponse[listResponse](resp)
	if err != nil {
		return nil, err
	}

	return &ListHostsResponse{
		Hosts: result.Hosts,
		Page: apiclient.PageResult{
			NextCursor: result.NextCursor,
			TotalCount: result.TotalCount,
		},
	}, nil
}

// Get returns a single runtime host by ID.
func (s *runtimeHostService) Get(ctx context.Context, hostID string) (*RuntimeHost, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/runtime-hosts/"+hostID, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[RuntimeHost](resp)
}

// Update updates host metadata.
func (s *runtimeHostService) Update(ctx context.Context, hostID string, req *UpdateHostRequest) (*RuntimeHost, error) {
	resp, err := s.c.transport.Patch(ctx, "/api/v1/runtime-hosts/"+hostID, req, nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[RuntimeHost](resp)
}

// Delete removes a host from all groves.
func (s *runtimeHostService) Delete(ctx context.Context, hostID string) error {
	resp, err := s.c.transport.Delete(ctx, "/api/v1/runtime-hosts/"+hostID, nil)
	if err != nil {
		return err
	}
	return apiclient.CheckResponse(resp)
}

// ListGroves returns groves this host contributes to.
func (s *runtimeHostService) ListGroves(ctx context.Context, hostID string) (*ListHostGrovesResponse, error) {
	resp, err := s.c.transport.Get(ctx, "/api/v1/runtime-hosts/"+hostID+"/groves", nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[ListHostGrovesResponse](resp)
}

// Heartbeat sends a heartbeat for a host.
func (s *runtimeHostService) Heartbeat(ctx context.Context, hostID string, status *HostHeartbeat) error {
	resp, err := s.c.transport.Post(ctx, "/api/v1/runtime-hosts/"+hostID+"/heartbeat", status, nil)
	if err != nil {
		return err
	}
	return apiclient.CheckResponse(resp)
}
