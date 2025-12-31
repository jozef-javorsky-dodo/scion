package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/util"
)

func DeleteAgentFiles(agentName string, grovePath string) error {
	var agentsDirs []string
	if projectDir, err := config.GetResolvedProjectDir(grovePath); err == nil {
		agentsDirs = append(agentsDirs, filepath.Join(projectDir, "agents"))
	}
	// Also check global just in case
	if globalDir, err := config.GetGlobalAgentsDir(); err == nil {
		agentsDirs = append(agentsDirs, globalDir)
	}

	for _, dir := range agentsDirs {
		agentDir := filepath.Join(dir, agentName)
		if _, err := os.Stat(agentDir); err != nil {
			continue
		}

		agentWorkspace := filepath.Join(agentDir, "workspace")
		// Check if it's a worktree before trying to remove it
		if _, err := os.Stat(filepath.Join(agentWorkspace, ".git")); err == nil {
			if err := util.RemoveWorktree(agentWorkspace); err != nil {
				// Warn or error?
			}
		}

		if err := os.RemoveAll(agentDir); err != nil {
			return fmt.Errorf("failed to remove agent directory: %w", err)
		}
	}
	return nil
}

func (m *AgentManager) Provision(ctx context.Context, opts api.StartOptions) (*api.ScionConfig, error) {
	_, _, _, cfg, err := GetAgent(ctx, opts.Name, opts.Template, opts.Image, opts.GrovePath, "created")
	if err == nil {
		_ = UpdateAgentConfig(opts.Name, opts.GrovePath, "created", m.Runtime.Name(), opts.Profile)
	}
	return cfg, err
}

func ProvisionAgent(ctx context.Context, agentName string, templateName string, agentImage string, grovePath string, optionalStatus string) (string, string, *api.ScionConfig, error) {
	// 1. Prepare agent directories
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return "", "", nil, err
	}

	groveName := config.GetGroveName(projectDir)

	// Verify .gitignore if in a repo
	if util.IsGitRepo() {
		// Find the projectDir relative to repo root if possible
		root, err := util.RepoRoot()
		if err == nil {
			rel, err := filepath.Rel(root, projectDir)
			if err == nil && !strings.HasPrefix(rel, "..") {
				agentsPath := filepath.Join(rel, "agents")
				if !util.IsIgnored(agentsPath + "/") {
					return "", "", nil, fmt.Errorf("security error: '%s/' must be in .gitignore when using a project-local grove", agentsPath)
				}
			}
		}
	}
	agentsDir := filepath.Join(projectDir, "agents")

	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	agentWorkspace := filepath.Join(agentDir, "workspace")

	if err := os.MkdirAll(agentHome, 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create agent home: %w", err)
	}

	// Create empty prompt.md in agent root
	promptFile := filepath.Join(agentDir, "prompt.md")
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		if err := os.WriteFile(promptFile, []byte(""), 0644); err != nil {
			return "", "", nil, fmt.Errorf("failed to create prompt.md: %w", err)
		}
	}

	if util.IsGitRepo() {
		// Remove existing workspace dir if it exists to allow worktree add
		os.RemoveAll(agentWorkspace)
		if err := util.CreateWorktree(agentWorkspace, agentName); err != nil {
			return "", "", nil, fmt.Errorf("failed to create git worktree: %w", err)
		}
	} else {
		if err := os.MkdirAll(agentWorkspace, 0755); err != nil {
			return "", "", nil, fmt.Errorf("failed to create agent workspace: %w", err)
		}
	}

	// 2. Load and copy templates
	chain, err := config.GetTemplateChain(templateName)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to load template: %w", err)
	}

	finalScionCfg := &api.ScionConfig{}

	for _, tpl := range chain {
		if err := util.CopyDir(tpl.Path, agentHome); err != nil {
			return "", "", nil, fmt.Errorf("failed to copy template %s: %w", tpl.Name, err)
		}

		// Load scion-agent.json from this template and merge it
		tplCfg, err := tpl.LoadConfig()
		if err == nil {
			finalScionCfg = config.MergeScionConfig(finalScionCfg, tplCfg)
		}
	}

	// Update agent-specific scion-agent.json
	if finalScionCfg == nil {
		finalScionCfg = &api.ScionConfig{}
	}
	finalScionCfg.Info = &api.AgentInfo{
		Grove:    groveName,
		Name:     agentName,
		Template: templateName,
	}
	if optionalStatus != "" {
		finalScionCfg.Info.Status = optionalStatus
	}
	// Image and other fields will be resolved at runtime from settings,
	// but we can persist the requested image if provided.
	if agentImage != "" {
		finalScionCfg.Info.Image = agentImage
	}
	agentCfgData, err := json.MarshalIndent(finalScionCfg, "", "  ")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to marshal agent config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "scion-agent.json"), agentCfgData, 0644); err != nil {
		return "", "", nil, fmt.Errorf("failed to write agent config: %w", err)
	}

	// Write agent-info.json to home for container access
	if finalScionCfg.Info != nil {
		infoData, err := json.MarshalIndent(finalScionCfg.Info, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join(agentHome, "agent-info.json"), infoData, 0644)
		}
	}

	// 3. Harness provisioning
	h := harness.New(finalScionCfg.Harness)
	if err := h.Provision(ctx, agentName, agentHome, agentWorkspace); err != nil {
		return "", "", nil, fmt.Errorf("harness provisioning failed: %w", err)
	}

	return agentHome, agentWorkspace, finalScionCfg, nil
}

func GetSavedProfile(agentName string, grovePath string) string {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return ""
	}
	scionJSONPath := filepath.Join(projectDir, "agents", agentName, "scion-agent.json")
	if _, err := os.Stat(scionJSONPath); err == nil {
		data, err := os.ReadFile(scionJSONPath)
		if err == nil {
			var cfg api.ScionConfig
			if err := json.Unmarshal(data, &cfg); err == nil && cfg.Info != nil {
				return cfg.Info.Profile
			}
		}
	}
	return ""
}

func GetSavedRuntime(agentName string, grovePath string) string {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return ""
	}
	scionJSONPath := filepath.Join(projectDir, "agents", agentName, "scion-agent.json")
	if _, err := os.Stat(scionJSONPath); err == nil {
		data, err := os.ReadFile(scionJSONPath)
		if err == nil {
			var cfg api.ScionConfig
			if err := json.Unmarshal(data, &cfg); err == nil && cfg.Info != nil {
				return cfg.Info.Runtime
			}
		}
	}
	return ""
}

func UpdateAgentConfig(agentName string, grovePath string, status string, runtime string, profile string) error {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return err
	}
	agentsDir := filepath.Join(projectDir, "agents")
	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	scionJsonPath := filepath.Join(agentDir, "scion-agent.json")
	agentInfoPath := filepath.Join(agentHome, "agent-info.json")

	if _, err := os.Stat(scionJsonPath); os.IsNotExist(err) {
		return nil // Nothing to update
	}

	data, err := os.ReadFile(scionJsonPath)
	if err != nil {
		return err
	}

	var cfg api.ScionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Info == nil {
		cfg.Info = &api.AgentInfo{}
	}
	if status != "" {
		cfg.Info.Status = status
	}
	if runtime != "" {
		cfg.Info.Runtime = runtime
	}
	if profile != "" {
		cfg.Info.Profile = profile
	}

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(scionJsonPath, newData, 0644); err != nil {
		return err
	}

	// Also update agent-info.json in home
	infoData, err := json.MarshalIndent(cfg.Info, "", "  ")
	if err == nil {
		_ = os.WriteFile(agentInfoPath, infoData, 0644)
	}

	return nil
}

func GetAgent(ctx context.Context, agentName string, templateName string, agentImage string, grovePath string, optionalStatus string) (string, string, string, *api.ScionConfig, error) {
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return "", "", "", nil, err
	}
	agentsDir := filepath.Join(projectDir, "agents")
	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	agentWorkspace := filepath.Join(agentDir, "workspace")

	// Load settings for default template
	settings, err := config.LoadSettings(projectDir)
	if err != nil {
		// Just log or ignore
	}
	defaultTemplate := "gemini"
	if settings != nil && settings.DefaultTemplate != "" {
		defaultTemplate = settings.DefaultTemplate
	}

	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		if templateName == "" {
			templateName = defaultTemplate
		}
		home, ws, cfg, err := ProvisionAgent(ctx, agentName, templateName, agentImage, grovePath, optionalStatus)
		return agentDir, home, ws, cfg, err
	}

	// Load the agent's scion-agent.json from agent root
	tpl := &config.Template{Path: agentDir}
	agentCfg, err := tpl.LoadConfig()
	if err != nil {
		return agentDir, agentHome, agentWorkspace, nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	// Re-construct the full config by merging the template chain
	effectiveTemplate := defaultTemplate
	if agentCfg.Info != nil && agentCfg.Info.Template != "" {
		effectiveTemplate = agentCfg.Info.Template
	}

	chain, err := config.GetTemplateChain(effectiveTemplate)
	if err != nil {
		return agentDir, agentHome, agentWorkspace, agentCfg, nil
	}

	mergedCfg := &api.ScionConfig{}
	for _, tpl := range chain {
		tplCfg, err := tpl.LoadConfig()
		if err == nil {
			mergedCfg = config.MergeScionConfig(mergedCfg, tplCfg)
		}
	}

	finalCfg := config.MergeScionConfig(mergedCfg, agentCfg)

	return agentDir, agentHome, agentWorkspace, finalCfg, nil
}

