package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)

	err = s.Migrate(context.Background())
	require.NoError(t, err)

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

// ============================================================================
// Agent Tests
// ============================================================================

func TestAgentCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// First create a grove for the agent
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test Grove",
		Slug:       "test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create agent
	agent := &store.Agent{
		ID:         api.NewUUID(),
		AgentID:    "test-agent",
		Name:       "Test Agent",
		Template:   "claude",
		GroveID:    grove.ID,
		Status:     store.AgentStatusPending,
		Visibility: store.VisibilityPrivate,
		Labels:     map[string]string{"env": "test"},
	}

	err := s.CreateAgent(ctx, agent)
	require.NoError(t, err)
	assert.NotZero(t, agent.Created)
	assert.Equal(t, int64(1), agent.StateVersion)

	// Get agent
	retrieved, err := s.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, retrieved.ID)
	assert.Equal(t, agent.AgentID, retrieved.AgentID)
	assert.Equal(t, agent.Name, retrieved.Name)
	assert.Equal(t, agent.Template, retrieved.Template)
	assert.Equal(t, "test", retrieved.Labels["env"])

	// Get by slug
	retrieved, err = s.GetAgentBySlug(ctx, grove.ID, "test-agent")
	require.NoError(t, err)
	assert.Equal(t, agent.ID, retrieved.ID)

	// Update agent
	retrieved.Name = "Updated Agent"
	retrieved.Status = store.AgentStatusRunning
	err = s.UpdateAgent(ctx, retrieved)
	require.NoError(t, err)
	assert.Equal(t, int64(2), retrieved.StateVersion)

	// Verify update
	retrieved, err = s.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Agent", retrieved.Name)
	assert.Equal(t, store.AgentStatusRunning, retrieved.Status)

	// Test version conflict
	oldVersion := retrieved.StateVersion
	retrieved.StateVersion = 1 // Use old version
	err = s.UpdateAgent(ctx, retrieved)
	assert.ErrorIs(t, err, store.ErrVersionConflict)

	// Restore correct version for delete
	retrieved.StateVersion = oldVersion

	// Delete agent
	err = s.DeleteAgent(ctx, agent.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = s.GetAgent(ctx, agent.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create grove
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test Grove",
		Slug:       "test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create multiple agents
	for i := 0; i < 5; i++ {
		agent := &store.Agent{
			ID:         api.NewUUID(),
			AgentID:    api.Slugify("agent-" + string(rune('a'+i))),
			Name:       "Agent " + string(rune('A'+i)),
			Template:   "claude",
			GroveID:    grove.ID,
			Status:     store.AgentStatusRunning,
			Visibility: store.VisibilityPrivate,
		}
		if i%2 == 0 {
			agent.Status = store.AgentStatusStopped
		}
		require.NoError(t, s.CreateAgent(ctx, agent))
	}

	// List all
	result, err := s.ListAgents(ctx, store.AgentFilter{}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 5, result.TotalCount)
	assert.Len(t, result.Items, 5)

	// List by status
	result, err = s.ListAgents(ctx, store.AgentFilter{Status: store.AgentStatusRunning}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, result.TotalCount)

	// List by grove
	result, err = s.ListAgents(ctx, store.AgentFilter{GroveID: grove.ID}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 5, result.TotalCount)

	// Test pagination
	result, err = s.ListAgents(ctx, store.AgentFilter{}, store.ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestAgentStatusUpdate(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create grove and agent
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test Grove",
		Slug:       "test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	agent := &store.Agent{
		ID:         api.NewUUID(),
		AgentID:    "test-agent",
		Name:       "Test Agent",
		Template:   "claude",
		GroveID:    grove.ID,
		Status:     store.AgentStatusPending,
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	// Update status
	err := s.UpdateAgentStatus(ctx, agent.ID, store.AgentStatusUpdate{
		Status:          store.AgentStatusRunning,
		SessionStatus:   "waiting",
		ContainerStatus: "Up 5 minutes",
	})
	require.NoError(t, err)

	// Verify
	retrieved, err := s.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, store.AgentStatusRunning, retrieved.Status)
	assert.Equal(t, "waiting", retrieved.SessionStatus)
	assert.Equal(t, "Up 5 minutes", retrieved.ContainerStatus)
}

// ============================================================================
// Grove Tests
// ============================================================================

func TestGroveCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create grove
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "My Project",
		Slug:       "my-project",
		GitRemote:  "github.com/org/repo",
		Visibility: store.VisibilityPrivate,
		Labels:     map[string]string{"team": "platform"},
	}

	err := s.CreateGrove(ctx, grove)
	require.NoError(t, err)
	assert.NotZero(t, grove.Created)

	// Get grove
	retrieved, err := s.GetGrove(ctx, grove.ID)
	require.NoError(t, err)
	assert.Equal(t, grove.Name, retrieved.Name)
	assert.Equal(t, grove.GitRemote, retrieved.GitRemote)
	assert.Equal(t, "platform", retrieved.Labels["team"])

	// Get by slug
	retrieved, err = s.GetGroveBySlug(ctx, "my-project")
	require.NoError(t, err)
	assert.Equal(t, grove.ID, retrieved.ID)

	// Get by git remote
	retrieved, err = s.GetGroveByGitRemote(ctx, "github.com/org/repo")
	require.NoError(t, err)
	assert.Equal(t, grove.ID, retrieved.ID)

	// Test unique constraint on git remote
	duplicate := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Duplicate",
		Slug:       "duplicate",
		GitRemote:  "github.com/org/repo",
		Visibility: store.VisibilityPrivate,
	}
	err = s.CreateGrove(ctx, duplicate)
	assert.ErrorIs(t, err, store.ErrAlreadyExists)

	// Update grove
	retrieved.Name = "Updated Project"
	err = s.UpdateGrove(ctx, retrieved)
	require.NoError(t, err)

	// Verify update
	retrieved, err = s.GetGrove(ctx, grove.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Project", retrieved.Name)

	// Delete grove
	err = s.DeleteGrove(ctx, grove.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestGroveList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create groves
	for i := 0; i < 3; i++ {
		grove := &store.Grove{
			ID:         api.NewUUID(),
			Name:       "Grove " + string(rune('A'+i)),
			Slug:       "grove-" + string(rune('a'+i)),
			Visibility: store.VisibilityPrivate,
		}
		if i == 0 {
			grove.Visibility = store.VisibilityPublic
		}
		require.NoError(t, s.CreateGrove(ctx, grove))
	}

	// List all
	result, err := s.ListGroves(ctx, store.GroveFilter{}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalCount)

	// List by visibility
	result, err = s.ListGroves(ctx, store.GroveFilter{Visibility: store.VisibilityPublic}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalCount)
}

// ============================================================================
// RuntimeHost Tests
// ============================================================================

func TestGroveLookupCaseInsensitive(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create a grove with mixed case name
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Global",
		Slug:       "global",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Look up with exact case - should work
	retrieved, err := s.GetGroveBySlugCaseInsensitive(ctx, "global")
	require.NoError(t, err)
	assert.Equal(t, grove.ID, retrieved.ID)

	// Look up with different case - should still work
	retrieved, err = s.GetGroveBySlugCaseInsensitive(ctx, "GLOBAL")
	require.NoError(t, err)
	assert.Equal(t, grove.ID, retrieved.ID)

	// Look up with mixed case - should still work
	retrieved, err = s.GetGroveBySlugCaseInsensitive(ctx, "Global")
	require.NoError(t, err)
	assert.Equal(t, grove.ID, retrieved.ID)

	// Look up non-existent - should return ErrNotFound
	_, err = s.GetGroveBySlugCaseInsensitive(ctx, "nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRuntimeHostLookupByName(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create a host
	host := &store.RuntimeHost{
		ID:     api.NewUUID(),
		Name:   "My-Laptop",
		Slug:   "my-laptop",
		Mode:   store.HostModeConnected,
		Status: store.HostStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeHost(ctx, host))

	// Look up with exact case - should work
	retrieved, err := s.GetRuntimeHostByName(ctx, "My-Laptop")
	require.NoError(t, err)
	assert.Equal(t, host.ID, retrieved.ID)

	// Look up with different case - should still work (case-insensitive)
	retrieved, err = s.GetRuntimeHostByName(ctx, "my-laptop")
	require.NoError(t, err)
	assert.Equal(t, host.ID, retrieved.ID)

	// Look up with all caps - should still work
	retrieved, err = s.GetRuntimeHostByName(ctx, "MY-LAPTOP")
	require.NoError(t, err)
	assert.Equal(t, host.ID, retrieved.ID)

	// Look up non-existent - should return ErrNotFound
	_, err = s.GetRuntimeHostByName(ctx, "nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// ============================================================================
// RuntimeHost Tests
// ============================================================================

func TestRuntimeHostCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create host
	host := &store.RuntimeHost{
		ID:      api.NewUUID(),
		Name:    "Dev Laptop",
		Slug:    "dev-laptop",
		Mode:    store.HostModeConnected,
		Version: "1.0.0",
		Status:  store.HostStatusOnline,
		Capabilities: &store.HostCapabilities{
			WebPTY: true,
			Sync:   true,
			Attach: true,
		},
		Profiles: []store.HostProfile{
			{Name: "default", Type: "docker", Available: true},
		},
	}

	err := s.CreateRuntimeHost(ctx, host)
	require.NoError(t, err)
	assert.NotZero(t, host.Created)

	// Get host
	retrieved, err := s.GetRuntimeHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, host.Name, retrieved.Name)
	assert.True(t, retrieved.Capabilities.WebPTY)
	assert.Len(t, retrieved.Profiles, 1)
	assert.Equal(t, "docker", retrieved.Profiles[0].Type)

	// Update host
	retrieved.Status = store.HostStatusOffline
	err = s.UpdateRuntimeHost(ctx, retrieved)
	require.NoError(t, err)

	// Verify update
	retrieved, err = s.GetRuntimeHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, store.HostStatusOffline, retrieved.Status)

	// Update heartbeat
	err = s.UpdateRuntimeHostHeartbeat(ctx, host.ID, store.HostStatusOnline)
	require.NoError(t, err)

	// Verify heartbeat
	retrieved, err = s.GetRuntimeHost(ctx, host.ID)
	require.NoError(t, err)
	assert.Equal(t, store.HostStatusOnline, retrieved.Status)
	assert.NotZero(t, retrieved.LastHeartbeat)

	// Delete host
	err = s.DeleteRuntimeHost(ctx, host.ID)
	require.NoError(t, err)

	_, err = s.GetRuntimeHost(ctx, host.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRuntimeHostList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create hosts
	for i := 0; i < 3; i++ {
		host := &store.RuntimeHost{
			ID:     api.NewUUID(),
			Name:   "Host " + string(rune('A'+i)),
			Slug:   "host-" + string(rune('a'+i)),
			Mode:   store.HostModeConnected,
			Status: store.HostStatusOnline,
			Profiles: []store.HostProfile{
				{Name: "default", Type: "docker", Available: true},
			},
		}
		if i == 0 {
			host.Status = store.HostStatusOffline
		}
		require.NoError(t, s.CreateRuntimeHost(ctx, host))
	}

	// List all
	result, err := s.ListRuntimeHosts(ctx, store.RuntimeHostFilter{}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalCount)

	// List by status
	result, err = s.ListRuntimeHosts(ctx, store.RuntimeHostFilter{Status: store.HostStatusOffline}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalCount)
}

// ============================================================================
// Template Tests
// ============================================================================

func TestTemplateCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create template
	template := &store.Template{
		ID:         api.NewUUID(),
		Name:       "Claude Default",
		Slug:       "claude-default",
		Harness:    "claude",
		Image:      "scion-claude:latest",
		Scope:      "global",
		Visibility: store.VisibilityPublic,
		Config: &store.TemplateConfig{
			Harness:  "claude",
			Detached: true,
		},
	}

	err := s.CreateTemplate(ctx, template)
	require.NoError(t, err)
	assert.NotZero(t, template.Created)

	// Get template
	retrieved, err := s.GetTemplate(ctx, template.ID)
	require.NoError(t, err)
	assert.Equal(t, template.Name, retrieved.Name)
	assert.Equal(t, template.Harness, retrieved.Harness)
	assert.True(t, retrieved.Config.Detached)

	// Get by slug
	retrieved, err = s.GetTemplateBySlug(ctx, "claude-default", "global", "")
	require.NoError(t, err)
	assert.Equal(t, template.ID, retrieved.ID)

	// Update template
	retrieved.Image = "scion-claude:v2"
	err = s.UpdateTemplate(ctx, retrieved)
	require.NoError(t, err)

	// Verify update
	retrieved, err = s.GetTemplate(ctx, template.ID)
	require.NoError(t, err)
	assert.Equal(t, "scion-claude:v2", retrieved.Image)

	// Delete template
	err = s.DeleteTemplate(ctx, template.ID)
	require.NoError(t, err)

	_, err = s.GetTemplate(ctx, template.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestTemplateList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create templates
	for i := 0; i < 3; i++ {
		template := &store.Template{
			ID:         api.NewUUID(),
			Name:       "Template " + string(rune('A'+i)),
			Slug:       "template-" + string(rune('a'+i)),
			Harness:    "claude",
			Scope:      "global",
			Visibility: store.VisibilityPublic,
		}
		if i == 0 {
			template.Harness = "gemini"
		}
		require.NoError(t, s.CreateTemplate(ctx, template))
	}

	// List all
	result, err := s.ListTemplates(ctx, store.TemplateFilter{}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalCount)

	// List by harness
	result, err = s.ListTemplates(ctx, store.TemplateFilter{Harness: "gemini"}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalCount)
}

// ============================================================================
// User Tests
// ============================================================================

func TestUserCRUD(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create user
	user := &store.User{
		ID:          api.NewUUID(),
		Email:       "test@example.com",
		DisplayName: "Test User",
		Role:        store.UserRoleMember,
		Status:      "active",
		Preferences: &store.UserPreferences{
			Theme: "dark",
		},
	}

	err := s.CreateUser(ctx, user)
	require.NoError(t, err)
	assert.NotZero(t, user.Created)

	// Get user
	retrieved, err := s.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.Email, retrieved.Email)
	assert.Equal(t, "dark", retrieved.Preferences.Theme)

	// Get by email
	retrieved, err = s.GetUserByEmail(ctx, "test@example.com")
	require.NoError(t, err)
	assert.Equal(t, user.ID, retrieved.ID)

	// Test unique constraint on email
	duplicate := &store.User{
		ID:          api.NewUUID(),
		Email:       "test@example.com",
		DisplayName: "Duplicate User",
		Role:        store.UserRoleMember,
		Status:      "active",
	}
	err = s.CreateUser(ctx, duplicate)
	assert.ErrorIs(t, err, store.ErrAlreadyExists)

	// Update user
	retrieved.DisplayName = "Updated User"
	retrieved.LastLogin = time.Now()
	err = s.UpdateUser(ctx, retrieved)
	require.NoError(t, err)

	// Verify update
	retrieved, err = s.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated User", retrieved.DisplayName)
	assert.NotZero(t, retrieved.LastLogin)

	// Delete user
	err = s.DeleteUser(ctx, user.ID)
	require.NoError(t, err)

	_, err = s.GetUser(ctx, user.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestUserList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create users
	for i := 0; i < 3; i++ {
		user := &store.User{
			ID:          api.NewUUID(),
			Email:       "user" + string(rune('a'+i)) + "@example.com",
			DisplayName: "User " + string(rune('A'+i)),
			Role:        store.UserRoleMember,
			Status:      "active",
		}
		if i == 0 {
			user.Role = store.UserRoleAdmin
		}
		require.NoError(t, s.CreateUser(ctx, user))
	}

	// List all
	result, err := s.ListUsers(ctx, store.UserFilter{}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result.TotalCount)

	// List by role
	result, err = s.ListUsers(ctx, store.UserFilter{Role: store.UserRoleAdmin}, store.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalCount)
}

// ============================================================================
// GroveContributor Tests
// ============================================================================

func TestGroveContributors(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create grove
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test Grove",
		Slug:       "test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create hosts
	host1 := &store.RuntimeHost{
		ID:     api.NewUUID(),
		Name:   "Host 1",
		Slug:   "host-1",
		Mode:   store.HostModeConnected,
		Status: store.HostStatusOnline,
		Profiles: []store.HostProfile{
			{Name: "docker", Type: "docker", Available: true},
			{Name: "dev", Type: "docker", Available: true},
		},
	}
	require.NoError(t, s.CreateRuntimeHost(ctx, host1))

	host2 := &store.RuntimeHost{
		ID:     api.NewUUID(),
		Name:   "Host 2",
		Slug:   "host-2",
		Mode:   store.HostModeConnected,
		Status: store.HostStatusOnline,
		Profiles: []store.HostProfile{
			{Name: "k8s-prod", Type: "kubernetes", Available: true},
		},
	}
	require.NoError(t, s.CreateRuntimeHost(ctx, host2))

	// Add contributors
	contrib1 := &store.GroveContributor{
		GroveID:  grove.ID,
		HostID:   host1.ID,
		HostName: host1.Name,
		Mode:     store.HostModeConnected,
		Status:   store.HostStatusOnline,
	}
	require.NoError(t, s.AddGroveContributor(ctx, contrib1))

	contrib2 := &store.GroveContributor{
		GroveID:  grove.ID,
		HostID:   host2.ID,
		HostName: host2.Name,
		Mode:     store.HostModeReadOnly,
		Status:   store.HostStatusOnline,
	}
	require.NoError(t, s.AddGroveContributor(ctx, contrib2))

	// Get grove contributors
	contributors, err := s.GetGroveContributors(ctx, grove.ID)
	require.NoError(t, err)
	assert.Len(t, contributors, 2)

	// Get host groves
	groves, err := s.GetHostGroves(ctx, host1.ID)
	require.NoError(t, err)
	assert.Len(t, groves, 1)
	assert.Equal(t, grove.ID, groves[0].GroveID)

	// Update contributor status
	err = s.UpdateContributorStatus(ctx, grove.ID, host1.ID, store.HostStatusOffline)
	require.NoError(t, err)

	// Verify update
	contributors, err = s.GetGroveContributors(ctx, grove.ID)
	require.NoError(t, err)
	for _, c := range contributors {
		if c.HostID == host1.ID {
			assert.Equal(t, store.HostStatusOffline, c.Status)
		}
	}

	// Verify grove's active host count
	retrievedGrove, err := s.GetGrove(ctx, grove.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, retrievedGrove.ActiveHostCount) // Only host2 is online

	// Remove contributor
	err = s.RemoveGroveContributor(ctx, grove.ID, host1.ID)
	require.NoError(t, err)

	contributors, err = s.GetGroveContributors(ctx, grove.ID)
	require.NoError(t, err)
	assert.Len(t, contributors, 1)
	assert.Equal(t, host2.ID, contributors[0].HostID)
}

// ============================================================================
// Migration Tests
// ============================================================================

func TestMigration(t *testing.T) {
	s, err := New(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()

	// Run migrations
	err = s.Migrate(ctx)
	require.NoError(t, err)

	// Run again (should be idempotent)
	err = s.Migrate(ctx)
	require.NoError(t, err)

	// Verify tables exist by inserting data
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test",
		Slug:       "test",
		Visibility: store.VisibilityPrivate,
	}
	err = s.CreateGrove(ctx, grove)
	require.NoError(t, err)
}

func TestPing(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	err := s.Ping(ctx)
	require.NoError(t, err)
}

// ============================================================================
// Error Cases
// ============================================================================

func TestNotFoundErrors(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	nonExistentID := api.NewUUID()

	// Agent
	_, err := s.GetAgent(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	err = s.DeleteAgent(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Grove
	_, err = s.GetGrove(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	err = s.DeleteGrove(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// RuntimeHost
	_, err = s.GetRuntimeHost(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	err = s.DeleteRuntimeHost(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Template
	_, err = s.GetTemplate(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	err = s.DeleteTemplate(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// User
	_, err = s.GetUser(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	err = s.DeleteUser(ctx, nonExistentID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestCascadeDelete(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create grove with agent
	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Test Grove",
		Slug:       "test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	agent := &store.Agent{
		ID:         api.NewUUID(),
		AgentID:    "test-agent",
		Name:       "Test Agent",
		Template:   "claude",
		GroveID:    grove.ID,
		Status:     store.AgentStatusRunning,
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	// Delete grove
	err := s.DeleteGrove(ctx, grove.ID)
	require.NoError(t, err)

	// Verify agent was cascade deleted
	_, err = s.GetAgent(ctx, agent.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}
