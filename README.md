# Nombra - AI-Powered PDF Filename Generator

## Overview
Nombra is a CLI tool that analyzes the content of PDF documents and generates meaningful file names using OpenAI's API. This is useful for organizing documents, automating metadata generation, and improving file management.

## Features
- Extracts text from PDFs
- Uses AI to generate relevant titles
- Supports OCR (via Tesseract) for scanned PDFs
- Handles special characters and long filenames safely
- Verbose mode for debugging

## Installation

### Prerequisites
- Go 1.24 or later
- OpenAI API Key (required for AI-generated titles)
- Optional: `tesseract-ocr` and `pdftoppm` for OCR functionality

### Install Nombra
```sh
# Clone the repository
git clone https://github.com/YOUR_USERNAME/nombra.git
cd nombra

# Build the binary
go build -o nombra
```

## Usage

### Basic usage
```sh
./nombra myfile.pdf
```
This will generate a title based on the PDF’s content and rename the file accordingly.

### Using an API key
```sh
./nombra myfile.pdf --key YOUR_OPENAI_API_KEY
```
Or set the environment variable:
```sh
export OPENAI_API_KEY=your_api_key
./nombra myfile.pdf
```

### Forcing OCR
While nombra automatically opts for OCR when needed, you can also force it:
```sh
./nombra myfile.pdf --ocr
```

### Verbose Mode
```sh
./nombra myfile.pdf --verbose
```

### Selecting an OpenAI model
By default, Nombra uses `gpt-3.5-turbo` for title generation. You can choose a
different model with the `--model` flag:
```sh
./nombra myfile.pdf --model gpt-4-turbo
```

## Contributing
Pull requests and issues are welcome! Please follow these guidelines:
- Report bugs and feature requests via GitHub issues.
- Follow Go best practices and maintain code readability.
- Add comments where necessary to explain logic.

## License
This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

