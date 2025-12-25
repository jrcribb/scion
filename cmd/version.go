/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"fmt"

	"github.com/ptone/scion-agent/pkg/version"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of scion",
	Long:  `All software has versions. This is scion's`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Get())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
