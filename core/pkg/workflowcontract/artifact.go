package workflowcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Artifact is a local submission descriptor. It intentionally has no accepted,
// review_status, execution_id or revision fields: only the server assigns those.
// AttachmentID refers to the existing workspace attachment store, never a URL.
type Artifact struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	MediaType     string `json:"media_type"`
	SchemaVersion string `json:"schema_version"`
	AttachmentID  string `json:"attachment_id"`
	Checksum      string `json:"checksum"`
}

func Checksum(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// ValidateArtifacts checks manifest structure only. The server must additionally
// authorize every attachment in the workspace, fetch its immutable bytes and
// verify Checksum(bytes) before persisting an artifact revision. A client digest
// or MIME declaration is not proof of content, provenance or business validity.
func (c Contract) ValidateArtifacts(artifacts []Artifact) ([]Violation, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	requirements := make(map[string]ArtifactRequirement, len(c.Artifacts))
	for _, requirement := range c.Artifacts {
		requirements[requirement.Name] = requirement
	}
	seen := map[string]bool{}
	attachments := map[string]bool{}
	var violations []Violation
	for i, artifact := range artifacts {
		path := fmt.Sprintf("/artifacts/%d", i)
		add := func(field, code, message string) {
			violations = append(violations, Violation{path + field, code, message})
		}
		requirement, exists := requirements[artifact.Name]
		if !exists {
			add("/name", "unknown_artifact", "artifact is not declared in contract")
		}
		if seen[artifact.Name] {
			add("/name", "duplicate_artifact", "artifact name must be unique")
		}
		seen[artifact.Name] = true
		if exists {
			if artifact.Kind != requirement.Kind {
				add("/kind", "kind", "artifact kind does not match contract")
			}
			if artifact.MediaType != requirement.MediaType {
				add("/media_type", "media_type", "artifact media type does not match contract")
			}
			if artifact.SchemaVersion != requirement.SchemaVersion {
				add("/schema_version", "schema_version", "artifact schema version does not match contract")
			}
		}
		if !canonicalUUID(artifact.AttachmentID) {
			add("/attachment_id", "attachment_id", "expected canonical attachment UUID")
		}
		if attachments[artifact.AttachmentID] {
			add("/attachment_id", "duplicate_attachment", "one attachment cannot satisfy multiple artifacts")
		}
		attachments[artifact.AttachmentID] = true
		digest := strings.TrimPrefix(artifact.Checksum, "sha256:")
		bytes, err := hex.DecodeString(digest)
		if !strings.HasPrefix(artifact.Checksum, "sha256:") || err != nil || len(bytes) != 32 || strings.ToLower(digest) != digest {
			add("/checksum", "checksum", "expected sha256: followed by 64 lowercase hex digits")
		}
	}
	for _, requirement := range c.Artifacts {
		if requirement.Required && !seen[requirement.Name] {
			violations = append(violations, Violation{"/artifacts", "required_artifact", "missing required artifact: " + requirement.Name})
		}
	}
	return violations, nil
}

func canonicalUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	digits := strings.ReplaceAll(id, "-", "")
	decoded, err := hex.DecodeString(digits)
	return err == nil && len(decoded) == 16 && digits == strings.ToLower(digits) && digits != strings.Repeat("0", 32)
}

// Submission is the entire client-owned result payload. Server-owned identity,
// accepted revisions and gate decisions are intentionally not part of it.
type Submission struct {
	Data      json.RawMessage `json:"data"`
	Artifacts []Artifact      `json:"artifacts"`
}

func (c Contract) ValidateSubmission(raw []byte) ([]Violation, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	var result Submission
	if err := strictDecode(raw, &result); err != nil {
		return []Violation{{"", "invalid_submission", err.Error()}}, nil
	}
	dataViolations, err := c.ValidateData(result.Data)
	if err != nil {
		return nil, err
	}
	artifactViolations, err := c.ValidateArtifacts(result.Artifacts)
	if err != nil {
		return nil, err
	}
	violations := append(dataViolations, artifactViolations...)
	if violations == nil {
		violations = []Violation{}
	}
	return violations, nil
}
