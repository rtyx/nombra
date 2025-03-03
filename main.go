package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

const (
	maxContentLength = 3000
	truncationSuffix = "... [content truncated]"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "quill",
		Short: "Quill is a CLI tool for titling PDF files",
		Long:  `A fast and flexible PDF title built with love by Rafael and friends in Go.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print("Quill is running!")
		},
	}
	rootCmd.Execute()
}
