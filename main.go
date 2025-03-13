package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ledongthuc/pdf"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxFilenameLength = 120
	truncationSuffix  = "... [content truncated]"
	sanitizeRegex     = `[<>:"\/\\|?*]`
)

var (
	verbose          bool
	maxContentLength = 3000
	ocr              bool
)

func main() {
	var apiKey string

	rootCmd := &cobra.Command{
		Use:   "nombra [PDF file]",
		Short: "Generate titles for PDF documents using AI",
		Long:  "A CLI tool that analyzes PDF content and generates appropriate titles using OpenAI's API",
		Args:  cobra.ExactArgs(1),
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

			title, err := generateOpenAITitle(textContent, apiKey)
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
	rootCmd.Flags().IntVar(&maxContentLength, "max-content-length", 3000, "Maximum content length for processing")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "OpenAI API key (default: $OPENAI_API_KEY)")

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

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

func extractPDFContent(path string) (string, error) {
	// Force OCR if specified
	if ocr {
		return extractTextViaOCR(path)
	}

	// First try standard text extraction
	text, err := extractTextFromPDF(path)
	if err == nil && text != "" {
		return text, nil
	}

	// Fallback to OCR if text extraction failed
	log.Println("Standard text extraction failed, attempting OCR...")
	return extractTextViaOCR(path)
}

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

func generateOpenAITitle(content, apiKey string) (string, error) {
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
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a professional document curator. Analyze the provided document for any dates in the format YYYY.MM.DD that indicate the document’s creation or signature date. If one or more valid dates are found, use the latest date and generate a title in the following format: 'YYYY.MM.DD - Entity - Document Description - Recipient'. If no valid date is found, omit the date and generate the title as 'Entity - Document Description - Recipient'. Likewise, if the document lacks a recipient, omit that segment entirely—do not use any placeholder text such as 'Recipient'. Respond with only the title. For example, if the document contains the date '2024.03.31', the title should be '2024.03.31 - Rafael Toledano Illán - Lohnabrechnung - Zürich'. If no date is found, then it should be 'Rafael Toledano Illán - Lohnabrechnung - Zürich'.",
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
	// Remove extraneous quotes and whitespace
	title = strings.Trim(title, "\"'“”‘’ \t\n")

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
