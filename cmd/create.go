package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create <agent-name>",
	Short: "Provision a new scion agent without starting it",
	Long: `Provision a new isolated LLM agent directory to perform a specific task.
The agent will be created from a template.`, 
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]

		// Check if container already exists
		rt := runtime.GetRuntime()
		agents, err := rt.List(context.Background(), nil)
		if err == nil {
			for _, a := range agents {
				if a.ID == agentName || a.Name == agentName {
					fmt.Printf("Agent container '%s' already exists (Status: %s).\n", agentName, a.Status)
					// We continue to check directory
				}
			}
		}

		projectDir, err := config.GetResolvedProjectDir(grovePath)
		if err != nil {
			return err
		}
		agentsDir := filepath.Join(projectDir, "agents")
		agentDir := filepath.Join(agentsDir, agentName)
		workspaceDir := filepath.Join(agentDir, "workspace")

		if _, err := os.Stat(agentDir); err == nil {
			if _, err := os.Stat(workspaceDir); err == nil {
				fmt.Printf("Agent '%s' already exists.\n", agentName)
				return nil
			}
		}

		fmt.Printf("Creating agent '%s'...\n", agentName)

		_, _, _, err = ProvisionAgent(agentName, templateName, agentImage, grovePath)
		if err != nil {
			return err
		}

		fmt.Printf("Agent '%s' created successfully.\n", agentName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().StringVarP(&templateName, "type", "t", "default", "Template to use")
	createCmd.Flags().StringVarP(&agentImage, "image", "i", "", "Container image to use (overrides template)")
}

