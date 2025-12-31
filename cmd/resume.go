package cmd

import (
	"github.com/spf13/cobra"
)

// resumeCmd represents the resume command
var resumeCmd = &cobra.Command{
	Use:   "resume <agent-name> [task...]",
	Short: "Resume a stopped scion agent",
	Long: `Resume an existing stopped LLM agent. 
The agent will be re-launched with the --resume flag, preserving its previous state.

The agent-name is required as the first argument. If subsequent arguments 
form a task prompt, they will be used. If no task arguments are provided, 
the agent will look for a prompt.md file in its root directory.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunAgent(cmd, args, true)
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
	resumeCmd.Flags().StringVarP(&templateName, "type", "t", "", "Template to use")
	resumeCmd.Flags().StringVarP(&agentImage, "image", "i", "", "Container image to use (overrides template)")
	resumeCmd.Flags().BoolVar(&noAuth, "no-auth", false, "Disable authentication propagation")
	resumeCmd.Flags().BoolVarP(&attach, "attach", "a", false, "Attach to the agent TTY after starting")
}