package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

const (
	maxContentLength = 3000
	truncationSuffix = "... [content truncated]"
)

func main() {
	var apiKey string

	rootCmd := &cobra.Command{
		Use:   "quill [PDF file]",
		Short: "Generate titles for PDF documents using AI",
		Long:  "A CLI tool that analyzes PDF content and generates appropriate titles using OpenAI's API",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]
			textContent, err := extractPDFContent(filePath)
			if err != nil {
				fmt.Printf("PDF processing error: %v\n", err)
				os.Exit(1)
			}

			title, err := generateOpenAITitle(textContent, apiKey)
			if err != nil {
				fmt.Printf("Title generation failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("\nSuggested title: %s\n\n", title)
		},
	}

	// Configure flags
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "OpenAI API key (default: $OPENAI_API_KEY)")
	rootCmd.MarkFlagRequired("key")
}
