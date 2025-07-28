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
	version          = "dev"
	maxContentLength = 3000
	minContentLength = 10
	ocr              bool
	model            string
)

// validModels lists the OpenAI models that can be used with the --model flag.
// The slice is used for validating user input and constructing helpful error
// messages when an unsupported model is supplied.
var validModels = []string{
	openai.GPT3Dot5Turbo,
	openai.GPT3Dot5Turbo0125,
	openai.GPT3Dot5Turbo1106,
	openai.GPT3Dot5Turbo16K,
	openai.GPT4Turbo,
	openai.GPT4Turbo0125,
	openai.GPT4Turbo1106,
	openai.GPT4TurboPreview,
	openai.GPT4Turbo20240409,
	openai.GPT4,
	openai.GPT4o,
	openai.GPT4oMini,
	openai.GPT4VisionPreview,
}

// validateModel ensures the provided model is one of the supported values.
// It returns an error listing the allowed models when validation fails.
func validateModel(m string) error {
	for _, v := range validModels {
		if m == v {
			return nil
		}
	}
	return fmt.Errorf("invalid model %q. valid models: %s", m, strings.Join(validModels, ", "))
}

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
		Version: version,
		Args:    cobra.ExactArgs(1),
		PreRun: func(cmd *cobra.Command, args []string) {
			if apiKey == "" {
				apiKey = os.Getenv("OPENAI_API_KEY")
			}
			if apiKey == "" {
				fmt.Println("Error: API key required. Use --key or set OPENAI_API_KEY")
				os.Exit(1)
			}
			if err := validateModel(model); err != nil {
				fmt.Println(err)
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
// If the combined text becomes very long, consider trimming to include mostly
// the beginning and end of the document where titles, parties, and dates are
// usually located. This could yield better context for title generation when
// approaching the `maxContentLength` limit.
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
					Content: "You are a professional document curator. Carefully read the provided text and generate a concise filename describing the document.\n\n1. Identify the document type or category in plain language (e.g., Contract, Invoice, Report, Sublease Agreement, Pay Slip).\n2. Identify the main parties or entities involved.\n3. Find the most relevant date (creation, signing, effective, due) and format it as YYYY.MM.DD.\n4. Determine the core subject or topic if needed.\n\nConstruct the filename using these elements with the following priority:\n- If date, type, and parties are present: 'YYYY.MM.DD - [Document Type] - [Party1] - [Party2]'.\n- If date and type are found: 'YYYY.MM.DD - [Document Type] - [Subject or Party]'.\n- If type and parties are found (no clear date): '[Document Type] - [Party1] - [Party2]'.\n- If type and subject are found: '[Document Type] - [Subject]'.\n- If only the type is clear: '[Document Type] - [Key Detail or Subject]'.\n- As a last resort, output a short descriptive phrase summarizing the document.\n\nUse only standard characters (letters, numbers, spaces, hyphens). Keep the title concise and omit placeholder text. Respond only with the filename.\n\nExample: A document mentioning 'Untermietvertrag', 'John Doe', 'Jane Smith' and the date '7.5.2025' should yield '2025.05.07 - Sublease Agreement - John Doe - Jane Smith'. Another example: 'Invoice from ACME Corp dated 15 January 2024' should yield '2024.01.15 - Invoice - ACME Corp'.",
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
// truncateContent ensures the most relevant parts of the document are
// preserved when limiting the text sent to OpenAI. When the content exceeds
// the configured maximum length, the function keeps portions from both the
// beginning and end of the text. A suffix is inserted between the two segments
// to indicate that the middle section has been omitted.
func truncateContent(content string) string {
	if len(content) <= maxContentLength {
		return content
	}

	// Ensure we always keep at least 1 character from start and end
	minKeep := 1
	available := maxContentLength - len(truncationSuffix)
	if available < 2*minKeep {
		// Not enough space for both start and end, just return the start
		return content[:maxContentLength]
	}

	keep := available / 2
	start := content[:keep]
	end := content[len(content)-keep:]
	return start + truncationSuffix + end
}

// cleanTitle cleans and formats the generated title by removing extraneous quotes and whitespace,
// normalizing spacing around dashes, and ensuring proper text formatting.
func cleanTitle(title string) string {
	// Remove extraneous quotes and surrounding whitespace
	title = strings.Trim(title, "\"' \t\n")

	// Replace newlines with a single space
	title = strings.ReplaceAll(title, "\n", " ")

	// Insert spaces between a lowercase letter followed immediately by an uppercase letter
	title = regexp.MustCompile(`([a-z])([A-Z])`).ReplaceAllString(title, "$1 $2")

	// Collapse runs of whitespace
	title = regexp.MustCompile(`\s+`).ReplaceAllString(title, " ")

	// Ensure dashes have a single space on each side
	title = regexp.MustCompile(`\s*-\s*`).ReplaceAllString(title, " - ")

	// Trim any trailing or leading whitespace introduced by replacements
	return strings.TrimSpace(title)
}
