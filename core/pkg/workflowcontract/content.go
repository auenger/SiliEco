package workflowcontract

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

const MaxArtifactBytes = 16 << 20

// ValidateArtifactContent checks the supported wire formats, not domain truth.
// Domain schemas and acceptance decisions remain separate from file validation.
func ValidateArtifactContent(mediaType string, content []byte) error {
	if len(content) == 0 || len(content) > MaxArtifactBytes {
		return fmt.Errorf("artifact must contain between 1 byte and 16 MiB")
	}
	switch mediaType {
	case "application/json":
		if !utf8.Valid(content) {
			return fmt.Errorf("JSON must be UTF-8")
		}
		return checkJSON(content)
	case "text/plain", "text/markdown", "text/csv":
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return fmt.Errorf("text artifact must be UTF-8 without NUL bytes")
		}
		return nil
	default:
		return fmt.Errorf("unsupported artifact media type %q", mediaType)
	}
}
