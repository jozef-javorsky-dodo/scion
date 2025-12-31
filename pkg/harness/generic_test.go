package harness

import (
	"os"
	"reflect"
	"testing"

	"github.com/ptone/scion-agent/pkg/api"
)

func TestGeneric_Name(t *testing.T) {
	g := &Generic{}
	if g.Name() != "generic" {
		t.Errorf("Expected name 'generic', got '%s'", g.Name())
	}
}

func TestGeneric_DiscoverAuth(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-key")
	os.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	defer os.Unsetenv("GEMINI_API_KEY")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	g := &Generic{}
	auth := g.DiscoverAuth("/tmp")

	if auth.GeminiAPIKey != "test-gemini-key" {
		t.Errorf("Expected GeminiAPIKey 'test-gemini-key', got '%s'", auth.GeminiAPIKey)
	}
	if auth.AnthropicAPIKey != "test-anthropic-key" {
		t.Errorf("Expected AnthropicAPIKey 'test-anthropic-key', got '%s'", auth.AnthropicAPIKey)
	}
}

func TestGeneric_GetEnv(t *testing.T) {
	g := &Generic{}
	auth := api.AuthConfig{
		GeminiAPIKey:         "test-gemini-key",
		AnthropicAPIKey:      "test-anthropic-key",
		GoogleAppCredentials: "/path/to/creds.json",
	}

	env := g.GetEnv("test-agent", "test-user", auth)

	expectedEnv := map[string]string{
		"SCION_AGENT_NAME":             "test-agent",
		"GEMINI_API_KEY":               "test-gemini-key",
		"ANTHROPIC_API_KEY":            "test-anthropic-key",
		"GOOGLE_APPLICATION_CREDENTIALS": "/home/test-user/.config/gcp/application_default_credentials.json",
	}

	for k, v := range expectedEnv {
		if env[k] != v {
			t.Errorf("Expected env[%s] = '%s', got '%s'", k, v, env[k])
		}
	}
}

func TestGeneric_GetCommand(t *testing.T) {
	g := &Generic{}

	cmd := g.GetCommand("test task", false, nil)
	if !reflect.DeepEqual(cmd, []string{"test task"}) {
		t.Errorf("Expected command ['test task'], got %v", cmd)
	}

	cmdWithArgs := g.GetCommand("test task", false, []string{"--arg1"})
	if !reflect.DeepEqual(cmdWithArgs, []string{"--arg1", "test task"}) {
		t.Errorf("Expected command ['--arg1', 'test task'], got %v", cmdWithArgs)
	}

	cmdEmpty := g.GetCommand("", false, nil)
	if len(cmdEmpty) != 0 {
		t.Errorf("Expected empty command, got %v", cmdEmpty)
	}
}

func TestGeneric_DefaultConfigDir(t *testing.T) {
	g := &Generic{}
	if g.DefaultConfigDir() != ".scion" {
		t.Errorf("Expected DefaultConfigDir '.scion', got '%s'", g.DefaultConfigDir())
	}
}
