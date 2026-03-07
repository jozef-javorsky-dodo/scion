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
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"

	"github.com/ptone/scion-agent/pkg/store"
)

// handleAgentCloudLogs handles GET /api/v1/agents/{id}/cloud-logs
// and GET /api/v1/groves/{groveId}/agents/{agentId}/cloud-logs
func (s *Server) handleAgentCloudLogs(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		MethodNotAllowed(w)
		return
	}

	if s.logQueryService == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"Cloud Logging is not configured", nil)
		return
	}

	ctx := r.Context()

	// Verify agent exists and caller has read access
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	if userIdent := GetUserIdentityFromContext(ctx); userIdent != nil {
		decision := s.authzService.CheckAccess(ctx, userIdent, agentResource(agent), ActionRead)
		if !decision.Allowed {
			writeError(w, http.StatusForbidden, ErrCodeForbidden, "Access denied", nil)
			return
		}
	}
	if agentIdent := GetAgentIdentityFromContext(ctx); agentIdent != nil {
		if agent.GroveID != agentIdent.GroveID() {
			NotFound(w, "Agent")
			return
		}
	}

	// Parse query parameters
	query := r.URL.Query()
	opts := LogQueryOptions{
		AgentID: agent.ID,
		GroveID: agent.GroveID,
	}

	if v := query.Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.Tail = n
		}
	}
	if v := query.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			opts.Since = t
		}
	}
	if v := query.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			opts.Until = t
		}
	}
	if v := query.Get("severity"); v != "" {
		opts.Severity = v
	}

	result, err := s.logQueryService.Query(ctx, opts)
	if err != nil {
		slog.Error("cloud log query failed", "agentID", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError,
			"Failed to query cloud logs", nil)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleAgentCloudLogsStream handles GET /api/v1/agents/{id}/cloud-logs/stream
// and GET /api/v1/groves/{groveId}/agents/{agentId}/cloud-logs/stream
// It returns an SSE stream of log entries via a server-side polling loop.
func (s *Server) handleAgentCloudLogsStream(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		MethodNotAllowed(w)
		return
	}

	if s.logQueryService == nil {
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"Cloud Logging is not configured", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// Verify agent exists and caller has read access
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	if userIdent := GetUserIdentityFromContext(ctx); userIdent != nil {
		decision := s.authzService.CheckAccess(ctx, userIdent, agentResource(agent), ActionRead)
		if !decision.Allowed {
			writeError(w, http.StatusForbidden, ErrCodeForbidden, "Access denied", nil)
			return
		}
	}

	// Parse severity filter
	severity := r.URL.Query().Get("severity")

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// Start polling loop: query every 3 seconds with a sliding cursor
	cursor := time.Now().Add(-1 * time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	pollTicker := time.NewTicker(3 * time.Second)
	defer pollTicker.Stop()

	// Server-side timeout: 10 minutes
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-pollTicker.C:
			entries, err := s.pollCloudLogs(ctx, agent.ID, cursor, severity)
			if err != nil {
				slog.Error("cloud log stream poll failed", "agentID", agentID, "error", err)
				continue
			}
			for _, entry := range entries {
				data, err := json.Marshal(entry)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
				flusher.Flush()
				if entry.Timestamp.After(cursor) {
					cursor = entry.Timestamp
				}
			}
		case <-heartbeat.C:
			fmt.Fprintf(w, ":heartbeat %d\n\n", time.Now().UnixMilli())
			flusher.Flush()
		case <-timeout.C:
			fmt.Fprintf(w, "event: timeout\ndata: {\"message\":\"stream timeout, please reconnect\"}\n\n")
			flusher.Flush()
			return
		case <-ctx.Done():
			return
		}
	}
}

// pollCloudLogs queries for new log entries since the cursor timestamp.
func (s *Server) pollCloudLogs(ctx context.Context, agentID string, since time.Time, severity string) ([]CloudLogEntry, error) {
	opts := LogQueryOptions{
		AgentID:  agentID,
		Since:    since.Add(time.Nanosecond), // Exclusive: skip the cursor entry itself
		Tail:     100,
		Severity: severity,
	}

	filter := BuildLogFilter(opts)

	it := s.logQueryService.client.Entries(ctx,
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
	)

	var entries []CloudLogEntry
	for len(entries) < 100 {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, ConvertLogEntry(entry))
	}

	// Reverse to chronological order for streaming
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
}

// resolveGroveAgent resolves an agent by slug or ID within a grove, returning
// the agent if found and it belongs to the specified grove.
func (s *Server) resolveGroveAgent(ctx context.Context, groveID, agentID string) (*store.Agent, error) {
	agent, err := s.store.GetAgentBySlug(ctx, groveID, agentID)
	if err != nil {
		if err == store.ErrNotFound {
			agent, err = s.store.GetAgent(ctx, agentID)
			if err != nil {
				return nil, err
			}
			if agent.GroveID != groveID {
				return nil, store.ErrNotFound
			}
			return agent, nil
		}
		return nil, err
	}
	return agent, nil
}
