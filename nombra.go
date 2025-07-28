// Copyright 2025 Rafael Toledano Illán
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
)

const (
	maxFilenameLength = 120
	truncationSuffix  = "... [content truncated]"
	sanitizeRegex     = `[<>:"\/\\|?*]`
)

var (
	verbose          bool
	maxContentLength = 3000
	minContentLength = 10
	ocr              bool
	model            string
)

// main initializes and executes the CLI command for generating a title for a PDF file.
// It sets up the command line flags, validates the API key, extracts content from the PDF,
// generates a title using OpenAI's API, and finally renames the file based on the title.
func main() {
	var apiKey string

	rootCmd := &cobra.Command{
		Use:     "nombra [PDF file]",
		Short:   "Generate titles for PDF documents using AI",
		Long:    "A CLI tool that analyzes PDF content and generates appropriate titles using OpenAI's API",
		Example: "nombra myfile.pdf\n  nombra myfile.pdf --model gpt-4-turbo",
		Args:    cobra.ExactArgs(1),
		PreRun: func(cmd *cobra.Command, args []string) {
			if apiKey == "" {
				apiKey = os.Getenv("OPENAI_API_KEY")
			}
			if apiKey == "" {
				fmt.Println("Error: API key required. Use --key or set OPENAI_API_KEY")
				os.Exit(1)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			if verbose {
				log.Println("Verbose mode enabled")
			}

			filePath := args[0]
			textContent, err := extractPDFContent(filePath)
			if err != nil {
				fmt.Printf("PDF processing error: %v\n", err)
				os.Exit(1)
			}

			title, err := generateOpenAITitle(textContent, apiKey, model)
			if err != nil {
				fmt.Printf("Title generation failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("\nGenerated title: %s\n\n", title)

			newPath, err := safeRenameFile(filePath, title)
			if err != nil {
				fmt.Printf("Renaming failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Successfully renamed:\n  %s\n  → %s\n\n", filepath.Base(filePath), filepath.Base(newPath))
		},
	}

	// Configure flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().BoolVarP(&ocr, "ocr", "o", false, "Force OCR text extraction")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", openai.GPT3Dot5Turbo, "OpenAI model to use")
	rootCmd.Flags().IntVarP(&maxContentLength, "max-content-length", "l", 3000, "Maximum content length for processing")
	rootCmd.Flags().IntVarP(&minContentLength, "min-content-length", "n", 10, "Minimum content length required for processing")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "OpenAI API key (default: $OPENAI_API_KEY)")

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// safeRenameFile renames the original file based on the generated title.
// It sanitizes the title to form a valid filename, ensures the filename length is safe,
// and handles name collisions by generating a unique filename.
func safeRenameFile(originalPath, title string) (string, error) {
	// Sanitize title and prepare new filename
	cleanTitle := sanitizeFilename(title)
	if cleanTitle == "" {
		return "", fmt.Errorf("generated title results in invalid filename")
	}

	dir := filepath.Dir(originalPath)
	ext := filepath.Ext(originalPath)
	baseName := cleanTitle + ext

	// Ensure filename length is filesystem-safe
	if len(baseName) > maxFilenameLength {
		baseName = baseName[:maxFilenameLength-len(ext)] + ext
	}

	newPath := filepath.Join(dir, baseName)

	// Handle existing files with same name
	if _, err := os.Stat(newPath); err == nil {
		newPath = generateUniqueName(dir, cleanTitle, ext)
	}

	// Perform actual rename
	if err := os.Rename(originalPath, newPath); err != nil {
		return "", fmt.Errorf("could not rename file: %w", err)
	}

	return newPath, nil
}

// sanitizeFilename removes any invalid characters from the title and trims whitespace.
// It ensures that the resulting string is safe to use as a filename.
func sanitizeFilename(title string) string {
	// Remove invalid characters
	reg := regexp.MustCompile(sanitizeRegex)
	clean := reg.ReplaceAllString(title, "")

	// Trim whitespace and truncate
	clean = strings.TrimSpace(clean)
	if len(clean) > maxFilenameLength {
		clean = clean[:maxFilenameLength]
	}

	// Handle cases where title becomes empty
	if clean == "" {
		return "untitled-document"
	}

	return clean
}

// generateUniqueName creates a unique filename by appending an incremental counter
// to the base name until an unused filename is found in the specified directory.
func generateUniqueName(dir, baseName, ext string) string {
	counter := 1
	pattern := filepath.Join(dir, baseName+"-%d"+ext)

	for {
		candidate := fmt.Sprintf(pattern, counter)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		counter++
	}
}

// validateContentLength checks if the content meets the minimum length requirement
func validateContentLength(content string) error {
	trimmedLength := len(strings.TrimSpace(content))
	if trimmedLength < minContentLength {
		return fmt.Errorf("extracted content length (%d) is below minimum required length (%d)", trimmedLength, minContentLength)
	}
	return nil
}

// extractPDFContent extracts text from the specified PDF file.
// It first attempts to extract text using a standard method.
// If that fails or produces empty content, it falls back to OCR-based extraction.
func extractPDFContent(path string) (string, error) {
	// Define extraction methods
	extractors := []struct {
		name string
		fn   func(string) (string, error)
	}{
		{"standard", extractTextFromPDF},
		{"OCR", extractTextViaOCR},
	}

	// If OCR is forced, only use OCR
	if ocr {
		text, err := extractTextViaOCR(path)
		if err != nil {
			return "", err
		}
		if err := validateContentLength(text); err != nil {
			return "", err
		}
		return text, nil
	}

	// Try each extraction method
	var lastErr error
	for _, extractor := range extractors {
		if verbose {
			log.Printf("Attempting %s text extraction...", extractor.name)
		}

		text, err := extractor.fn(path)
		if err != nil {
			lastErr = err
			continue
		}

		if text != "" {
			if err := validateContentLength(text); err != nil {
				lastErr = err
				continue
			}
			return text, nil
		}
	}

	// If we get here, all extraction methods failed
	if lastErr != nil {
		return "", fmt.Errorf("all text extraction methods failed: %w", lastErr)
	}
	return "", fmt.Errorf("no text could be extracted from the PDF")
}

// extractTextFromPDF extracts plain text from the PDF using the pdf library.
// It iterates through all pages, concatenates the extracted text, and performs basic validations.
func extractTextFromPDF(path string) (string, error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer file.Close()

	var content strings.Builder
	totalPages := reader.NumPage()
	if totalPages == 0 {
		return "", fmt.Errorf("PDF appears to be empty")
	}

	for i := 1; i <= totalPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("page %d text extraction failed: %w", i, err)
		}
		content.WriteString(strings.TrimSpace(text))
	}

	if content.Len() == 0 {
		return "", fmt.Errorf("no extractable text found in PDF")
	}

	// If the extraction provides a long string of text with no spaces in it, throw an error
	if len(content.String()) > 500 && !strings.Contains(content.String(), " ") {
		return "", fmt.Errorf("text extraction failed: no spaces found in extracted text")
	}

	return content.String(), nil
}

// extractTextViaOCR extracts text from a PDF by converting each page to an image and running OCR on them.
// It uses external tools: 'pdftoppm' to convert the PDF to PNG images and 'tesseract' to perform OCR.
func extractTextViaOCR(pdfPath string) (string, error) {
	tempDir, err := os.MkdirTemp("", "nombra-ocr")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Convert PDF to images
	cmd := exec.Command("pdftoppm", "-png", pdfPath, filepath.Join(tempDir, "page"))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftoppm failed: %w", err)
	}

	// Process each page image
	var content strings.Builder
	pages, _ := filepath.Glob(filepath.Join(tempDir, "page-*.png"))

	if len(pages) == 0 {
		return "", fmt.Errorf("no pages converted from PDF")
	}

	for _, page := range pages {
		// Run OCR on each page
		cmd := exec.Command("tesseract", page, "stdout")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("tesseract failed: %w", err)
		}

		content.WriteString(out.String())
		content.WriteString("\n")
	}

	if content.Len() == 0 {
		return "", fmt.Errorf("OCR extracted no text")
	}

	return content.String(), nil
}

// generateOpenAITitle sends the extracted PDF content to OpenAI's Chat Completion API
// to generate an appropriate title based on specific formatting rules.
// It then cleans the returned title to ensure proper formatting.
func generateOpenAITitle(content, apiKey, model string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("empty content provided for title generation")
	}

	content = truncateContent(content)

	// Add debug logging
	log.Printf("Sending content to OpenAI (length: %d characters)", len(content))

	// If verbose logging is enabled, print the content
	if verbose {
		log.Println("Content:")
		log.Println(content)
	}

	client := openai.NewClient(apiKey)

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a professional document curator. Analyze the provided document for any dates in the format YYYY.MM.DD that indicate the document's creation or signature date. If one or more valid dates are found, use the latest date and generate a title in the following format: 'YYYY.MM.DD - Entity - Document Description - Recipient'. If no valid date is found, omit the date and generate the title as 'Entity - Document Description - Recipient'. Likewise, if the document lacks a recipient, omit that segment entirely—do not use any placeholder text such as 'Recipient'. Respond with only the title. For example, if the document contains the date '2024.03.31', the title should be '2024.03.31 - John Doe - Lohnabrechnung - Zürich'. If no date is found, then it should be 'Rafael Toledano Illán - Lohnabrechnung - Zürich'.",
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

// truncateContent shortens the input content if it exceeds the maximum allowed length,
// appending a suffix to indicate that the content has been truncated.
func truncateContent(content string) string {
	if len(content) > maxContentLength {
		return content[:maxContentLength-len(truncationSuffix)] + truncationSuffix
	}
	return content
}

// cleanTitle cleans and formats the generated title by removing extraneous quotes and whitespace,
// normalizing spacing around dashes, and ensuring proper text formatting.
func cleanTitle(title string) string {
	// Remove extraneous quotes and whitespace
	title = strings.Trim(title, "\"'\"'' \t\n")

	// Replace newlines with a single space
	title = strings.ReplaceAll(title, "\n", " ")

	// Ensure that dashes have a single space on each side
	title = regexp.MustCompile(`\s*-\s*`).ReplaceAllString(title, " - ")

	// Optionally, insert spaces between a lowercase letter followed immediately by an uppercase letter
	title = regexp.MustCompile(`([a-z])([A-Z])`).ReplaceAllString(title, "$1 $2")

	// Collapse any extra spaces into a single space
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")

	return title
}
