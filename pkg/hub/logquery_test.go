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
	"testing"
	"time"

	gcplog "cloud.google.com/go/logging"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
	logpb "google.golang.org/genproto/googleapis/logging/v2"
)

func TestBuildLogFilter(t *testing.T) {
	tests := []struct {
		name     string
		opts     LogQueryOptions
		expected string
	}{
		{
			name:     "empty options",
			opts:     LogQueryOptions{},
			expected: "",
		},
		{
			name: "agent ID only",
			opts: LogQueryOptions{
				AgentID: "agent-123",
			},
			expected: `labels.agent_id = "agent-123"`,
		},
		{
			name: "agent ID with severity",
			opts: LogQueryOptions{
				AgentID:  "agent-123",
				Severity: "ERROR",
			},
			expected: `labels.agent_id = "agent-123" AND severity >= ERROR`,
		},
		{
			name: "all filters",
			opts: LogQueryOptions{
				AgentID:  "agent-123",
				Since:    time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC),
				Until:    time.Date(2026, 3, 7, 11, 0, 0, 0, time.UTC),
				Severity: "INFO",
			},
			expected: `labels.agent_id = "agent-123" AND timestamp >= "2026-03-07T10:00:00Z" AND timestamp < "2026-03-07T11:00:00Z" AND severity >= INFO`,
		},
		{
			name: "severity case normalization",
			opts: LogQueryOptions{
				Severity: "warning",
			},
			expected: `severity >= WARNING`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildLogFilter(tt.opts)
			if result != tt.expected {
				t.Errorf("BuildLogFilter() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConvertLogEntry(t *testing.T) {
	t.Run("string payload", func(t *testing.T) {
		ts := time.Date(2026, 3, 7, 10, 15, 32, 0, time.UTC)
		entry := &gcplog.Entry{
			Timestamp: ts,
			Severity:  gcplog.Info,
			Payload:   "Agent started processing task",
			Labels: map[string]string{
				"agent_id": "abc123",
				"grove_id": "my-grove",
			},
			InsertID: "insert-1",
		}

		result := ConvertLogEntry(entry)

		if result.Timestamp != ts {
			t.Errorf("Timestamp = %v, want %v", result.Timestamp, ts)
		}
		if result.Severity != "Info" {
			t.Errorf("Severity = %q, want %q", result.Severity, "Info")
		}
		if result.Message != "Agent started processing task" {
			t.Errorf("Message = %q, want %q", result.Message, "Agent started processing task")
		}
		if result.InsertID != "insert-1" {
			t.Errorf("InsertID = %q, want %q", result.InsertID, "insert-1")
		}
		if result.Labels["agent_id"] != "abc123" {
			t.Errorf("Labels[agent_id] = %q, want %q", result.Labels["agent_id"], "abc123")
		}
	})

	t.Run("map payload with message", func(t *testing.T) {
		entry := &gcplog.Entry{
			Timestamp: time.Now(),
			Severity:  gcplog.Error,
			Payload: map[string]interface{}{
				"message":   "Failed to route message",
				"subsystem": "hub.dispatch",
				"error":     "connection refused",
			},
			InsertID: "insert-2",
		}

		result := ConvertLogEntry(entry)

		if result.Message != "Failed to route message" {
			t.Errorf("Message = %q, want %q", result.Message, "Failed to route message")
		}
		if result.JSONPayload["subsystem"] != "hub.dispatch" {
			t.Errorf("JSONPayload[subsystem] = %v, want %q", result.JSONPayload["subsystem"], "hub.dispatch")
		}
	})

	t.Run("source location", func(t *testing.T) {
		entry := &gcplog.Entry{
			Timestamp: time.Now(),
			Severity:  gcplog.Warning,
			Payload:   "test",
			InsertID:  "insert-3",
			SourceLocation: &logpb.LogEntrySourceLocation{
				File:     "pkg/hub/dispatch.go",
				Line:     342,
				Function: "github.com/ptone/scion-agent/pkg/hub.(*Server).dispatch",
			},
		}

		result := ConvertLogEntry(entry)

		if result.SourceLocation == nil {
			t.Fatal("SourceLocation is nil")
		}
		if result.SourceLocation.File != "pkg/hub/dispatch.go" {
			t.Errorf("File = %q, want %q", result.SourceLocation.File, "pkg/hub/dispatch.go")
		}
		if result.SourceLocation.Line != "342" {
			t.Errorf("Line = %q, want %q", result.SourceLocation.Line, "342")
		}
	})

	t.Run("resource info", func(t *testing.T) {
		entry := &gcplog.Entry{
			Timestamp: time.Now(),
			Severity:  gcplog.Info,
			Payload:   "test",
			InsertID:  "insert-4",
			Resource: &mrpb.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "12345",
					"zone":        "us-central1-a",
				},
			},
		}

		result := ConvertLogEntry(entry)

		if result.Resource == nil {
			t.Fatal("Resource is nil")
		}
		if result.Resource["type"] != "gce_instance" {
			t.Errorf("Resource.type = %v, want %q", result.Resource["type"], "gce_instance")
		}
		labels, ok := result.Resource["labels"].(map[string]interface{})
		if !ok {
			t.Fatal("Resource.labels is not a map")
		}
		if labels["instance_id"] != "12345" {
			t.Errorf("Resource.labels.instance_id = %v, want %q", labels["instance_id"], "12345")
		}
	})
}

func TestLogQueryOptionsTailCapping(t *testing.T) {
	// Test that tail defaults and caps are applied correctly
	// This is tested indirectly through the Query method, but we can
	// verify the BuildLogFilter doesn't add tail to the filter.
	opts := LogQueryOptions{
		AgentID: "test",
		Tail:    5000, // Over the 1000 cap
	}
	filter := BuildLogFilter(opts)
	// Tail should not appear in the filter string
	if filter != `labels.agent_id = "test"` {
		t.Errorf("BuildLogFilter() = %q, tail should not be in filter", filter)
	}
}
