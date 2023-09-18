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
	Run: func(cmd *cobra.Command, args []string) {
		rootCmd.SetArgs([]string{"--version"})
		_ = rootCmd.Execute()
	},
}
