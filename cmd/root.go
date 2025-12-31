/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"fmt"
	"os"

	"github.com/ptone/scion-agent/pkg/util"
	"github.com/spf13/cobra"
)



var (
	grovePath  string
	globalMode bool
	profile    string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "scion",
	Short: "A container-based orchestration tool for managing concurrent LLM agents",
	Long: `Scion is a container-based orchestration tool for managing 
concurrent LLM agents. It enables parallel execution of specialized 
sub-agents with isolated identities, credentials, and workspaces.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if globalMode && grovePath == "" {
			grovePath = "global"
		}

		if util.IsGitRepo() {
			if err := util.CheckGitVersion(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&grovePath, "grove", "g", "", "Path to a .scion grove directory")
	rootCmd.PersistentFlags().BoolVar(&globalMode, "global", false, "Use the global grove (equivalent to --grove global)")
	rootCmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "Configuration profile to use")
}


