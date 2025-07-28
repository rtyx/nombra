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

# Build the binary with version information
go build -ldflags="-X main.version=$(git rev-parse --short HEAD)" -o nombra
```

### Make Nombra a Global Command
To use `nombra` from anywhere in your system, move the built binary to a directory included in your `PATH`, such as `/usr/local/bin`:

```sh
sudo mv nombra /usr/local/bin/
```

You can now run `nombra` from any directory:

```sh
nombra myfile.pdf
```

If `/usr/local/bin` is not in your `PATH`, you can check your `PATH` with:

```sh
echo $PATH
```

And add it to your shell configuration if needed.

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

### Checking the Version
Display the Git commit the binary was built from:
```sh
./nombra --version
```

### Adjusting Content Length
You can control how much text is sent to the language model. The
`--max-content-length` flag specifies the maximum number of characters that will
be included when generating a title. Experimenting with this value can help
strike a balance between providing enough context and keeping requests small:

```sh
./nombra myfile.pdf --max-content-length 5000
```

The minimum length required for processing can also be changed via
`--min-content-length`.

### Content Extraction Optimization
`extractTextFromPDF` concatenates the text from every page. When the combined
text would exceed the configured limit, Nombra keeps portions from the start and
end of the document and discards the middle. This strategy preserves important
sections like titles, parties, and dates even when large documents are truncated.

## Contributing
Pull requests and issues are welcome! Please follow these guidelines:
- Report bugs and feature requests via GitHub issues.
- Follow Go best practices and maintain code readability.
- Add comments where necessary to explain logic.

## License
This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

