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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/agent/state"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/store"
	"github.com/ptone/scion-agent/pkg/store/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDispatcher is a mock AgentDispatcher that records DispatchAgentMessage calls.
type recordingDispatcher struct {
	mu       sync.Mutex
	calls    []dispatchCall
	returnErr error
}

type dispatchCall struct {
	Agent     *store.Agent
	Message   string
	Interrupt bool
}

func (d *recordingDispatcher) DispatchAgentMessage(_ context.Context, agent *store.Agent, message string, interrupt bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dispatchCall{Agent: agent, Message: message, Interrupt: interrupt})
	return d.returnErr
}

func (d *recordingDispatcher) getCalls() []dispatchCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]dispatchCall, len(d.calls))
	copy(result, d.calls)
	return result
}

// Implement remaining AgentDispatcher methods as no-ops.
func (d *recordingDispatcher) DispatchAgentCreate(_ context.Context, _ *store.Agent) error { return nil }
func (d *recordingDispatcher) DispatchAgentProvision(_ context.Context, _ *store.Agent) error {
	return nil
}
func (d *recordingDispatcher) DispatchAgentStart(_ context.Context, _ *store.Agent, _ string) error {
	return nil
}
func (d *recordingDispatcher) DispatchAgentStop(_ context.Context, _ *store.Agent) error  { return nil }
func (d *recordingDispatcher) DispatchAgentRestart(_ context.Context, _ *store.Agent) error {
	return nil
}
func (d *recordingDispatcher) DispatchAgentDelete(_ context.Context, _ *store.Agent, _, _, _ bool, _ time.Time) error {
	return nil
}
func (d *recordingDispatcher) DispatchCheckAgentPrompt(_ context.Context, _ *store.Agent) (bool, error) {
	return false, nil
}
func (d *recordingDispatcher) DispatchAgentCreateWithGather(_ context.Context, _ *store.Agent) (*RemoteEnvRequirementsResponse, error) {
	return nil, nil
}
func (d *recordingDispatcher) DispatchFinalizeEnv(_ context.Context, _ *store.Agent, _ map[string]string) error {
	return nil
}

// notificationTestEnv holds all components for a notification test.
type notificationTestEnv struct {
	store      store.Store
	pub        *ChannelEventPublisher
	dispatcher *recordingDispatcher
	nd         *NotificationDispatcher
	grove      *store.Grove
	watched    *store.Agent // the agent being watched
	subscriber *store.Agent // the agent receiving notifications
	sub        *store.NotificationSubscription
}

// setupNotificationTest creates an in-memory SQLite store, event publisher,
// recording dispatcher, grove, watched agent, subscriber agent, and subscription.
func setupNotificationTest(t *testing.T) *notificationTestEnv {
	t.Helper()

	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	require.NoError(t, s.Migrate(context.Background()))
	t.Cleanup(func() { s.Close() })

	pub := NewChannelEventPublisher()
	t.Cleanup(func() { pub.Close() })

	dispatcher := &recordingDispatcher{}

	ctx := context.Background()

	grove := &store.Grove{
		ID:         api.NewUUID(),
		Name:       "Notification Test Grove",
		Slug:       "notif-test-grove",
		Visibility: store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	broker := &store.RuntimeBroker{
		ID:     "broker-1",
		Name:   "Test Broker",
		Slug:   "test-broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	watched := &store.Agent{
		ID:              api.NewUUID(),
		Slug:            "watched-agent",
		Name:            "Watched Agent",
		Template:        "claude",
		GroveID:         grove.ID,
		Phase: string(state.PhaseRunning),
		RuntimeBrokerID: "broker-1",
		Visibility:      store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateAgent(ctx, watched))

	subscriber := &store.Agent{
		ID:              api.NewUUID(),
		Slug:            "subscriber-agent",
		Name:            "Subscriber Agent",
		Template:        "claude",
		GroveID:         grove.ID,
		Phase: string(state.PhaseRunning),
		RuntimeBrokerID: "broker-1",
		Visibility:      store.VisibilityPrivate,
	}
	require.NoError(t, s.CreateAgent(ctx, subscriber))

	sub := &store.NotificationSubscription{
		ID:              api.NewUUID(),
		AgentID:         watched.ID,
		SubscriberType:  store.SubscriberTypeAgent,
		SubscriberID:    subscriber.Slug,
		GroveID:         grove.ID,
		TriggerStatuses: []string{"COMPLETED", "WAITING_FOR_INPUT"},
		CreatedAt:       time.Now(),
		CreatedBy:       "test",
	}
	require.NoError(t, s.CreateNotificationSubscription(ctx, sub))

	nd := NewNotificationDispatcher(s, pub, func() AgentDispatcher { return dispatcher })

	return &notificationTestEnv{
		store:      s,
		pub:        pub,
		dispatcher: dispatcher,
		nd:         nd,
		grove:      grove,
		watched:    watched,
		subscriber: subscriber,
		sub:        sub,
	}
}

// publishStatus publishes an agent status event via the event publisher.
func (env *notificationTestEnv) publishStatus(activity string) {
	env.pub.PublishAgentStatus(context.Background(), &store.Agent{
		ID:       env.watched.ID,
		Slug:     env.watched.Slug,
		GroveID:  env.grove.ID,
		Phase:    string(state.PhaseRunning),
		Activity: activity,
	})
}

func TestNotificationDispatcher_HappyPath(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	calls := env.dispatcher.getCalls()
	assert.Equal(t, env.subscriber.ID, calls[0].Agent.ID)
	assert.Contains(t, calls[0].Message, "watched-agent has reached a state of COMPLETED")
	assert.False(t, calls[0].Interrupt)

	// Verify notification was stored
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.Equal(t, "COMPLETED", notifs[0].Status)
	assert.True(t, notifs[0].Dispatched)
}

func TestNotificationDispatcher_NonMatchingStatus(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("running")

	// Give time for event to be processed
	time.Sleep(200 * time.Millisecond)

	assert.Empty(t, env.dispatcher.getCalls())

	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

func TestNotificationDispatcher_Dedup(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	// Publish same status again
	env.publishStatus("completed")

	// Wait and verify no additional dispatch
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, env.dispatcher.getCalls(), 1)

	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
}

func TestNotificationDispatcher_DifferentStatuses(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	env.publishStatus("waiting_for_input")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 2
	}, 2*time.Second, 50*time.Millisecond)

	calls := env.dispatcher.getCalls()
	assert.Contains(t, calls[0].Message, "COMPLETED")
	assert.Contains(t, calls[1].Message, "WAITING_FOR_INPUT")
}

func TestNotificationDispatcher_NoSubscriptions(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	// Publish status for an agent with no subscriptions
	env.pub.PublishAgentStatus(context.Background(), &store.Agent{
		ID:       api.NewUUID(), // different agent
		GroveID:  env.grove.ID,
		Phase:    string(state.PhaseRunning),
		Activity: "completed",
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, env.dispatcher.getCalls())
}

func TestNotificationDispatcher_SubscriberAgentNotFound(t *testing.T) {
	env := setupNotificationTest(t)

	// Delete the subscriber agent
	require.NoError(t, env.store.DeleteAgent(context.Background(), env.subscriber.ID))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// No dispatch call since subscriber not found
	assert.Empty(t, env.dispatcher.getCalls())

	// Notification should still be stored
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.False(t, notifs[0].Dispatched) // not dispatched since subscriber was not found
}

func TestNotificationDispatcher_SubscriberNoBroker(t *testing.T) {
	env := setupNotificationTest(t)

	// Update subscriber to have no broker
	env.subscriber.RuntimeBrokerID = ""
	require.NoError(t, env.store.UpdateAgent(context.Background(), env.subscriber))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// No DispatchAgentMessage call
	assert.Empty(t, env.dispatcher.getCalls())

	// Notification should be stored and marked dispatched (best-effort)
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.True(t, notifs[0].Dispatched)
}

func TestNotificationDispatcher_DispatchFailure(t *testing.T) {
	env := setupNotificationTest(t)
	env.dispatcher.returnErr = fmt.Errorf("broker unavailable")

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	// Even on dispatch failure, notification is stored and marked dispatched (best-effort)
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.True(t, notifs[0].Dispatched)
}

func TestNotificationDispatcher_UserSubscriber(t *testing.T) {
	env := setupNotificationTest(t)

	// Replace the agent subscription with a user subscription
	require.NoError(t, env.store.DeleteNotificationSubscription(context.Background(), env.sub.ID))
	userSub := &store.NotificationSubscription{
		ID:              api.NewUUID(),
		AgentID:         env.watched.ID,
		SubscriberType:  store.SubscriberTypeUser,
		SubscriberID:    "user-123",
		GroveID:         env.grove.ID,
		TriggerStatuses: []string{"COMPLETED"},
		CreatedAt:       time.Now(),
		CreatedBy:       "test",
	}
	require.NoError(t, env.store.CreateNotificationSubscription(context.Background(), userSub))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// No dispatch call for user subscribers
	assert.Empty(t, env.dispatcher.getCalls())

	// But notification should be stored
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeUser, "user-123", false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.Equal(t, "COMPLETED", notifs[0].Status)
}

func TestNotificationDispatcher_Stop(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	env.nd.Stop()

	// Publish after stop — should not panic or process
	env.publishStatus("completed")

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, env.dispatcher.getCalls())
}

func TestNotificationDispatcher_NilDispatcher(t *testing.T) {
	env := setupNotificationTest(t)
	// Replace with a nil-returning getter to simulate no dispatcher available
	env.nd.getDispatcher = func() AgentDispatcher { return nil }

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// No dispatch calls since dispatcher is nil
	assert.Empty(t, env.dispatcher.getCalls())

	// Notification should be stored and marked dispatched (best-effort)
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 1)
	assert.True(t, notifs[0].Dispatched)
}

func TestNotificationDispatcher_CaseInsensitiveStatus(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	// Publish lowercase status — should match
	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	// Stored as uppercase
	notifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	assert.Equal(t, "COMPLETED", notifs[0].Status)
}

func TestFormatNotificationMessage(t *testing.T) {
	tests := []struct {
		name     string
		agent    *store.Agent
		status   string
		expected string
	}{
		{
			name:     "COMPLETED without summary",
			agent:    &store.Agent{Slug: "worker"},
			status:   "COMPLETED",
			expected: "worker has reached a state of COMPLETED",
		},
		{
			name:     "COMPLETED with summary",
			agent:    &store.Agent{Slug: "worker", TaskSummary: "Finished refactoring"},
			status:   "COMPLETED",
			expected: "worker has reached a state of COMPLETED: Finished refactoring",
		},
		{
			name:     "WAITING_FOR_INPUT without message",
			agent:    &store.Agent{Slug: "helper"},
			status:   "WAITING_FOR_INPUT",
			expected: "helper is WAITING_FOR_INPUT",
		},
		{
			name:     "WAITING_FOR_INPUT with message",
			agent:    &store.Agent{Slug: "helper", Message: "Need API key"},
			status:   "WAITING_FOR_INPUT",
			expected: "helper is WAITING_FOR_INPUT: Need API key",
		},
		{
			name:     "LIMITS_EXCEEDED without message",
			agent:    &store.Agent{Slug: "cruncher"},
			status:   "LIMITS_EXCEEDED",
			expected: "cruncher has reached a state of LIMITS_EXCEEDED",
		},
		{
			name:     "LIMITS_EXCEEDED with message",
			agent:    &store.Agent{Slug: "cruncher", Message: "Token limit reached"},
			status:   "LIMITS_EXCEEDED",
			expected: "cruncher has reached a state of LIMITS_EXCEEDED: Token limit reached",
		},
		{
			name:     "Unknown status",
			agent:    &store.Agent{Slug: "bot"},
			status:   "ERROR",
			expected: "bot has reached status: ERROR",
		},
		{
			name:     "Case insensitive input",
			agent:    &store.Agent{Slug: "bot"},
			status:   "completed",
			expected: "bot has reached a state of COMPLETED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNotificationMessage(tt.agent, tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNotificationDispatcher_MultipleSubscribers(t *testing.T) {
	env := setupNotificationTest(t)

	// Add a user subscription in addition to the existing agent subscription
	userSub := &store.NotificationSubscription{
		ID:              api.NewUUID(),
		AgentID:         env.watched.ID,
		SubscriberType:  store.SubscriberTypeUser,
		SubscriberID:    "user-456",
		GroveID:         env.grove.ID,
		TriggerStatuses: []string{"COMPLETED"},
		CreatedAt:       time.Now(),
		CreatedBy:       "test",
	}
	require.NoError(t, env.store.CreateNotificationSubscription(context.Background(), userSub))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	// Agent subscriber should get a dispatch
	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	// Both notifications should be stored
	agentNotifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeAgent, env.subscriber.Slug, false)
	require.NoError(t, err)
	assert.Len(t, agentNotifs, 1)

	userNotifs, err := env.store.GetNotifications(context.Background(), store.SubscriberTypeUser, "user-456", false)
	require.NoError(t, err)
	assert.Len(t, userNotifs, 1)
}

func TestNotificationDispatcher_PublisherClosed(t *testing.T) {
	env := setupNotificationTest(t)
	env.nd.Start()
	defer env.nd.Stop()

	// Close the publisher — goroutine should exit cleanly
	env.pub.Close()

	// Give time for goroutine to exit
	time.Sleep(200 * time.Millisecond)

	// No panic or deadlock — test passes if we get here
}

func TestNotificationDispatcher_CompletedWithTaskSummary(t *testing.T) {
	env := setupNotificationTest(t)

	// Update the watched agent with a task summary
	env.watched.TaskSummary = "Refactored auth module"
	require.NoError(t, env.store.UpdateAgent(context.Background(), env.watched))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("completed")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	calls := env.dispatcher.getCalls()
	assert.Equal(t, "watched-agent has reached a state of COMPLETED: Refactored auth module", calls[0].Message)
}

func TestNotificationDispatcher_WaitingForInputWithMessage(t *testing.T) {
	env := setupNotificationTest(t)

	// Update the watched agent with a message
	env.watched.Message = "Please approve the PR"
	require.NoError(t, env.store.UpdateAgent(context.Background(), env.watched))

	env.nd.Start()
	defer env.nd.Stop()

	env.publishStatus("waiting_for_input")

	require.Eventually(t, func() bool {
		return len(env.dispatcher.getCalls()) == 1
	}, 2*time.Second, 50*time.Millisecond)

	calls := env.dispatcher.getCalls()
	assert.Equal(t, "watched-agent is WAITING_FOR_INPUT: Please approve the PR", calls[0].Message)
}
