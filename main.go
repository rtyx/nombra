package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"strings"
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

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func extractPDFContent(path string) (string, error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer file.Close()

	var content strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("page %d text extraction failed: %w", i, err)
		}
		content.WriteString(text)
	}

	return content.String(), nil
}
