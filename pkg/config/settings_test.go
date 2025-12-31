package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSettings(t *testing.T) {
	// Create temporary directories for global and grove settings
	tmpDir := t.TempDir()

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	groveDir := filepath.Join(tmpDir, "my-grove")
	groveScionDir := filepath.Join(groveDir, ".scion")
	if err := os.MkdirAll(groveScionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Test defaults
	s, err := LoadSettings(groveScionDir)
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if s.ActiveProfile != "local" {
		t.Errorf("expected active profile 'local', got '%s'", s.ActiveProfile)
	}
	if s.DefaultTemplate != "gemini" {
		t.Errorf("expected default template 'gemini', got '%s'", s.DefaultTemplate)
	}

	// 2. Test Global overrides
	globalSettings := `{
		"active_profile": "prod",
		"default_template": "claude",
		"runtimes": {
			"kubernetes": { "namespace": "scion-global" }
		},
		"profiles": {
			"prod": { "runtime": "kubernetes", "tmux": false }
		}
	}`
	globalScionDir := filepath.Join(tmpDir, ".scion")
	if err := os.MkdirAll(globalScionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalScionDir, "settings.json"), []byte(globalSettings), 0644); err != nil {
		t.Fatal(err)
	}

	s, err = LoadSettings(groveScionDir)
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if s.ActiveProfile != "prod" {
		t.Errorf("expected global override active_profile 'prod', got '%s'", s.ActiveProfile)
	}
	if s.DefaultTemplate != "claude" {
		t.Errorf("expected global override template 'claude', got '%s'", s.DefaultTemplate)
	}
	if s.Runtimes["kubernetes"].Namespace != "scion-global" {
		t.Errorf("expected global override runtime namespace 'scion-global', got '%s'", s.Runtimes["kubernetes"].Namespace)
	}

	// 3. Test Grove overrides
	groveSettings := `{
		"active_profile": "local-dev",
		"profiles": {
			"local-dev": { "runtime": "local", "tmux": true }
		}
	}`
	if err := os.WriteFile(filepath.Join(groveScionDir, "settings.json"), []byte(groveSettings), 0644); err != nil {
		t.Fatal(err)
	}

	s, err = LoadSettings(groveScionDir)
	if err != nil {
		t.Fatalf("LoadSettings failed: %v", err)
	}
	if s.ActiveProfile != "local-dev" {
		t.Errorf("expected grove override active_profile 'local-dev', got '%s'", s.ActiveProfile)
	}
	// Template should still be claude from global
	if s.DefaultTemplate != "claude" {
		t.Errorf("expected inherited global template 'claude', got '%s'", s.DefaultTemplate)
	}
}

func TestUpdateSetting(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	groveDir := filepath.Join(tmpDir, "my-grove")
	groveScionDir := filepath.Join(groveDir, ".scion")
	if err := os.MkdirAll(groveScionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Update local setting
	err := UpdateSetting(groveScionDir, "active_profile", "kubernetes", false)
	if err != nil {
		t.Fatalf("UpdateSetting failed: %v", err)
	}

	// Verify file content
	content, err := os.ReadFile(filepath.Join(groveScionDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"active_profile": "kubernetes"`) {
		t.Errorf("expected file to contain active_profile: kubernetes, got %s", string(content))
	}

	// Update default_template
	err = UpdateSetting(groveScionDir, "default_template", "my-template", false)
	if err != nil {
		t.Fatalf("UpdateSetting default_template failed: %v", err)
	}
	content, _ = os.ReadFile(filepath.Join(groveScionDir, "settings.json"))
	if !strings.Contains(string(content), `"default_template": "my-template"`) {
		t.Errorf("expected file to contain default_template: my-template, got %s", string(content))
	}
}

func TestResolve(t *testing.T) {
	s := &Settings{
		ActiveProfile: "local",
		Runtimes: map[string]RuntimeConfig{
			"docker": {Host: "unix:///var/run/docker.sock"},
		},
		Harnesses: map[string]HarnessConfig{
			"gemini": {Image: "gemini:latest", User: "root"},
		},
		Profiles: map[string]ProfileConfig{
			"local": {
				Runtime: "docker",
				HarnessOverrides: map[string]HarnessOverride{
					"gemini": {Image: "gemini:dev"},
				},
			},
		},
	}

	runtime, name, err := s.ResolveRuntime("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "docker" {
		t.Errorf("expected runtime name docker, got %s", name)
	}
	if runtime.Host != "unix:///var/run/docker.sock" {
		t.Errorf("expected host unix:///var/run/docker.sock, got %s", runtime.Host)
	}

	harness, err := s.ResolveHarness("", "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if harness.Image != "gemini:dev" {
		t.Errorf("expected image gemini:dev, got %s", harness.Image)
	}
	if harness.User != "root" {
		t.Errorf("expected user root, got %s", harness.User)
	}
}