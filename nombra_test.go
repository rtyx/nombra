package main

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	longInput := strings.Repeat("a", maxFilenameLength+10)
	tests := []struct {
		name      string
		input     string
		expected  string
		expectLen int
	}{
		{
			name:     "InvalidCharactersRemoved",
			input:    "inval<id>fi:lena\"me/with\\bad|chars?*",
			expected: "invalidfilenamewithbadchars",
		},
		{
			name:     "TrimSpaces",
			input:    "  My Document  ",
			expected: "My Document",
		},
		{
			name:      "TruncateLong",
			input:     longInput,
			expectLen: maxFilenameLength,
		},
		{
			name:     "EmptyTitleWhitespace",
			input:    "   \t  ",
			expected: "untitled-document",
		},
		{
			name:     "EmptyTitleInvalidChars",
			input:    "<>:\"/\\|?*",
			expected: "untitled-document",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			if tc.expectLen != 0 {
				if len(got) != tc.expectLen {
					t.Errorf("expected length %d, got %d", tc.expectLen, len(got))
				}
			}
			if got != tc.expected && tc.expected != "" {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
