// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !no_sqlite

package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptone/scion-agent/pkg/agent/state"
	"github.com/ptone/scion-agent/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHubNativeGrovePath(t *testing.T) {
	path, err := hubNativeGrovePath("my-test-grove")
	require.NoError(t, err)

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	expected := filepath.Join(homeDir, ".scion", "groves", "my-test-grove")
	assert.Equal(t, expected, path)
}

func TestCreateGrove_HubNative_NoGitRemote(t *testing.T) {
	srv, _ := testServer(t)

	body := CreateGroveRequest{
		Name: "Hub Native Grove",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var grove store.Grove
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&grove))

	assert.Equal(t, "Hub Native Grove", grove.Name)
	assert.Equal(t, "hub-native-grove", grove.Slug)
	assert.Empty(t, grove.GitRemote, "hub-native grove should have no git remote")

	// Verify the filesystem was initialized
	workspacePath, err := hubNativeGrovePath(grove.Slug)
	require.NoError(t, err)

	scionDir := filepath.Join(workspacePath, ".scion")
	settingsPath := filepath.Join(scionDir, "settings.yaml")

	_, err = os.Stat(settingsPath)
	assert.NoError(t, err, "settings.yaml should exist for hub-native grove")

	// Cleanup
	t.Cleanup(func() {
		os.RemoveAll(workspacePath)
	})
}

func TestCreateGrove_GitBacked_NoFilesystemInit(t *testing.T) {
	srv, _ := testServer(t)

	body := CreateGroveRequest{
		Name:      "Git Grove",
		GitRemote: "github.com/test/repo",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var grove store.Grove
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&grove))

	assert.Equal(t, "github.com/test/repo", grove.GitRemote)

	// Verify no filesystem was created for git-backed grove
	workspacePath, err := hubNativeGrovePath(grove.Slug)
	require.NoError(t, err)

	_, err = os.Stat(workspacePath)
	assert.True(t, os.IsNotExist(err), "no workspace directory should be created for git-backed groves")
}

func TestPopulateAgentConfig_HubNativeGrove_SetsWorkspace(t *testing.T) {
	srv, _ := testServer(t)

	grove := &store.Grove{
		ID:   "grove-hub-native",
		Name: "Hub Native",
		Slug: "hub-native",
		// No GitRemote — hub-native grove
	}

	agent := &store.Agent{
		ID:            "agent-test",
		AppliedConfig: &store.AgentAppliedConfig{},
	}

	srv.populateAgentConfig(agent, grove, nil)

	expectedPath, err := hubNativeGrovePath("hub-native")
	require.NoError(t, err)
	assert.Equal(t, expectedPath, agent.AppliedConfig.Workspace,
		"Workspace should be set for hub-native groves")
	assert.Nil(t, agent.AppliedConfig.GitClone,
		"GitClone should not be set for hub-native groves")
}

func TestPopulateAgentConfig_HubNativeGrove_RemoteBroker_WorkspaceSet(t *testing.T) {
	srv, _ := testServer(t)

	grove := &store.Grove{
		ID:   "grove-hub-native-remote",
		Name: "Hub Native Remote",
		Slug: "hub-native-remote",
		// No GitRemote — hub-native grove
	}

	agent := &store.Agent{
		ID:            "agent-remote",
		AppliedConfig: &store.AgentAppliedConfig{},
	}

	srv.populateAgentConfig(agent, grove, nil)

	// populateAgentConfig sets Workspace for hub-native groves.
	// For remote brokers, the createAgent handler later swaps this to
	// WorkspaceStoragePath. Here we verify the initial workspace is set.
	expectedPath, err := hubNativeGrovePath("hub-native-remote")
	require.NoError(t, err)
	assert.Equal(t, expectedPath, agent.AppliedConfig.Workspace)
}

func TestPopulateAgentConfig_GitGrove_NoWorkspace(t *testing.T) {
	srv, _ := testServer(t)

	grove := &store.Grove{
		ID:        "grove-git",
		Name:      "Git Grove",
		Slug:      "git-grove",
		GitRemote: "github.com/test/repo",
	}

	agent := &store.Agent{
		ID:            "agent-test",
		AppliedConfig: &store.AgentAppliedConfig{},
	}

	srv.populateAgentConfig(agent, grove, nil)

	assert.Empty(t, agent.AppliedConfig.Workspace,
		"Workspace should not be set for git-backed groves")
	assert.NotNil(t, agent.AppliedConfig.GitClone,
		"GitClone should be set for git-backed groves")
}

// TestCreateAgent_HubNativeGrove_ExplicitBroker_AutoLinks tests that creating an agent
// in a hub-native grove with an explicitly selected broker auto-links the broker as a
// provider, even if it wasn't previously registered as one.
func TestCreateAgent_HubNativeGrove_ExplicitBroker_AutoLinks(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a runtime broker
	broker := &store.RuntimeBroker{
		ID:     "broker-hub-autolink",
		Slug:   "hub-autolink-broker",
		Name:   "Hub Autolink Broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a hub-native grove (no git remote, no default broker, no providers)
	grove := &store.Grove{
		ID:   "grove-hub-autolink",
		Slug: "hub-autolink",
		Name: "Hub Autolink Grove",
		// No GitRemote — hub-native
		// No DefaultRuntimeBrokerID
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create agent with explicit broker — this should auto-link the broker
	body := map[string]interface{}{
		"name":            "autolink-agent",
		"groveId":         grove.ID,
		"runtimeBrokerId": broker.ID,
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp CreateAgentResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.NotNil(t, resp.Agent)
	assert.Equal(t, broker.ID, resp.Agent.RuntimeBrokerID,
		"Agent should be assigned to the explicitly selected broker")

	// Verify the broker was auto-linked as a provider
	provider, err := s.GetGroveProvider(ctx, grove.ID, broker.ID)
	require.NoError(t, err, "Broker should have been auto-linked as a provider")
	assert.Equal(t, broker.ID, provider.BrokerID)
	assert.Equal(t, "agent-create", provider.LinkedBy)

	// Verify the broker was set as the default
	updatedGrove, err := s.GetGrove(ctx, grove.ID)
	require.NoError(t, err)
	assert.Equal(t, broker.ID, updatedGrove.DefaultRuntimeBrokerID,
		"Broker should be set as the default for the grove")
}

// TestCreateGrove_HubNative_AutoProvide tests that creating a hub-native grove
// auto-links brokers with auto_provide enabled.
func TestCreateGrove_HubNative_AutoProvide(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a broker with auto_provide enabled
	broker := &store.RuntimeBroker{
		ID:          "broker-autoprovide",
		Slug:        "autoprovide-broker",
		Name:        "Auto Provide Broker",
		Status:      store.BrokerStatusOnline,
		AutoProvide: true,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a hub-native grove via the API
	body := CreateGroveRequest{
		Name: "Auto Provide Grove",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var grove store.Grove
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&grove))
	assert.Empty(t, grove.GitRemote, "should be hub-native")

	// Verify the auto-provide broker was linked
	provider, err := s.GetGroveProvider(ctx, grove.ID, broker.ID)
	require.NoError(t, err, "Auto-provide broker should be linked as a provider")
	assert.Equal(t, "auto-provide", provider.LinkedBy)

	// Verify the broker was set as the default
	updatedGrove, err := s.GetGrove(ctx, grove.ID)
	require.NoError(t, err)
	assert.Equal(t, broker.ID, updatedGrove.DefaultRuntimeBrokerID,
		"Auto-provide broker should be set as the default")

	// Now create an agent — should work without explicit broker
	agentBody := map[string]interface{}{
		"name":    "autoprovide-agent",
		"groveId": grove.ID,
	}
	rec = doRequest(t, srv, http.MethodPost, "/api/v1/agents", agentBody)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var resp CreateAgentResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, broker.ID, resp.Agent.RuntimeBrokerID,
		"Agent should use the auto-provided default broker")

	// Cleanup hub-native grove filesystem
	workspacePath, err := hubNativeGrovePath(grove.Slug)
	if err == nil {
		t.Cleanup(func() { os.RemoveAll(workspacePath) })
	}
}

// TestCreateAgent_HubNativeGrove_NoProviders_NoBroker tests that creating an agent
// in a hub-native grove with no providers and no explicit broker returns an appropriate error.
func TestDeleteGrove_HubNative_RemovesFilesystem(t *testing.T) {
	srv, s := testServer(t)

	// Create a hub-native grove via the API (initializes filesystem)
	grove, workspacePath := createTestHubNativeGrove(t, srv, "FS Delete Test")

	// Verify filesystem exists before deletion
	_, err := os.Stat(workspacePath)
	require.NoError(t, err, "workspace should exist before deletion")

	// Delete grove via API
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify filesystem was removed
	_, err = os.Stat(workspacePath)
	assert.True(t, os.IsNotExist(err), "workspace should be deleted from filesystem")

	// Verify grove deleted from database
	ctx := context.Background()
	_, err = s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound, "grove should be deleted from database")
}

func TestDeleteGrove_GitBacked_NoFilesystemCleanup(t *testing.T) {
	srv, s := testServer(t)

	// Create a git-backed grove (no filesystem initialization)
	grove := createTestGitGrove(t, srv, "Git Delete Test", "github.com/test/git-delete-repo")

	// Delete grove via API
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify grove deleted from database
	ctx := context.Background()
	_, err := s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound, "grove should be deleted from database")
}

func TestDeleteGrove_DeleteAgents_DispatchesToBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Set up a mock dispatcher to track agent deletion
	disp := &deleteDispatcher{}
	srv.SetDispatcher(disp)

	grove, _, _ := setupOnlineBrokerAgent(t, s, "grove-del")

	// Create a second agent in the same grove
	agent2 := &store.Agent{
		ID:              "agent-online-grove-del-2",
		Slug:            "agent-online-grove-del-2-slug",
		Name:            "Agent Online grove-del 2",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-online-grove-del",
		Phase: string(state.PhaseRunning),
	}
	require.NoError(t, s.CreateAgent(ctx, agent2))

	// Delete grove with deleteAgents=true
	rec := doRequest(t, srv, http.MethodDelete,
		"/api/v1/groves/"+grove.ID+"?deleteAgents=true", nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify dispatcher was called for both agents
	assert.Equal(t, 2, disp.deleteCalls,
		"DispatchAgentDelete should be called once per agent")

	// Verify grove deleted from database
	_, err := s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Verify agents cascade-deleted from database
	_, err = s.GetAgent(ctx, "agent-online-grove-del")
	assert.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.GetAgent(ctx, agent2.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestDeleteGrove_WithoutDeleteAgents_SkipsBrokerDispatch(t *testing.T) {
	srv, s := testServer(t)

	disp := &deleteDispatcher{}
	srv.SetDispatcher(disp)

	grove, _, _ := setupOnlineBrokerAgent(t, s, "grove-nodelflag")

	// Delete grove without deleteAgents flag
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Dispatcher should NOT have been called
	assert.Equal(t, 0, disp.deleteCalls,
		"DispatchAgentDelete should not be called without deleteAgents flag")

	// Grove should still be deleted from database (cascade deletes agent records)
	ctx := context.Background()
	_, err := s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestCreateAgent_HubNativeGrove_NoProviders_NoBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a hub-native grove with no providers
	grove := &store.Grove{
		ID:   "grove-hub-noproviders",
		Slug: "hub-noproviders",
		Name: "No Providers Grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	body := map[string]interface{}{
		"name":    "orphan-agent",
		"groveId": grove.ID,
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
	// Should fail because there are no providers and no broker specified
	assert.NotEqual(t, http.StatusCreated, rec.Code,
		"Should fail when no providers exist and no broker is specified")
}

// TestAutoLinkProviders_HubNativeGrove_NoLocalPath verifies that autoLinkProviders
// does NOT set LocalPath on the provider for hub-native groves. The hub's local
// path is not valid for remote brokers — instead, groveSlug is sent so each
// broker resolves the path on its own filesystem.
func TestAutoLinkProviders_HubNativeGrove_NoLocalPath(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a broker with auto_provide enabled
	broker := &store.RuntimeBroker{
		ID:          "broker-localpath-auto",
		Slug:        "localpath-auto-broker",
		Name:        "LocalPath Auto Broker",
		Status:      store.BrokerStatusOnline,
		AutoProvide: true,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a hub-native grove via the API — this triggers autoLinkProviders
	body := CreateGroveRequest{
		Name: "LocalPath Auto Grove",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var grove store.Grove
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&grove))
	assert.Empty(t, grove.GitRemote, "should be hub-native")

	// Verify the auto-linked provider does NOT have LocalPath set
	provider, err := s.GetGroveProvider(ctx, grove.ID, broker.ID)
	require.NoError(t, err, "Auto-provide broker should be linked as a provider")
	assert.Equal(t, "auto-provide", provider.LinkedBy)
	assert.Empty(t, provider.LocalPath,
		"LocalPath should NOT be set for hub-native grove auto-linked provider")

	// Cleanup hub-native grove filesystem
	workspacePath, err := hubNativeGrovePath(grove.Slug)
	if err == nil {
		t.Cleanup(func() { os.RemoveAll(workspacePath) })
	}
}

// TestAutoLinkProviders_GitGrove_NoLocalPath verifies that autoLinkProviders
// does NOT set LocalPath on the provider for git-backed groves.
func TestAutoLinkProviders_GitGrove_NoLocalPath(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a broker with auto_provide enabled
	broker := &store.RuntimeBroker{
		ID:          "broker-localpath-git",
		Slug:        "localpath-git-broker",
		Name:        "LocalPath Git Broker",
		Status:      store.BrokerStatusOnline,
		AutoProvide: true,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a git-backed grove via the API — this also triggers autoLinkProviders
	body := CreateGroveRequest{
		Name:      "LocalPath Git Grove",
		GitRemote: "github.com/test/localpath-git-repo",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves", body)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var grove store.Grove
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&grove))

	// Verify the provider does NOT have LocalPath set
	provider, err := s.GetGroveProvider(ctx, grove.ID, broker.ID)
	require.NoError(t, err, "Auto-provide broker should be linked")
	assert.Empty(t, provider.LocalPath,
		"LocalPath should NOT be set for git-backed grove providers")
}

// TestDeleteGrove_HubNative_DispatchesCleanupToBrokers verifies that deleting a
// hub-native grove dispatches CleanupGrove to each provider broker (except the
// embedded/co-located broker).
func TestDeleteGrove_HubNative_DispatchesCleanupToBrokers(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a hub-native grove
	grove := &store.Grove{
		ID:   "grove-cleanup-dispatch",
		Slug: "cleanup-dispatch",
		Name: "Cleanup Dispatch Grove",
		// No GitRemote — hub-native
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create two brokers
	broker1 := &store.RuntimeBroker{
		ID:       "broker-cleanup-1",
		Slug:     "cleanup-broker-1",
		Name:     "Cleanup Broker 1",
		Status:   store.BrokerStatusOnline,
		Endpoint: "http://broker1:9800",
	}
	broker2 := &store.RuntimeBroker{
		ID:       "broker-cleanup-2",
		Slug:     "cleanup-broker-2",
		Name:     "Cleanup Broker 2",
		Status:   store.BrokerStatusOnline,
		Endpoint: "http://broker2:9800",
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker1))
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker2))

	// Link both as providers
	require.NoError(t, s.AddGroveProvider(ctx, &store.GroveProvider{
		GroveID:  grove.ID,
		BrokerID: broker1.ID,
		LinkedBy: "test",
	}))
	require.NoError(t, s.AddGroveProvider(ctx, &store.GroveProvider{
		GroveID:  grove.ID,
		BrokerID: broker2.ID,
		LinkedBy: "test",
	}))

	// Set up a mock client and dispatcher
	mockClient := &mockRuntimeBrokerClient{}
	disp := NewHTTPAgentDispatcherWithClient(s, mockClient, false)
	srv.SetDispatcher(disp)

	// Delete grove
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify CleanupGrove was called for both brokers
	assert.Equal(t, 2, mockClient.cleanupCalls, "CleanupGrove should be called for each provider broker")
	assert.Contains(t, mockClient.cleanupSlugs, "cleanup-dispatch")

	// Verify grove deleted from database
	_, err := s.GetGrove(ctx, grove.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestDeleteGrove_HubNative_SkipsEmbeddedBroker verifies that the embedded broker
// (co-located hub+broker) is not called for cleanup since the hub handles its own copy.
func TestDeleteGrove_HubNative_SkipsEmbeddedBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a hub-native grove
	grove := &store.Grove{
		ID:   "grove-cleanup-embedded",
		Slug: "cleanup-embedded",
		Name: "Cleanup Embedded Grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create embedded and remote brokers
	embeddedBroker := &store.RuntimeBroker{
		ID:       "broker-embedded",
		Slug:     "embedded-broker",
		Name:     "Embedded Broker",
		Status:   store.BrokerStatusOnline,
		Endpoint: "http://localhost:9800",
	}
	remoteBroker := &store.RuntimeBroker{
		ID:       "broker-remote",
		Slug:     "remote-broker",
		Name:     "Remote Broker",
		Status:   store.BrokerStatusOnline,
		Endpoint: "http://remote:9800",
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, embeddedBroker))
	require.NoError(t, s.CreateRuntimeBroker(ctx, remoteBroker))

	// Link both as providers
	require.NoError(t, s.AddGroveProvider(ctx, &store.GroveProvider{
		GroveID:  grove.ID,
		BrokerID: embeddedBroker.ID,
		LinkedBy: "test",
	}))
	require.NoError(t, s.AddGroveProvider(ctx, &store.GroveProvider{
		GroveID:  grove.ID,
		BrokerID: remoteBroker.ID,
		LinkedBy: "test",
	}))

	// Mark embedded broker
	srv.SetEmbeddedBrokerID(embeddedBroker.ID)

	// Set up mock client and dispatcher
	mockClient := &mockRuntimeBrokerClient{}
	disp := NewHTTPAgentDispatcherWithClient(s, mockClient, false)
	srv.SetDispatcher(disp)

	// Delete grove
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Only the remote broker should receive CleanupGrove, not the embedded one
	assert.Equal(t, 1, mockClient.cleanupCalls, "CleanupGrove should only be called for non-embedded brokers")
	assert.Contains(t, mockClient.cleanupSlugs, "cleanup-embedded")
}

// TestDeleteGrove_GitBacked_NoCleanupDispatched verifies that deleting a git-backed
// grove does NOT trigger broker cleanup (those directories are externally managed).
func TestDeleteGrove_GitBacked_NoCleanupDispatched(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a git-backed grove
	grove := &store.Grove{
		ID:        "grove-git-nocleanup",
		Slug:      "git-nocleanup",
		Name:      "Git No Cleanup Grove",
		GitRemote: "github.com/test/nocleanup",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create a broker and link as provider
	broker := &store.RuntimeBroker{
		ID:       "broker-git-nocleanup",
		Slug:     "git-nocleanup-broker",
		Name:     "Git NoCleanup Broker",
		Status:   store.BrokerStatusOnline,
		Endpoint: "http://broker:9800",
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))
	require.NoError(t, s.AddGroveProvider(ctx, &store.GroveProvider{
		GroveID:  grove.ID,
		BrokerID: broker.ID,
		LinkedBy: "test",
	}))

	// Set up mock client and dispatcher
	mockClient := &mockRuntimeBrokerClient{}
	disp := NewHTTPAgentDispatcherWithClient(s, mockClient, false)
	srv.SetDispatcher(disp)

	// Delete grove
	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/groves/"+grove.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// CleanupGrove should NOT be called for git-backed groves
	assert.Equal(t, 0, mockClient.cleanupCalls, "CleanupGrove should not be called for git-backed groves")
}

// TestResolveRuntimeBroker_HubNativeGrove_NoLocalPath verifies that when a broker
// is auto-linked during agent creation for a hub-native grove, LocalPath is NOT
// set. Remote brokers resolve the path themselves via groveSlug.
func TestResolveRuntimeBroker_HubNativeGrove_NoLocalPath(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a runtime broker (not auto-provide — will be explicitly selected)
	broker := &store.RuntimeBroker{
		ID:     "broker-resolve-localpath",
		Slug:   "resolve-localpath-broker",
		Name:   "Resolve LocalPath Broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a hub-native grove with no providers
	grove := &store.Grove{
		ID:   "grove-resolve-localpath",
		Slug: "resolve-localpath",
		Name: "Resolve LocalPath Grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create agent with explicit broker — triggers resolveRuntimeBroker auto-link
	agentBody := map[string]interface{}{
		"name":            "resolve-localpath-agent",
		"groveId":         grove.ID,
		"runtimeBrokerId": broker.ID,
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", agentBody)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	// Verify the auto-linked provider does NOT have LocalPath set
	provider, err := s.GetGroveProvider(ctx, grove.ID, broker.ID)
	require.NoError(t, err, "Broker should have been auto-linked")
	assert.Equal(t, "agent-create", provider.LinkedBy)
	assert.Empty(t, provider.LocalPath,
		"LocalPath should NOT be set when auto-linking during agent creation for hub-native grove")
}

