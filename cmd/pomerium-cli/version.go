package main

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version",
	Long:  `Print the cli version.`,
	Run: func(_ *cobra.Command, _ []string) {
		rootCmd.SetArgs([]string{"--version"})
		_ = rootCmd.Execute()
	},
}
