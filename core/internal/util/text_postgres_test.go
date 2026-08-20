package util

import (
	"strings"
	"testing"
)

func TestSanitizeTextForPostgres(t *testing.T) {
	t.Parallel()

	if got, want := SanitizeTextForPostgres("before\x00after"), "beforeafter"; got != want {
		t.Fatalf("SanitizeTextForPostgres() = %q, want %q", got, want)
	}
	if got := SanitizeTextForPostgres(string([]byte{'a', 0xff, 'b'})); got != "a\uFFFDb" {
		t.Fatalf("invalid UTF-8 replacement = %q", got)
	}
}

func TestSanitizeJSONForPostgres(t *testing.T) {
	t.Parallel()

	got := SanitizeJSONForPostgres(map[string]any{
		"bad\x00key": []any{"left\x00right", map[string]any{"nested": "ok\x00"}},
	}).(map[string]any)
	if _, ok := got["badkey"]; !ok {
		t.Fatalf("sanitized key missing: %#v", got)
	}
	if strings.ContainsRune(got["badkey"].([]any)[0].(string), 0) {
		t.Fatalf("nested NUL survived: %#v", got)
	}
}
