package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
)

func (m *AgentManager) List(ctx context.Context, filter map[string]string) ([]api.AgentInfo, error) {
	agents, err := m.Runtime.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Also find "created" agents that don't have a container yet
	// We need to know which groves to scan.
	// If filter has scion.grove, we scan that one.
	// Otherwise, we scan current and global?
	
	var grovesToScan []string
	if groveName, ok := filter["scion.grove"]; ok {
		_ = groveName
		// We need to resolve groveName to a path. This is currently not easy without searching.
		// For now, if scion.grove is provided, we assume we only care about running ones 
		// OR we need to be passed a grove path.
	}

	// This logic is a bit tied to how CLI uses it.
	// Let's at least support scanning a specific grove if provided in filter?
	// Or maybe Add a special filter key for GrovePath.
	
	grovePath := filter["scion.grove_path"]
	if grovePath != "" {
		grovesToScan = append(grovesToScan, grovePath)
	} else if len(filter) == 0 || (len(filter) == 1 && filter["scion.agent"] == "true") {
		// Default: scan current resolved project dir and global dir
		pd, _ := config.GetResolvedProjectDir("")
		if pd != "" {
			grovesToScan = append(grovesToScan, pd)
		}
		gd, _ := config.GetGlobalDir()
		if gd != "" && gd != pd {
			grovesToScan = append(grovesToScan, gd)
		}
	}

	runningNames := make(map[string]bool)
	for i := range agents {
		runningNames[agents[i].Name] = true
		if agents[i].GrovePath != "" {
			scionJSON := filepath.Join(agents[i].GrovePath, "agents", agents[i].Name, "scion-agent.json")
			if data, err := os.ReadFile(scionJSON); err == nil {
				var cfg api.ScionConfig
				if err := json.Unmarshal(data, &cfg); err == nil && cfg.Info != nil {
					agents[i].Status = cfg.Info.Status
					if agents[i].Runtime == "" {
						agents[i].Runtime = cfg.Info.Runtime
					}
					if agents[i].Profile == "" {
						agents[i].Profile = cfg.Info.Profile
					}
				}
			}
		}
	}

	for _, gp := range grovesToScan {
		agentsDir := filepath.Join(gp, "agents")
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			continue
		}
		groveName := config.GetGroveName(gp)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if runningNames[e.Name()] {
				continue
			}

			// Check scion-agent.json
			agentScionJSON := filepath.Join(agentsDir, e.Name(), "scion-agent.json")
			data, err := os.ReadFile(agentScionJSON)
			if err != nil {
				continue
			}
			var cfg api.ScionConfig
			if err := json.Unmarshal(data, &cfg); err == nil && cfg.Info != nil {
				agents = append(agents, api.AgentInfo{
					Name:            e.Name(),
					Template:        cfg.Info.Template,
					Grove:           groveName,
					GrovePath:       gp,
					ContainerStatus: "created",
					Image:           cfg.Info.Image,
					Status:          cfg.Info.Status,
					Runtime:         cfg.Info.Runtime,
					Profile:         cfg.Info.Profile,
				})
			}
		}
	}

	return agents, nil
}
