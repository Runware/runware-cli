package model

import "github.com/spf13/cobra"

func New() *cobra.Command {
	return &cobra.Command{
		Use:   "model",
		Short: "Manage and search models",
	}
}
