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

	gcplog "cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
)

// LogQueryService queries Google Cloud Logging for structured log entries.
type LogQueryService struct {
	client    *logadmin.Client
	projectID string
}

// CloudLogEntry represents a structured log entry from Cloud Logging.
type CloudLogEntry struct {
	Timestamp      time.Time              `json:"timestamp"`
	Severity       string                 `json:"severity"`
	Message        string                 `json:"message"`
	Labels         map[string]string      `json:"labels,omitempty"`
	Resource       map[string]interface{} `json:"resource,omitempty"`
	JSONPayload    map[string]interface{} `json:"jsonPayload,omitempty"`
	InsertID       string                 `json:"insertId"`
	SourceLocation *LogSourceLocation     `json:"sourceLocation,omitempty"`
}

// LogSourceLocation identifies the source code location of a log entry.
type LogSourceLocation struct {
	File     string `json:"file,omitempty"`
	Line     string `json:"line,omitempty"`
	Function string `json:"function,omitempty"`
}

// LogQueryOptions configures a Cloud Logging query.
type LogQueryOptions struct {
	AgentID   string
	GroveID   string
	Tail      int
	Since     time.Time
	Until     time.Time
	Severity  string
	PageToken string
}

// LogQueryResult contains the result of a log query.
type LogQueryResult struct {
	Entries       []CloudLogEntry `json:"entries"`
	NextPageToken string          `json:"nextPageToken,omitempty"`
	HasMore       bool            `json:"hasMore"`
}

// NewLogQueryService creates a LogQueryService. Returns an error if the
// logadmin client cannot be created.
func NewLogQueryService(ctx context.Context, projectID string) (*LogQueryService, error) {
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID is required")
	}

	client, err := logadmin.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("creating logadmin client: %w", err)
	}

	return &LogQueryService{
		client:    client,
		projectID: projectID,
	}, nil
}

// Close releases resources held by the LogQueryService.
func (s *LogQueryService) Close() error {
	return s.client.Close()
}

// Query returns log entries matching the given options.
func (s *LogQueryService) Query(ctx context.Context, opts LogQueryOptions) (*LogQueryResult, error) {
	filter := BuildLogFilter(opts)

	tail := opts.Tail
	if tail <= 0 {
		tail = 200
	}
	if tail > 1000 {
		tail = 1000
	}

	slog.Debug("querying cloud logs", "filter", filter, "tail", tail)

	it := s.client.Entries(ctx,
		logadmin.Filter(filter),
		logadmin.NewestFirst(),
	)

	var entries []CloudLogEntry
	for len(entries) < tail {
		entry, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading log entries: %w", err)
		}
		entries = append(entries, ConvertLogEntry(entry))
	}

	return &LogQueryResult{
		Entries: entries,
	}, nil
}

// BuildLogFilter constructs a Cloud Logging filter string from the options.
func BuildLogFilter(opts LogQueryOptions) string {
	var parts []string

	if opts.AgentID != "" {
		parts = append(parts, fmt.Sprintf(`labels.agent_id = %q`, opts.AgentID))
	}
	if !opts.Since.IsZero() {
		parts = append(parts, fmt.Sprintf(`timestamp >= %q`, opts.Since.Format(time.RFC3339Nano)))
	}
	if !opts.Until.IsZero() {
		parts = append(parts, fmt.Sprintf(`timestamp < %q`, opts.Until.Format(time.RFC3339Nano)))
	}
	if opts.Severity != "" {
		parts = append(parts, fmt.Sprintf(`severity >= %s`, strings.ToUpper(opts.Severity)))
	}

	return strings.Join(parts, " AND ")
}

// ConvertLogEntry converts a Cloud Logging Entry to a CloudLogEntry.
func ConvertLogEntry(entry *gcplog.Entry) CloudLogEntry {
	e := CloudLogEntry{
		Timestamp: entry.Timestamp,
		Severity:  entry.Severity.String(),
		Labels:    entry.Labels,
		InsertID:  entry.InsertID,
	}

	// Extract payload
	switch p := entry.Payload.(type) {
	case string:
		e.Message = p
	case map[string]interface{}:
		e.JSONPayload = p
		if msg, ok := p["message"].(string); ok {
			e.Message = msg
		}
	default:
		// For other types, try JSON marshaling to extract a map
		if p != nil {
			data, err := json.Marshal(p)
			if err == nil {
				payload := make(map[string]interface{})
				if json.Unmarshal(data, &payload) == nil {
					e.JSONPayload = payload
					if msg, ok := payload["message"].(string); ok {
						e.Message = msg
					}
				}
			}
		}
	}

	// Extract resource info
	if res := entry.Resource; res != nil {
		resMap := map[string]interface{}{
			"type": res.GetType(),
		}
		if labels := res.GetLabels(); len(labels) > 0 {
			labelsMap := make(map[string]interface{}, len(labels))
			for k, v := range labels {
				labelsMap[k] = v
			}
			resMap["labels"] = labelsMap
		}
		e.Resource = resMap
	}

	// Extract source location
	if sl := entry.SourceLocation; sl != nil {
		e.SourceLocation = &LogSourceLocation{
			File:     sl.GetFile(),
			Line:     fmt.Sprintf("%d", sl.GetLine()),
			Function: sl.GetFunction(),
		}
	}

	return e
}
