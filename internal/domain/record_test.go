package domain

import "testing"

func TestParseFilterDate(t *testing.T) {
	start, end, err := ParseFilterDate("20260601")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start.Format("2006-01-02 15:04:05") != "2026-06-01 00:00:00" {
		t.Fatalf("unexpected start: %s", start)
	}
	if end.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("unexpected end: %s", end)
	}
	for _, raw := range []string{"2026-06-01", "2026061", "20261301", "20260230", "abc20260601"} {
		if _, _, err := ParseFilterDate(raw); err == nil {
			t.Fatalf("expected invalid date for %q", raw)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	cases := map[string]ContentType{
		"https://example.com/a": ContentURL,
		"42":                    ContentNumber,
		"3.14":                  ContentNumber,
		"hello":                 ContentText,
	}
	for input, want := range cases {
		if got := DetectContentType(input); got != want {
			t.Fatalf("DetectContentType(%q)=%s want %s", input, got, want)
		}
	}
}
