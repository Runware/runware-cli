package cmd

import "github.com/spf13/cobra"

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage and search models",
}

func init() {
	modelCmd.AddCommand(modelSearchCmd)
}
