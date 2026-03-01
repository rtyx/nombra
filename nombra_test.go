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
		"Untitled":                         false,
		"Document":                         false,
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

func TestBuildTitleFromMetadata(t *testing.T) {
	cases := []struct {
		name     string
		metadata extractedMetadata
		want     string
		ok       bool
	}{
		{
			name: "date and title",
			metadata: extractedMetadata{
				Date:  "2024.01.15",
				Title: "Residence Permit Renewal",
			},
			want: "2024.01.15 - Residence Permit Renewal",
			ok:   true,
		},
		{
			name: "document type before names",
			metadata: extractedMetadata{
				Date:         "2007.07.03",
				DocumentType: "Authorization Letter",
				Author:       "John Doe",
				Recipient:    "Jane Doe",
			},
			want: "2007.07.03 - Authorization Letter - John Doe - Jane Doe",
			ok:   true,
		},
		{
			name: "topic fallback",
			metadata: extractedMetadata{
				Date:  "2020.10.31",
				Topic: "Diving Fitness Questionnaire",
			},
			want: "2020.10.31 - Diving Fitness Questionnaire",
			ok:   true,
		},
		{
			name: "prefer organization over signer and avoid verbose overlap",
			metadata: extractedMetadata{
				Date:         "2022.02.23",
				Title:        "Termination of Employment",
				DocumentType: "Termination Letter",
				Organization: "Acme Corp",
				Author:       "John Doe",
				Recipient:    "Jane Doe",
				Topic:        "Notice of termination of employment and related procedures",
			},
			want: "2022.02.23 - Termination of Employment - Acme Corp - Jane Doe",
			ok:   true,
		},
		{
			name: "strip bilingual organization and keep main language",
			metadata: extractedMetadata{
				Date:         "2021.12.22",
				Language:     "de",
				DocumentType: "COVID-Zertifikat",
				Organization: "Acme AG (Acme Corporation)",
				Topic:        "COVID-19 Impfung",
			},
			want: "2021.12.22 - COVID-Zertifikat - Acme AG - COVID-19 Impfung",
			ok:   true,
		},
		{
			name: "reject names only",
			metadata: extractedMetadata{
				Date:      "2007.07.03",
				Author:    "John Doe",
				Recipient: "Jane Doe",
			},
			ok: false,
		},
	}

	for _, tc := range cases {
		got, ok := buildTitleFromMetadata(tc.metadata)
		if ok != tc.ok {
			t.Errorf("%s: buildTitleFromMetadata() ok = %v; want %v", tc.name, ok, tc.ok)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: buildTitleFromMetadata() = %q; want %q", tc.name, got, tc.want)
		}
	}
}

func TestCompactTitle(t *testing.T) {
	in := "2022.02.23 - Termination of Employment - Notice of termination of employment and related procedures - Johannes Senn"
	got := compactTitle(in)
	if len(got) > maxFilenameLength {
		t.Fatalf("compactTitle produced overlong title: %d chars", len(got))
	}
	if strings.Contains(got, "Notice of termination of employment and related procedures") {
		t.Fatalf("compactTitle did not remove verbose overlapping part: %q", got)
	}
}

func TestStripTrailingTranslation(t *testing.T) {
	in := "Acme AG (Acme Corporation)"
	got := stripTrailingTranslation(in)
	want := "Acme AG"
	if got != want {
		t.Fatalf("stripTrailingTranslation(%q) = %q; want %q", in, got, want)
	}
}

func TestParseMetadataResponse(t *testing.T) {
	raw := "```json\n{\"date\":\"2024.01.15\",\"language\":\"de\",\"title\":\"Residence Permit Renewal\",\"document_type\":\"\",\"organization\":\"Office for Migration\",\"author\":\"\",\"recipient\":\"\",\"topic\":\"Residence Permit\"}\n```"
	got, err := parseMetadataResponse(raw)
	if err != nil {
		t.Fatalf("parseMetadataResponse returned error: %v", err)
	}
	if got.Date != "2024.01.15" || got.Language != "de" || got.Title != "Residence Permit Renewal" || got.Topic != "Residence Permit" || got.Organization != "Office for Migration" {
		t.Fatalf("unexpected metadata parsed: %+v", got)
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
