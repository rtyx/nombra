package main

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		" \"2024.03.31 -John Doe-Title\" \n": "2024.03.31 - John Doe - Title",
		"FooBar":                             "Foo Bar",
		"  Something-Else  ":                 "Something - Else",
		"Taucher Medizincheck - Kandidaten - Fragebogen - 2020.10.31": "2020.10.31 - Taucher Medizincheck - Kandidaten - Fragebogen",
		"Invoice - ACME Corp - 2024-01-15":                           "2024.01.15 - Invoice - ACME Corp",
		"Residence Permit Renew.pdf":                                   "Residence Permit Renew",
	}

	for in, want := range cases {
		got := cleanTitle(in)
		if got != want {
			t.Errorf("cleanTitle(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestIsLikelyFilename(t *testing.T) {
	cases := map[string]bool{
		"2024.01.15 - Invoice - ACME Corp": true,
		"Residence Permit Renew":           true,
		"No clear document type or parties are mentioned in the text. A descriptive filename could be 'Residence Permit Renew": false,
		"The filename should be: Residence Permit Renewal": false,
		"Filename: Residence Permit Renewal":              false,
	}

	for in, want := range cases {
		got := isLikelyFilename(in)
		if got != want {
			t.Errorf("isLikelyFilename(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestValidateModel(t *testing.T) {
	if err := validateModel(openai.GPT3Dot5Turbo); err != nil {
		t.Errorf("validateModel returned error for valid model: %v", err)
	}

	err := validateModel("invalid-model")
	if err == nil {
		t.Fatalf("expected error for invalid model")
	}

	// ensure the error message lists valid models
	for _, m := range validModels {
		if !strings.Contains(err.Error(), m) {
			t.Fatalf("error message does not contain %q", m)
		}
	}
}
