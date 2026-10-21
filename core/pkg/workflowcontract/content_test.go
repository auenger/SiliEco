package workflowcontract

import "testing"

func TestWorkflowContent(t *testing.T) {
	for _, tc := range []struct {
		name, media, content string
		valid                bool
	}{
		{"json", "application/json", `{"temperature":80}`, true},
		{"duplicate keys", "application/json", `{"temperature":80,"temperature":90}`, false},
		{"trailing JSON", "application/json", `{} {}`, false},
		{"broken JSON", "application/json", `{"x":`, false},
		{"markdown", "text/markdown", "# Report\n温度正常", true},
		{"binary text", "text/plain", "abc\x00def", false},
		{"invalid UTF8", "text/plain", "\xff", false},
		{"unknown format", "application/pdf", "%PDF-1.7", false},
		{"empty", "text/plain", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateArtifactContent(tc.media, []byte(tc.content))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
