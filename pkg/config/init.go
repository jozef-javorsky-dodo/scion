package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ptone/scion-agent/pkg/api"
)

//go:embed all:embeds/*
var embedsFS embed.FS

func GetDefaultSettingsData() ([]byte, error) {
	return embedsFS.ReadFile("embeds/default_settings.json")
}

func SeedTemplateDir(templateDir, templateName, harness, embedDir, configDirName string, force bool) error {
	// Create directories
	dirs := []string{
		templateDir,
		filepath.Join(templateDir, configDirName),
		filepath.Join(templateDir, ".config", "gcloud"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Helper to read embedded file
	readEmbed := func(name string) string {
		data, err := embedsFS.ReadFile(filepath.Join("embeds", embedDir, name))
		if err != nil {
			// Fallback to gemini if not found in harness dir
			data, err = embedsFS.ReadFile(filepath.Join("embeds", "gemini", name))
			if err != nil {
				return ""
			}
		}
		return string(data)
	}

	scionJSONStr := readEmbed("scion-agent.json")
	var scionConfig api.ScionConfig
	if err := json.Unmarshal([]byte(scionJSONStr), &scionConfig); err != nil {
		// Fallback if it's not valid JSON, though it should be
		// Maybe log warning?
	} else {
		// TODO clean up this legacy reference to a "default template"
		// templates are now named after harness, and default template is a setting
		if templateName != "" && templateName != "default" {
			if scionConfig.Info == nil {
				scionConfig.Info = &api.AgentInfo{}
			}
			scionConfig.Info.Template = templateName
		}

		// Update scionJSONStr with modified config
		if modifiedData, err := json.MarshalIndent(scionConfig, "", "  "); err == nil {
			scionJSONStr = string(modifiedData)
		}
	}

	mdFile := "gemini.md"
	claudeJSON := ""
	if harness == "claude" {
		mdFile = "claude.md"
		claudeJSON = readEmbed(".claude.json")
	}

	// Seed template files
	files := []struct {
		path    string
		content string
	}{
		{filepath.Join(templateDir, "scion-agent.json"), scionJSONStr},
		{filepath.Join(templateDir, "scion_hook.py"), readEmbed("scion_hook.py")},
		{filepath.Join(templateDir, configDirName, "settings.json"), readEmbed("settings.json")},
		{filepath.Join(templateDir, configDirName, "system_prompt.md"), readEmbed("system_prompt.md")},
		{filepath.Join(templateDir, configDirName, mdFile), readEmbed(mdFile)},
		{filepath.Join(templateDir, ".bashrc"), readEmbed("bashrc")},
	}

	if claudeJSON != "" {
		files = append(files, struct {
			path    string
			content string
		}{filepath.Join(templateDir, ".claude.json"), claudeJSON})
	}

	for _, f := range files {
		// Always write settings.json and .claude.json to ensure they match current defaults
		baseName := filepath.Base(f.path)
		if force || baseName == "settings.json" || baseName == ".claude.json" {
			if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
			continue
		}

		if _, err := os.Stat(f.path); os.IsNotExist(err) {
			if err := os.WriteFile(f.path, []byte(f.content), 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
		}
	}

	return nil
}

func InitProject(targetDir string) error {
	var projectDir string
	var err error

	if targetDir != "" {
		projectDir = targetDir
	} else {
		projectDir, err = GetTargetProjectDir()
		if err != nil {
			return err
		}
	}

	// Create grove-level settings file if it doesn't exist
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}
	settingsPath := filepath.Join(projectDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		// Seed with default settings
		defaultSettings, err := embedsFS.ReadFile("embeds/default_settings.json")
		if err != nil {
			return fmt.Errorf("failed to read default settings: %w", err)
		}
		if err := os.WriteFile(settingsPath, defaultSettings, 0644); err != nil {
			return fmt.Errorf("failed to seed settings.json: %w", err)
		}
	}

	templatesDir := filepath.Join(projectDir, "templates")
	agentsDir := filepath.Join(projectDir, "agents")

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	if err := SeedTemplateDir(filepath.Join(templatesDir, "gemini"), "gemini", "gemini", "gemini", ".gemini", false); err != nil {
		return fmt.Errorf("failed to seed gemini template: %w", err)
	}

	return SeedTemplateDir(filepath.Join(templatesDir, "claude"), "claude", "claude", "claude", ".claude", false)
}

func InitGlobal() error {
	globalDir, err := GetGlobalDir()
	if err != nil {
		return err
	}

	// Create global settings file if it doesn't exist
	settingsPath := filepath.Join(globalDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		defaultSettings, err := embedsFS.ReadFile("embeds/default_settings.json")
		if err != nil {
			return fmt.Errorf("failed to read default settings: %w", err)
		}
		if err := os.WriteFile(settingsPath, defaultSettings, 0644); err != nil {
			return fmt.Errorf("failed to seed global settings.json: %w", err)
		}
	}

	templatesDir := filepath.Join(globalDir, "templates")
	agentsDir := filepath.Join(globalDir, "agents")

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create global agents directory: %w", err)
	}

	if err := SeedTemplateDir(filepath.Join(templatesDir, "gemini"), "gemini", "gemini", "gemini", ".gemini", false); err != nil {
		return fmt.Errorf("failed to seed global gemini template: %w", err)
	}

	return SeedTemplateDir(filepath.Join(templatesDir, "claude"), "claude", "claude", "claude", ".claude", false)
}
