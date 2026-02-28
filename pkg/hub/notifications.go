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

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/store"
)

// NotificationDispatcher listens for agent status events, matches them against
// notification subscriptions, stores notification records, and dispatches
// messages to subscriber agents.
type NotificationDispatcher struct {
	store         store.Store
	events        *ChannelEventPublisher
	getDispatcher func() AgentDispatcher // lazy getter; dispatcher may be set after startup
	stopCh        chan struct{}
}

// NewNotificationDispatcher creates a new NotificationDispatcher.
// The getDispatcher function is called at dispatch time to resolve the current
// AgentDispatcher, allowing the dispatcher to be set up after the notification
// system starts (e.g. in combined hub+web mode).
func NewNotificationDispatcher(s store.Store, events *ChannelEventPublisher, getDispatcher func() AgentDispatcher) *NotificationDispatcher {
	return &NotificationDispatcher{
		store:         s,
		events:        events,
		getDispatcher: getDispatcher,
		stopCh:        make(chan struct{}),
	}
}

// Start subscribes to agent status events and spawns a goroutine to process them.
func (nd *NotificationDispatcher) Start() {
	ch, unsubscribe := nd.events.Subscribe("grove.>.agent.status")

	go func() {
		defer unsubscribe()
		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				nd.handleEvent(evt)
			case <-nd.stopCh:
				return
			}
		}
	}()

	slog.Info("Notification dispatcher started")
}

// Stop signals the dispatcher goroutine to exit.
func (nd *NotificationDispatcher) Stop() {
	close(nd.stopCh)
	slog.Info("Notification dispatcher stopped")
}

// handleEvent processes a single agent status event.
func (nd *NotificationDispatcher) handleEvent(evt Event) {
	var statusEvt AgentStatusEvent
	if err := json.Unmarshal(evt.Data, &statusEvt); err != nil {
		slog.Error("Failed to unmarshal agent status event", "error", err)
		return
	}

	ctx := context.Background()

	subs, err := nd.store.GetNotificationSubscriptions(ctx, statusEvt.AgentID)
	if err != nil {
		slog.Error("Failed to get notification subscriptions",
			"agentID", statusEvt.AgentID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	// Use activity for matching (notifications trigger on activity changes)
	matchStatus := statusEvt.Activity

	for i := range subs {
		sub := &subs[i]
		if !sub.MatchesStatus(matchStatus) {
			continue
		}

		// Dedup: check if the last notification for this subscription already has this status
		lastStatus, err := nd.store.GetLastNotificationStatus(ctx, sub.ID)
		if err != nil {
			slog.Error("Failed to get last notification status",
				"subscriptionID", sub.ID, "error", err)
			continue
		}
		if strings.EqualFold(lastStatus, matchStatus) {
			continue
		}

		nd.storeAndDispatch(ctx, sub, statusEvt)
	}
}

// storeAndDispatch creates a notification record and dispatches it to the subscriber.
func (nd *NotificationDispatcher) storeAndDispatch(ctx context.Context, sub *store.NotificationSubscription, evt AgentStatusEvent) {
	agent, err := nd.store.GetAgent(ctx, evt.AgentID)
	if err != nil {
		slog.Error("Failed to get agent for notification",
			"agentID", evt.AgentID, "error", err)
		return
	}

	// Use activity for matching/display
	effectiveStatus := evt.Activity

	message := formatNotificationMessage(agent, effectiveStatus)

	notif := &store.Notification{
		ID:             api.NewUUID(),
		SubscriptionID: sub.ID,
		AgentID:        evt.AgentID,
		GroveID:        sub.GroveID,
		SubscriberType: sub.SubscriberType,
		SubscriberID:   sub.SubscriberID,
		Status:         strings.ToUpper(effectiveStatus),
		Message:        message,
		CreatedAt:      time.Now(),
	}

	if err := nd.store.CreateNotification(ctx, notif); err != nil {
		slog.Error("Failed to create notification",
			"subscriptionID", sub.ID, "agentID", evt.AgentID, "error", err)
		return
	}

	switch sub.SubscriberType {
	case store.SubscriberTypeAgent:
		nd.dispatchToAgent(ctx, sub, notif)
	case store.SubscriberTypeUser:
		slog.Debug("User notification stored (dispatch not yet implemented)",
			"subscriberID", sub.SubscriberID, "notificationID", notif.ID)
	default:
		slog.Warn("Unknown subscriber type", "type", sub.SubscriberType)
	}
}

// dispatchToAgent sends a notification message to a subscriber agent.
func (nd *NotificationDispatcher) dispatchToAgent(ctx context.Context, sub *store.NotificationSubscription, notif *store.Notification) {
	subscriber, err := nd.store.GetAgentBySlug(ctx, sub.GroveID, sub.SubscriberID)
	if err != nil {
		slog.Warn("Subscriber agent not found, skipping dispatch",
			"subscriberID", sub.SubscriberID, "groveID", sub.GroveID, "error", err)
		return
	}

	dispatcher := nd.getDispatcher()
	if dispatcher == nil {
		slog.Warn("No dispatcher available, skipping notification dispatch",
			"subscriberID", sub.SubscriberID)
		// Mark dispatched anyway (best-effort)
		if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
			slog.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
		}
		return
	}

	if subscriber.RuntimeBrokerID == "" {
		slog.Warn("Subscriber agent has no runtime broker, skipping dispatch",
			"subscriberID", sub.SubscriberID)
		if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
			slog.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
		}
		return
	}

	if err := dispatcher.DispatchAgentMessage(ctx, subscriber, notif.Message, false); err != nil {
		slog.Error("Failed to dispatch notification to agent",
			"subscriberID", sub.SubscriberID, "error", err)
	}

	// Mark dispatched regardless of success (best-effort)
	if err := nd.store.MarkNotificationDispatched(ctx, notif.ID); err != nil {
		slog.Error("Failed to mark notification dispatched", "notificationID", notif.ID, "error", err)
	}
}

// formatNotificationMessage formats a notification message based on agent state and status.
func formatNotificationMessage(agent *store.Agent, status string) string {
	upper := strings.ToUpper(status)
	switch upper {
	case "COMPLETED":
		msg := fmt.Sprintf("%s has reached a state of COMPLETED", agent.Slug)
		if agent.TaskSummary != "" {
			msg += ": " + agent.TaskSummary
		}
		return msg
	case "WAITING_FOR_INPUT":
		msg := fmt.Sprintf("%s is WAITING_FOR_INPUT", agent.Slug)
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	case "LIMITS_EXCEEDED":
		msg := fmt.Sprintf("%s has reached a state of LIMITS_EXCEEDED", agent.Slug)
		if agent.Message != "" {
			msg += ": " + agent.Message
		}
		return msg
	default:
		return fmt.Sprintf("%s has reached status: %s", agent.Slug, upper)
	}
}
