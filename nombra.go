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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ledongthuc/pdf"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
)

const (
	maxFilenameLength = 120
	truncationSuffix  = "... [content truncated]"
	sanitizeRegex     = `[<>:"\/\\|?*]`
	visionModel       = openai.GPT4oMini
	titlePrompt       = "You generate filename stems for PDF documents.\n\nFirst, identify these fields from the document:\n- title: the explicit document title or heading, if present\n- document type: what this document actually is\n- author: who issued, authored, or signed the document\n- recipient: who the document is addressed to\n- topic: the main subject or purpose\n- date: the most relevant document date (creation, issue, signature, or effective date)\n\nUse these questions to guide extraction:\n- What is this document?\n- What does it look like it is for?\n- Is there a visible title or heading?\n- Who is authoring or issuing this document?\n- To whom is this document addressed?\n- What is the main topic?\n- What is the date the document was written or issued?\n\nThen produce exactly one filename stem and nothing else.\n\nFormatting rules:\n- If a date is used, it must appear only at the beginning as YYYY.MM.DD\n- Never place a date at the end; move it to the front instead\n- Prefer an explicit title when it is meaningful and specific\n- If there is no useful title, identify what the document is and name that first\n- When there is no title, ask yourself: what is this, and what does it look like? Use the answer as the basis for the filename\n- Do not default to just listing person names when the document itself can be identified\n- Otherwise prefer document type plus the most relevant people or entities\n- If author and recipient are both clear, include them when useful\n- If no clear document type exists, use the topic or title instead of inventing a type\n- Do not include quotes\n- Do not include a file extension\n- Do not explain your reasoning\n- Do not mention uncertainty\n- Do not write sentences like 'No clear document type...' or 'A descriptive filename could be...'\n- Use only letters, numbers, spaces, and hyphens\n- Keep the result concise\n\nUse the first matching pattern:\n- YYYY.MM.DD - [Title]\n- YYYY.MM.DD - [Document Type] - [Author] - [Recipient]\n- YYYY.MM.DD - [Document Type] - [Topic or Entity]\n- [Title]\n- [Document Type] - [Author] - [Recipient]\n- [Document Type] - [Topic]\n- [Topic]\n\nGood outputs:\n2025.05.07 - Sublease Agreement - John Doe - Jane Smith\n2024.01.15 - Invoice - ACME Corp\nResidence Permit Renewal\nMedical Questionnaire - Diving Fitness\n\nBad outputs:\nNo clear document type or parties are mentioned in the text\nA descriptive filename could be Residence Permit Renewal\nFilename: Residence Permit Renewal\nResidence Permit Renewal.pdf\n2007.07.03 - Rafael Toledano Illan - Rafael Toledano Cantero"
	retryTitlePrompt  = "You generate filename stems for PDF documents.\n\nThe previous filename attempt was invalid because it was too generic, explanatory, not filename-like, or focused on names instead of identifying the document. Re-read the document and try again with a different strategy.\n\nStrategy:\n- First ask: what is this document, and what does it look like it is for?\n- Prefer a specific visible title or heading if present\n- Otherwise identify the concrete document type, form, letter, certificate, permit, application, report, questionnaire, or other recognizable document kind\n- Only include person names after the document itself has been identified\n- Never return placeholders like Untitled, Document, File, Scan, Image, PDF, or Unknown\n- If the document is ambiguous, choose the most specific subject or document kind you can support from the text\n- If a date is present, it must appear only at the beginning as YYYY.MM.DD\n- Never place a date at the end\n\nReturn exactly one filename stem and nothing else.\nDo not explain your reasoning.\nDo not include quotes.\nDo not include a file extension.\nUse only letters, numbers, spaces, and hyphens."
)

var (
	verbose          bool
	version          = "dev"
	maxContentLength = 3000
	minContentLength = 10
	ocr              bool
	model            string
	dryRun           bool
	printOnly        bool
	interactive      bool
	workers          int
	inputDir         string
)

type fileJob struct {
	index int
	path  string
}

type fileResult struct {
	index   int
	path    string
	title   string
	newPath string
	skipped bool
	err     error
}

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
		Use:     "nombra [PDF file ...]",
		Short:   "Generate titles for PDF documents using AI",
		Long:    "A CLI tool that analyzes PDF content and generates appropriate titles using OpenAI's API",
		Example: "nombra myfile.pdf\n  nombra myfile1.pdf myfile2.pdf --workers 4\n  nombra --dir ./docs --workers 6\n  nombra myfile.pdf --model gpt-4-turbo",
		Version: version,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && strings.TrimSpace(inputDir) == "" {
				return fmt.Errorf("provide at least one PDF file or use --dir")
			}
			return nil
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			if apiKey == "" {
				apiKey = os.Getenv("OPENAI_API_KEY")
			}
			if apiKey == "" {
				fmt.Println("Error: API key required. Use --key or set OPENAI_API_KEY")
				os.Exit(1)
			}
			if printOnly && dryRun {
				fmt.Println("Error: --print-only cannot be combined with --dry-run")
				os.Exit(1)
			}
			if printOnly && interactive {
				fmt.Println("Error: --print-only cannot be combined with --interactive")
				os.Exit(1)
			}
			if dryRun && interactive {
				fmt.Println("Error: --dry-run cannot be combined with --interactive")
				os.Exit(1)
			}
			if err := validateModel(model); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if workers < 1 {
				fmt.Println("Error: --workers must be at least 1")
				os.Exit(1)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			if verbose {
				log.Println("Verbose mode enabled")
			}

			files, err := collectInputPDFs(args, inputDir)
			if err != nil {
				fmt.Printf("Input error: %v\n", err)
				os.Exit(1)
			}

			if interactive && len(files) > 1 {
				fmt.Println("Error: --interactive supports only one file at a time")
				os.Exit(1)
			}

			if workers > len(files) {
				workers = len(files)
			}

			client := openai.NewClient(apiKey)
			results := processFiles(files, client, workers)

			var successCount, failedCount, skippedCount int
			for _, result := range results {
				if result.err != nil {
					failedCount++
					fmt.Printf("[FAIL] %s: %v\n", filepath.Base(result.path), result.err)
					continue
				}

				if result.skipped {
					skippedCount++
					fmt.Printf("[SKIP] %s: rename cancelled\n", filepath.Base(result.path))
					continue
				}

				successCount++
				switch {
				case printOnly:
					fmt.Printf("%s: %s\n", filepath.Base(result.path), result.title)
				case dryRun:
					fmt.Printf("Dry run (no changes made):\n  %s\n  -> %s\n\n", filepath.Base(result.path), filepath.Base(result.newPath))
				default:
					fmt.Printf("Successfully renamed:\n  %s\n  -> %s\n\n", filepath.Base(result.path), filepath.Base(result.newPath))
				}
			}

			if len(files) > 1 {
				fmt.Printf("Summary: %d succeeded, %d skipped, %d failed (total: %d)\n", successCount, skippedCount, failedCount, len(files))
			}

			if failedCount > 0 {
				os.Exit(1)
			}
		},
	}

	// Configure flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().BoolVarP(&ocr, "ocr", "o", false, "Force OCR text extraction")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", openai.GPT3Dot5Turbo, "OpenAI model to use")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the new filename without renaming")
	rootCmd.Flags().BoolVar(&printOnly, "print-only", false, "Print only the generated title")
	rootCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Ask for confirmation before renaming")
	rootCmd.Flags().StringVar(&inputDir, "dir", "", "Directory containing PDF files to process")
	rootCmd.Flags().IntVarP(&workers, "workers", "w", runtime.NumCPU(), "Number of files to process concurrently")
	rootCmd.Flags().IntVarP(&maxContentLength, "max-content-length", "l", 3000, "Maximum content length for processing")
	rootCmd.Flags().IntVarP(&minContentLength, "min-content-length", "n", 10, "Minimum content length required for processing")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "OpenAI API key (default: $OPENAI_API_KEY)")

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func collectInputPDFs(args []string, dir string) ([]string, error) {
	candidates := append([]string{}, args...)

	if dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read --dir %q: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.EqualFold(filepath.Ext(name), ".pdf") {
				candidates = append(candidates, filepath.Join(dir, name))
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no PDF files found")
	}

	seen := make(map[string]struct{}, len(candidates))
	files := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !strings.EqualFold(filepath.Ext(candidate), ".pdf") {
			return nil, fmt.Errorf("not a PDF file: %s", candidate)
		}
		info, err := os.Stat(candidate)
		if err != nil {
			return nil, fmt.Errorf("file not accessible %q: %w", candidate, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("path is a directory, expected PDF file: %s", candidate)
		}

		abs, err := filepath.Abs(candidate)
		if err != nil {
			abs = candidate
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		files = append(files, abs)
	}

	sort.Strings(files)
	return files, nil
}

func processFiles(files []string, client *openai.Client, workerCount int) []fileResult {
	jobs := make(chan fileJob)
	results := make(chan fileResult, len(files))
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result := processSingleFile(job.path, client)
				result.index = job.index
				result.path = job.path
				results <- result
			}
		}()
	}

	for i, path := range files {
		jobs <- fileJob{index: i, path: path}
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]fileResult, 0, len(files))
	for result := range results {
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].index < out[j].index
	})
	return out
}

func processSingleFile(filePath string, client *openai.Client) fileResult {
	textContent, err := extractPDFContent(filePath, client)
	if err != nil {
		return fileResult{err: fmt.Errorf("PDF processing error: %w", err)}
	}

	title, err := generateOpenAITitle(textContent, client, model)
	if err != nil {
		return fileResult{err: fmt.Errorf("title generation failed: %w", err)}
	}

	if printOnly {
		return fileResult{title: title}
	}

	if dryRun {
		return fileResult{title: title, newPath: buildProposedPath(filePath, title)}
	}

	if interactive && !confirmRename(filePath, title) {
		return fileResult{title: title, skipped: true}
	}

	newPath, err := safeRenameFile(filePath, title)
	if err != nil {
		return fileResult{err: fmt.Errorf("renaming failed: %w", err)}
	}
	return fileResult{title: title, newPath: newPath}
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

	newPath := buildProposedPath(originalPath, cleanTitle)
	dir := filepath.Dir(originalPath)
	ext := filepath.Ext(originalPath)

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

func buildProposedPath(originalPath, title string) string {
	dir := filepath.Dir(originalPath)
	ext := filepath.Ext(originalPath)
	baseName := sanitizeFilename(title) + ext
	if len(baseName) > maxFilenameLength {
		baseName = baseName[:maxFilenameLength-len(ext)] + ext
	}
	return filepath.Join(dir, baseName)
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
// If that fails or produces empty content, it falls back to OCR-based extraction,
// then to image analysis via OpenAI as a last resort.
func extractPDFContent(path string, client *openai.Client) (string, error) {
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
			return extractContentViaVisionFallback(path, client, err)
		}
		if err := validateContentLength(text); err != nil {
			return extractContentViaVisionFallback(path, client, err)
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
	return extractContentViaVisionFallback(path, client, lastErr)
}

func extractContentViaVisionFallback(path string, client *openai.Client, textExtractionErr error) (string, error) {
	if verbose {
		log.Printf("Text extraction failed; attempting OpenAI image analysis fallback (model: %s)...", visionModel)
	}

	description, err := describePDFImage(path, client)
	if err == nil && strings.TrimSpace(description) != "" {
		if verbose {
			log.Printf("Vision fallback succeeded (description length: %d characters)", len(description))
		}
		return description, nil
	}

	if textExtractionErr != nil && err != nil {
		return "", fmt.Errorf("all text extraction methods failed: %v; vision fallback failed: %w", textExtractionErr, err)
	}
	if textExtractionErr != nil {
		return "", fmt.Errorf("all text extraction methods failed: %w", textExtractionErr)
	}
	if err != nil {
		return "", fmt.Errorf("no text could be extracted from the PDF and vision fallback failed: %w", err)
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
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}
		content.WriteString(trimmed)
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

	// Ensure OCR processes pages in numeric order (page-2 before page-10).
	sort.Slice(pages, func(i, j int) bool {
		pi := pageNumberFromPath(pages[i])
		pj := pageNumberFromPath(pages[j])
		if pi == pj {
			return pages[i] < pages[j]
		}
		return pi < pj
	})

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

// describePDFImage analyzes the first page of the PDF as an image and returns
// a short plain-text description that can be used for title generation.
func describePDFImage(pdfPath string, client *openai.Client) (string, error) {
	tempDir, err := os.MkdirTemp("", "nombra-vision")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory for vision fallback: %w", err)
	}
	defer os.RemoveAll(tempDir)

	imagePrefix := filepath.Join(tempDir, "page-1")
	cmd := exec.Command("pdftoppm", "-f", "1", "-singlefile", "-png", pdfPath, imagePrefix)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftoppm failed for vision fallback: %w", err)
	}

	imagePath := imagePrefix + ".png"
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read rendered page image: %w", err)
	}

	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: visionModel,
			Messages: []openai.ChatCompletionMessage{
				{
					Role: openai.ChatMessageRoleSystem,
					Content: "You analyze document images. Describe what the image likely is so a filename can be generated. " +
						"Include document type, visible entities, and date if readable. " +
						"If text is unreadable, give a concise visual description. Respond in plain text only.",
				},
				{
					Role: openai.ChatMessageRoleUser,
					MultiContent: []openai.ChatMessagePart{
						{
							Type: openai.ChatMessagePartTypeText,
							Text: "Describe this PDF page image for naming the file.",
						},
						{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL:    dataURL,
								Detail: openai.ImageURLDetailHigh,
							},
						},
					},
				},
			},
			Temperature: 0.2,
		},
	)
	if err != nil {
		return "", fmt.Errorf("OpenAI vision API error: %w", err)
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("empty response from OpenAI vision API")
	}

	return resp.Choices[0].Message.Content, nil
}

func pageNumberFromPath(path string) int {
	matches := regexp.MustCompile(`page-(\d+)\.png$`).FindStringSubmatch(filepath.Base(path))
	if len(matches) != 2 {
		return 0
	}

	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}

	return n
}

func confirmRename(filePath, title string) bool {
	ext := filepath.Ext(filePath)
	proposedName := sanitizeFilename(title) + ext
	if len(proposedName) > maxFilenameLength {
		proposedName = proposedName[:maxFilenameLength-len(ext)] + ext
	}

	fmt.Printf("Rename file?\n  %s\n  -> %s\nProceed? [y/N]: ", filepath.Base(filePath), proposedName)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// generateOpenAITitle sends the extracted PDF content to OpenAI's Chat Completion API
// to generate an appropriate title based on specific formatting rules.
// It then cleans the returned title to ensure proper formatting.
func generateOpenAITitle(content string, client *openai.Client, model string) (string, error) {
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

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: titlePrompt,
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

	rawTitle := resp.Choices[0].Message.Content
	title := cleanTitle(rawTitle)
	if isLikelyFilename(title) {
		return title, nil
	}

	repairedTitle, err := retryOpenAITitle(content, rawTitle, client, model)
	if err != nil {
		return "", err
	}

	title = cleanTitle(repairedTitle)
	if !isLikelyFilename(title) {
		return "", fmt.Errorf("model returned invalid filename output")
	}

	return title, nil
}

func retryOpenAITitle(content, rejectedTitle string, client *openai.Client, model string) (string, error) {
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: retryTitlePrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: fmt.Sprintf("Rejected filename: %s\n\nDocument text:\n%s", rejectedTitle, content),
				},
			},
			Temperature: 0,
		},
	)
	if err != nil {
		return "", fmt.Errorf("OpenAI retry error: %w", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from OpenAI retry")
	}
	return resp.Choices[0].Message.Content, nil
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

	title = regexp.MustCompile(`(?i)\.pdf$`).ReplaceAllString(title, "")

	title = moveTrailingDateToFront(strings.TrimSpace(title))

	// Trim any trailing or leading whitespace introduced by replacements
	return strings.TrimSpace(title)
}

func isLikelyFilename(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}

	disallowedPhrases := []string{
		"no clear",
		"mentioned in the text",
		"descriptive filename could be",
		"filename should be",
		"filename could be",
		"document type",
		"do not",
		"respond only",
	}
	disallowedTitles := map[string]struct{}{
		"untitled":         {},
		"document":         {},
		"file":             {},
		"pdf":              {},
		"scan":             {},
		"scanned document": {},
		"image":            {},
		"unknown":          {},
		"misc":             {},
		"miscellaneous":    {},
	}
	lowerTitle := strings.ToLower(title)
	if _, found := disallowedTitles[lowerTitle]; found {
		return false
	}
	for _, phrase := range disallowedPhrases {
		if strings.Contains(lowerTitle, phrase) {
			return false
		}
	}

	if strings.ContainsAny(title, "\n\r") {
		return false
	}

	return !strings.Contains(title, ":")
}

func moveTrailingDateToFront(title string) string {
	datePattern := `\d{4}(?:\s*[./-]\s*)\d{2}(?:\s*[./-]\s*)\d{2}`
	leadingDate := regexp.MustCompile(`^` + datePattern + `(?:\s+-\s+|$)`)
	if leadingDate.MatchString(title) {
		return title
	}

	trailingDate := regexp.MustCompile(`^(.*?)(?:\s+-\s+)(` + datePattern + `)$`)
	matches := trailingDate.FindStringSubmatch(title)
	if len(matches) != 3 {
		return title
	}

	body := strings.TrimSpace(matches[1])
	date := normalizeDateSeparators(matches[2])
	if body == "" {
		return date
	}
	return date + " - " + body
}

func normalizeDateSeparators(date string) string {
	date = regexp.MustCompile(`\s*([./-])\s*`).ReplaceAllString(date, "$1")
	return strings.NewReplacer("/", ".", "-", ".").Replace(date)
}
