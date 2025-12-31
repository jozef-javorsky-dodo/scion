package api

import (
	"context"
)

// Harness interface defines the methods a harness must implement
type Harness interface {
	Name() string
	DiscoverAuth(agentHome string) AuthConfig
	GetEnv(agentName string, unixUsername string, auth AuthConfig) map[string]string
	GetCommand(task string, resume bool, baseArgs []string) []string
	PropagateFiles(homeDir, unixUsername string, auth AuthConfig) error
	GetVolumes(unixUsername string, auth AuthConfig) []VolumeMount
	DefaultConfigDir() string
	HasSystemPrompt() bool

	// Provision performs harness-specific setup during agent creation.
	// This is called after templates are copied and scion-agent.json is written.
	Provision(ctx context.Context, agentName, agentHome, agentWorkspace string) error

	// GetEmbedDir returns the name of the directory in pkg/config/embeds/
	// that contains template files for this harness (e.g., "claude", "gemini").
	GetEmbedDir() string
}
