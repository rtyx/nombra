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
	}

	for in, want := range cases {
		got := cleanTitle(in)
		if got != want {
			t.Errorf("cleanTitle(%q) = %q; want %q", in, got, want)
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
