package handler

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"/":                "",
		"uploads":          "uploads",
		"/uploads/":        "uploads",
		"//a//b//":         "a/b",
		"cloudreve/prod":   "cloudreve/prod",
		"../x":             "x",
		"a/../b":           "a/b",
		"  /foo/bar/  ":    "foo/bar",
	}
	for in, want := range cases {
		if got := normalizeBasePath(in); got != want {
			t.Errorf("normalizeBasePath(%q) = %q, want %q", in, got, want)
		}
	}
}
