package skillbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
)

const RevisionSchemaVersion = "silieco.skill-revision/v1"
const SourceRuntime = "runtime"

// ToolDependency records the declared tool contract version for a revision.
// Order is meaningful: callers retain declared dependency precedence.
type ToolDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// RevisionSpec contains deliverable skill content only. Evaluation cases and
// held-out answers are stored separately and must not enter an agent bundle.
type RevisionSpec struct {
	Skill               Skill
	CompatibleContracts []string
	ToolDependencies    []ToolDependency
}

type RevisionManifest struct {
	SchemaVersion       string           `json:"schema_version"`
	Hash                string           `json:"hash"`
	BundleHash          string           `json:"bundle_hash"`
	SizeBytes           int64            `json:"size_bytes"`
	CompatibleContracts []string         `json:"compatible_contracts"`
	ToolDependencies    []ToolDependency `json:"tool_dependencies"`
}

// BuildRevisionManifest extends the existing bundle digest without changing
// hashes used by installed clients. A revision also pins compatibility and tools.
func BuildRevisionManifest(spec RevisionSpec) (RevisionManifest, error) {
	empty := RevisionManifest{}
	if strings.TrimSpace(spec.Skill.ID) == "" || strings.TrimSpace(spec.Skill.Name) == "" {
		return empty, fmt.Errorf("skill identity and name are required")
	}
	switch spec.Skill.Source {
	case SourceWorkspace, SourceBuiltin, SourceRuntime:
	default:
		return empty, fmt.Errorf("unsupported skill source")
	}
	if strings.TrimSpace(spec.Skill.Content) == "" {
		return empty, fmt.Errorf("SKILL.md content is required")
	}
	paths := map[string]bool{"skill.md": true}
	for _, file := range spec.Skill.Files {
		if file.Path == "" || file.Path == "." || strings.Contains(file.Path, ":") || path.IsAbs(file.Path) || path.Clean(file.Path) != file.Path || file.Path == ".." || strings.HasPrefix(file.Path, "../") || strings.ContainsAny(file.Path, "\\\x00") || paths[strings.ToLower(file.Path)] {
			return empty, fmt.Errorf("invalid or duplicated skill file path %q", file.Path)
		}
		paths[strings.ToLower(file.Path)] = true
	}
	contracts := append([]string{}, spec.CompatibleContracts...)
	sort.Strings(contracts)
	for i, contract := range contracts {
		if strings.TrimSpace(contract) == "" || contract != strings.TrimSpace(contract) || (i > 0 && contracts[i-1] == contract) {
			return empty, fmt.Errorf("contract references must be distinct nonempty values")
		}
	}
	dependencies := append([]ToolDependency{}, spec.ToolDependencies...)
	seen := map[string]bool{}
	for _, dependency := range dependencies {
		if strings.TrimSpace(dependency.Name) == "" || dependency.Name != strings.TrimSpace(dependency.Name) || strings.TrimSpace(dependency.Version) == "" || dependency.Version != strings.TrimSpace(dependency.Version) || seen[dependency.Name] {
			return empty, fmt.Errorf("tool dependencies require unique names and explicit versions")
		}
		seen[dependency.Name] = true
	}
	bundle := BuildManifest(spec.Skill)
	digest := sha256.New()
	writeHashPart(digest, RevisionSchemaVersion)
	writeHashPart(digest, bundle.Hash)
	writeHashPart(digest, "contracts")
	writeHashPart(digest, fmt.Sprint(len(contracts)))
	for _, contract := range contracts {
		writeHashPart(digest, contract)
	}
	writeHashPart(digest, "tools")
	writeHashPart(digest, fmt.Sprint(len(dependencies)))
	for _, dependency := range dependencies {
		writeHashPart(digest, dependency.Name)
		writeHashPart(digest, dependency.Version)
	}
	return RevisionManifest{SchemaVersion: RevisionSchemaVersion, Hash: "sha256:" + hex.EncodeToString(digest.Sum(nil)), BundleHash: bundle.Hash, SizeBytes: bundle.SizeBytes, CompatibleContracts: contracts, ToolDependencies: dependencies}, nil
}
