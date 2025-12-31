package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/spf13/cobra"
)

// templatesCmd represents the templates command
var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage agent templates",
	Long:  `List and inspect templates used to provision new agents.`,
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		templates, err := config.ListTemplates()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH")
		for _, t := range templates {
			fmt.Fprintf(w, "%s\t%s\n", t.Name, t.Path)
		}
		w.Flush()
		return nil
	},
}

var templatesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show template configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		tpl, err := config.FindTemplate(name)
		if err != nil {
			return err
		}

		cfg, err := tpl.LoadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Template: %s\n", tpl.Name)
		fmt.Printf("Path:     %s\n", tpl.Path)
		fmt.Println("Configuration (scion-agent.json):")
		
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	},
}

var templatesCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		harnessName, _ := cmd.Flags().GetString("harness")
		if harnessName == "" {
			harnessName = "gemini"
		}

		h := harness.New(harnessName)
		embedDir := h.GetEmbedDir()
		configDir := h.DefaultConfigDir()

		err := config.CreateTemplate(name, harnessName, embedDir, configDir, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s created successfully.\n", name)
		return nil
	},
}

var templatesDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a template",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		err := config.DeleteTemplate(name, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s deleted successfully.\n", name)
		return nil
	},
}

var templatesCloneCmd = &cobra.Command{
	Use:   "clone <src-name> <dest-name>",
	Short: "Clone an existing template",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcName := args[0]
		destName := args[1]
		global, _ := cmd.Flags().GetBool("global")
		err := config.CloneTemplate(srcName, destName, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s cloned to %s successfully.\n", srcName, destName)
		return nil
	},
}

var templatesUpdateDefaultCmd = &cobra.Command{
	Use:   "update-default",
	Short: "Update default templates with the latest from the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		global, _ := cmd.Flags().GetBool("global")
		err := config.UpdateDefaultTemplates(global)
		if err != nil {
			return err
		}
		fmt.Println("Default templates updated successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesShowCmd)
	templatesCmd.AddCommand(templatesCreateCmd)
	templatesCmd.AddCommand(templatesCloneCmd)
	templatesCmd.AddCommand(templatesDeleteCmd)
	templatesCmd.AddCommand(templatesUpdateDefaultCmd)

	templatesCreateCmd.Flags().StringP("harness", "H", "", "Harness type (e.g. gemini, claude)")
}
