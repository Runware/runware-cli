package cmd

import "github.com/spf13/cobra"

var inferenceCmd = &cobra.Command{
	Use:   "inference",
	Short: "Run inference tasks",
}

func init() {
	inferenceCmd.AddCommand(imageInferenceCmd)
	inferenceCmd.AddCommand(videoInferenceCmd)
	inferenceCmd.AddCommand(audioInferenceCmd)
	inferenceCmd.AddCommand(textInferenceCmd)
}
