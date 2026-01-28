package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/hubsync"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/spf13/cobra"
)

var globalInit bool

// groveCmd represents the grove command
var groveCmd = &cobra.Command{
	Use:     "grove",
	Aliases: []string{"group"},
	Short:   "Manage scion groves (agent groups)",
	Long:    `A grove is the grouping construct for a set of agents. The .scion folder represents a grove.`,
}

// groveInitCmd represents the init subcommand for grove
var groveInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new grove",
	Long: `Initialize a new grove by creating the .scion directory structure
and seeding the default template. 

By default, it initializes in:
- The root of the current git repo if run inside a repo
- The current directory

With --global, it initializes in the user's home folder.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		harnesses := harness.All()

		if globalInit {
			fmt.Println("Initializing global scion directory...")
			if err := config.InitGlobal(harnesses); err != nil {
				return fmt.Errorf("failed to initialize global config: %w", err)
			}
			fmt.Println("scion grove successfully initialized.")

			// Prompt for Hub registration if Hub is configured
			if err := promptHubRegistration(true); err != nil {
				// Non-fatal: just log the error
				fmt.Printf("Note: %v\n", err)
			}

			return nil
		}

		// Check for nested grove - error if already inside a scion project
		if grovePath, rootDir, found := config.GetEnclosingGrovePath(); found {
			wd, _ := os.Getwd()
			if filepath.Clean(wd) == filepath.Clean(rootDir) {
				return fmt.Errorf("already inside a scion project at %s. skipping re-initialization", rootDir)
			}
			// Allow initialization if the found grove is the global one
			// This permits project groves to exist when ~/.scion exists
			globalDir, err := config.GetGlobalDir()
			if err != nil || filepath.Clean(grovePath) != filepath.Clean(globalDir) {
				return fmt.Errorf("already inside a scion project at %s. Nested groves are not supported", rootDir)
			}
			// Found grove is the global one - allow project initialization to proceed
		}

		// Determine target directory
		targetDir, err := config.GetTargetProjectDir()
		if err != nil {
			return fmt.Errorf("failed to determine project directory: %w", err)
		}

		// Check if we're in a subdirectory of a git repo
		wd, _ := os.Getwd()
		if util.IsGitRepo() {
			repoRoot, err := util.RepoRoot()
			if err == nil && repoRoot != "" {
				expectedTarget := filepath.Join(repoRoot, config.DotScion)
				if targetDir == expectedTarget && wd != repoRoot {
					fmt.Printf("Note: Creating .scion at repository root (%s)\n", repoRoot)
				}
			}
		}

		fmt.Println("Initializing scion project grove...")
		if err := config.InitProject("", harnesses); err != nil {
			return fmt.Errorf("failed to initialize project grove: %w", err)
		}

		// Generate and save grove_id
		groveID := config.GenerateGroveIDForDir(filepath.Dir(targetDir))
		if err := config.UpdateSetting(targetDir, "grove_id", groveID, false); err != nil {
			fmt.Printf("Warning: failed to save grove_id: %v\n", err)
		}

		fmt.Println("scion grove successfully initialized.")
		fmt.Printf("Grove ID: %s\n", groveID)

		// Prompt for Hub registration if Hub is configured
		if err := promptHubRegistration(false); err != nil {
			// Non-fatal: just log the error
			fmt.Printf("Note: %v\n", err)
		}

		return nil
	},
}

// promptHubRegistration checks if Hub is configured and prompts to register the grove.
func promptHubRegistration(isGlobal bool) error {
	// Skip if --no-hub is set
	if noHub {
		return nil
	}

	// Resolve grove path
	var gp string
	if isGlobal {
		gp = "global"
	}
	resolvedPath, _, err := config.ResolveGrovePath(gp)
	if err != nil {
		return nil // Silently skip if we can't resolve path
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return nil // Silently skip if we can't load settings
	}

	// Check if Hub endpoint is configured (but not necessarily enabled)
	if !settings.IsHubConfigured() {
		return nil // No Hub endpoint configured, skip
	}

	// Prompt for registration
	if hubsync.ShowInitRegistrationPrompt(autoConfirm) {
		// Create Hub client and register
		client, err := getHubClient(settings)
		if err != nil {
			return fmt.Errorf("failed to create Hub client: %w", err)
		}

		// Check health first
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := client.Health(ctx); err != nil {
			return fmt.Errorf("Hub is not responding: %w", err)
		}

		// Get grove info
		var groveName string
		var gitRemote string
		groveID := settings.GroveID

		if isGlobal {
			groveName = "global"
		} else {
			gitRemote = util.GetGitRemote()
			if gitRemote != "" {
				groveName = util.ExtractRepoName(gitRemote)
			} else {
				groveName = filepath.Base(filepath.Dir(resolvedPath))
			}
		}

		// Get hostname
		hostName, _ := os.Hostname()
		if hostName == "" {
			hostName = "local-host"
		}

		// Get existing host ID if available
		var existingHostID string
		if settings.Hub != nil {
			existingHostID = settings.Hub.HostID
		}

		// Register
		req := &hubclient.RegisterGroveRequest{
			ID:        groveID,
			Name:      groveName,
			GitRemote: util.NormalizeGitRemote(gitRemote),
			Path:      resolvedPath,
			Mode:      "connected",
			Host: &hubclient.HostInfo{
				ID:   existingHostID,
				Name: hostName,
			},
		}

		ctxReg, cancelReg := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelReg()

		resp, err := client.Groves().Register(ctxReg, req)
		if err != nil {
			return fmt.Errorf("registration failed: %w", err)
		}

		// Save credentials
		if resp.HostToken != "" {
			_ = config.UpdateSetting(resolvedPath, "hub.hostToken", resp.HostToken, isGlobal)
		}
		if resp.Host != nil && resp.Host.ID != "" {
			_ = config.UpdateSetting(resolvedPath, "hub.hostId", resp.Host.ID, isGlobal)
		}

		// Enable Hub integration
		_ = config.UpdateSetting(resolvedPath, "hub.enabled", "true", isGlobal)

		if resp.Created {
			fmt.Printf("Created new grove on Hub: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
		} else {
			fmt.Printf("Linked to existing grove on Hub: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
		}
		if resp.Host != nil {
			fmt.Printf("Host registered: %s (ID: %s)\n", resp.Host.Name, resp.Host.ID)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(groveCmd)
	groveCmd.AddCommand(groveInitCmd)

	groveInitCmd.Flags().BoolVar(&globalInit, "global", false, "Initialize the global grove in the home directory")
}
