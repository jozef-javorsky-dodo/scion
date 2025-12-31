package harness

import (
	"github.com/ptone/scion-agent/pkg/api"
)

func New(harnessName string) api.Harness {
	switch harnessName {
	case "claude":
		return &ClaudeCode{}
	case "gemini":
		return &GeminiCLI{}
	default:
		return &Generic{}
	}
}
