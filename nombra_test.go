package main

import "testing"

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
