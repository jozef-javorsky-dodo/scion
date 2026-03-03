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
	"log/slog"
	"net/http"

	"github.com/ptone/scion-agent/pkg/store"
)

// handleNotifications handles GET /api/v1/notifications.
// Lists notifications for the authenticated user.
//
// Without agentId: returns flat []Notification array (existing tray behavior).
// With ?agentId=X: returns { userNotifications: [...], agentNotifications: [...] }.
func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowed(w)
		return
	}

	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	acknowledged := r.URL.Query().Get("acknowledged")
	onlyUnacknowledged := acknowledged != "true"

	agentID := r.URL.Query().Get("agentId")

	if agentID == "" {
		// Existing behaviour: flat array of user notifications
		notifs, err := s.store.GetNotifications(r.Context(), "user", user.ID(), onlyUnacknowledged)
		if err != nil {
			writeErrorFromErr(w, err, "")
			return
		}
		writeJSON(w, http.StatusOK, notifs)
		return
	}

	// Agent-scoped: return combined response
	userNotifs, err := s.store.GetNotificationsByAgent(r.Context(), agentID, "user", user.ID(), onlyUnacknowledged)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	agentNotifs, err := s.store.GetNotifications(r.Context(), "agent", agentID, false)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	writeJSON(w, http.StatusOK, agentNotificationsResponse{
		UserNotifications:  userNotifs,
		AgentNotifications: agentNotifs,
	})
}

// agentNotificationsResponse is returned when ?agentId= is provided.
type agentNotificationsResponse struct {
	UserNotifications  []store.Notification `json:"userNotifications"`
	AgentNotifications []store.Notification `json:"agentNotifications"`
}

// handleNotificationRoutes handles requests under /api/v1/notifications/.
// Routes:
//   - POST /api/v1/notifications/ack-all: Acknowledge all notifications
//   - POST /api/v1/notifications/{id}/ack: Acknowledge a single notification
func (s *Server) handleNotificationRoutes(w http.ResponseWriter, r *http.Request) {
	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	id, action := extractAction(r, "/api/v1/notifications")

	// POST /api/v1/notifications/ack-all
	if id == "ack-all" && r.Method == http.MethodPost {
		if err := s.store.AcknowledgeAllNotifications(r.Context(), "user", user.ID()); err != nil {
			writeErrorFromErr(w, err, "")
			return
		}
		slog.Info("All notifications acknowledged", "userID", user.ID())
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// POST /api/v1/notifications/{id}/ack
	if id != "" && action == "ack" && r.Method == http.MethodPost {
		if err := s.store.AcknowledgeNotification(r.Context(), id); err != nil {
			writeErrorFromErr(w, err, "")
			return
		}
		slog.Info("Notification acknowledged", "notificationID", id, "userID", user.ID())
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if id == "" {
		NotFound(w, "Notification")
		return
	}

	MethodNotAllowed(w)
}
