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

package cmd

import (
	"testing"
	"time"
)

func TestParseSinceFlag(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{
			name:    "RFC3339 timestamp",
			value:   "2026-03-07T10:00:00Z",
			wantErr: false,
		},
		{
			name:    "RFC3339 with timezone offset",
			value:   "2026-03-07T10:00:00-07:00",
			wantErr: false,
		},
		{
			name:    "duration 1h",
			value:   "1h",
			wantErr: false,
		},
		{
			name:    "duration 30m",
			value:   "30m",
			wantErr: false,
		},
		{
			name:    "duration 2h30m",
			value:   "2h30m",
			wantErr: false,
		},
		{
			name:    "invalid value",
			value:   "yesterday",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSinceFlag(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseSinceFlag(%q) expected error, got nil", tt.value)
				}
				return
			}
			if err != nil {
				t.Errorf("parseSinceFlag(%q) error = %v", tt.value, err)
				return
			}
			if result == "" {
				t.Errorf("parseSinceFlag(%q) returned empty string", tt.value)
			}

			// For duration values, verify the result is a valid timestamp in the past
			if tt.value == "1h" || tt.value == "30m" || tt.value == "2h30m" {
				parsed, err := time.Parse(time.RFC3339Nano, result)
				if err != nil {
					t.Errorf("parseSinceFlag(%q) returned invalid RFC3339: %q", tt.value, result)
					return
				}
				if parsed.After(time.Now()) {
					t.Errorf("parseSinceFlag(%q) returned future time: %v", tt.value, parsed)
				}
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"INFO", 8, "INFO    "},
		{"ERROR", 8, "ERROR   "},
		{"CRITICAL", 8, "CRITICAL"},
		{"DEBUG", 5, "DEBUG"},
		{"WARN", 3, "WARN"},
	}

	for _, tt := range tests {
		result := padRight(tt.input, tt.width)
		if result != tt.expected {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.input, tt.width, result, tt.expected)
		}
	}
}
