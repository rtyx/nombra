package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ledongthuc/pdf"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
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

	// Mark the key flag as required
	err := rootCmd.MarkFlagRequired("key")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

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

func generateOpenAITitle(content, apiKey string) (string, error) {
	content = truncateContent(content)
	client := openai.NewClient(apiKey)

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role: openai.ChatMessageRoleSystem,
					Content: "You are a professional document curator. " +
						"Generate a concise, descriptive title for the following content. " +
						"Respond only with the title itself, no additional text.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: content,
				},
			},
			Temperature: 0.3,
		},
	)

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from OpenAI API")
	}

	return cleanTitle(resp.Choices[0].Message.Content), nil
}

func truncateContent(content string) string {
	if len(content) > maxContentLength {
		return content[:maxContentLength-len(truncationSuffix)] + truncationSuffix
	}
	return content
}

func cleanTitle(title string) string {
	title = strings.Trim(title, "\"'“”‘’ \t\n")
	return strings.ReplaceAll(title, "\n", " ")
}
