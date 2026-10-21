// Package skilleval defines immutable evaluation inputs and deterministic checks.
// It performs no I/O. Workspace authorization, isolated execution, budgets and
// publication decisions belong to the owning services, not this package.
package skilleval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

const SchemaVersion = "silieco.skill-evaluation-suite/v1"
const ValidatorVersion = "silieco.skill-evaluation-validator/v1"
const MaxSuiteBytes = 8 << 20
const MaxCases = 1000
const MaxChecksPerCase = 100

type Partition string

const (
	Improvement Partition = "improvement"
	Regression  Partition = "regression"
	HeldOut     Partition = "held_out"
)

type DataCheck struct {
	ID       string                  `json:"id"`
	Critical bool                    `json:"critical"`
	Schema   workflowcontract.Schema `json:"schema"`
}

// CaseSource is a reference, not proof of access or permission to reuse data.
// Services must authorize and resolve it before creating a stored suite version.
type CaseSource struct {
	ExecutionID    string `json:"execution_id"`
	ResultRevision int32  `json:"result_revision,omitempty"`
}

type Case struct {
	ID             string                    `json:"id"`
	Partition      Partition                 `json:"partition"`
	Input          json.RawMessage           `json:"input"`
	OutputContract workflowcontract.Contract `json:"output_contract"`
	Checks         []DataCheck               `json:"checks"`
	Source         *CaseSource               `json:"source,omitempty"`
}

type SuiteSpec struct {
	SchemaVersion    string `json:"schema_version"`
	ValidatorVersion string `json:"validator_version"`
	Name             string `json:"name"`
	Cases            []Case `json:"cases"`
}

// Suite owns a deep-frozen encoding. It cannot accidentally serialize cases or
// private answers as part of an agent payload. Every projection returns a copy.
type Suite struct {
	encoded []byte
	hash    string
}

func Parse(raw []byte) (Suite, error) {
	if len(raw) == 0 || len(raw) > MaxSuiteBytes {
		return Suite{}, fmt.Errorf("suite must be between 1 byte and 8 MiB")
	}
	if err := workflowcontract.ValidateArtifactContent("application/json", raw); err != nil {
		return Suite{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var spec SuiteSpec
	if err := decoder.Decode(&spec); err != nil {
		return Suite{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Suite{}, fmt.Errorf("expected one suite object")
	}
	return New(spec)
}

func New(spec SuiteSpec) (Suite, error) {
	// Encoding first isolates caller-owned maps, slices and RawMessage values.
	raw, err := json.Marshal(spec)
	if err != nil {
		return Suite{}, err
	}
	if len(raw) > MaxSuiteBytes {
		return Suite{}, fmt.Errorf("suite exceeds 8 MiB")
	}
	if err = workflowcontract.ValidateArtifactContent("application/json", raw); err != nil {
		return Suite{}, err
	}
	var owned SuiteSpec
	if err = json.Unmarshal(raw, &owned); err != nil {
		return Suite{}, err
	}
	if owned.SchemaVersion != SchemaVersion || owned.ValidatorVersion != ValidatorVersion {
		return Suite{}, fmt.Errorf("unsupported suite or validator version")
	}
	if strings.TrimSpace(owned.Name) == "" || len(owned.Name) > 256 || len(owned.Cases) == 0 || len(owned.Cases) > MaxCases {
		return Suite{}, fmt.Errorf("suite requires a name and 1–1000 cases")
	}
	seen := map[string]bool{}
	for i := range owned.Cases {
		c := &owned.Cases[i]
		if !validID(c.ID) || seen[c.ID] {
			return Suite{}, fmt.Errorf("invalid or duplicate case ID %q", c.ID)
		}
		seen[c.ID] = true
		switch c.Partition {
		case Improvement, Regression, HeldOut:
		default:
			return Suite{}, fmt.Errorf("case %s has unsupported partition", c.ID)
		}
		if len(c.Input) > 1<<20 {
			return Suite{}, fmt.Errorf("case %s input exceeds 1 MiB", c.ID)
		}
		c.Input, err = canonicalInput(c.Input)
		if err != nil {
			return Suite{}, fmt.Errorf("case %s input: %w", c.ID, err)
		}
		if err = c.OutputContract.Check(); err != nil {
			return Suite{}, fmt.Errorf("case %s output contract: %w", c.ID, err)
		}
		if len(c.Checks) > MaxChecksPerCase {
			return Suite{}, fmt.Errorf("case %s exceeds 100 checks", c.ID)
		}
		checks := map[string]bool{"output_contract": true}
		for _, check := range c.Checks {
			if !validID(check.ID) || checks[check.ID] {
				return Suite{}, fmt.Errorf("case %s has invalid or duplicate check ID", c.ID)
			}
			checks[check.ID] = true
			contract := workflowcontract.Contract{SchemaVersion: workflowcontract.Version, Data: check.Schema}
			if err = contract.Check(); err != nil {
				return Suite{}, fmt.Errorf("case %s check %s: %w", c.ID, check.ID, err)
			}
		}
		if c.Source != nil && (!canonicalUUID(c.Source.ExecutionID) || c.Source.ResultRevision < 0) {
			return Suite{}, fmt.Errorf("case %s has invalid source reference", c.ID)
		}
	}
	encoded, err := json.Marshal(owned)
	if err != nil {
		return Suite{}, err
	}
	sum := sha256.Sum256(encoded)
	return Suite{encoded: encoded, hash: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func validID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func canonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func canonicalInput(raw []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	return json.Marshal(value)
}

func (s Suite) Hash() string { return s.hash }

// StorageJSON includes private answers. Only trusted persistence paths may use
// it; do not expose this encoding to improvers or skill bundle builders.
func (s Suite) StorageJSON() []byte { return append([]byte(nil), s.encoded...) }

func (s Suite) spec() (SuiteSpec, error) {
	var spec SuiteSpec
	if len(s.encoded) == 0 {
		return spec, fmt.Errorf("uninitialized suite")
	}
	err := json.Unmarshal(s.encoded, &spec)
	return spec, err
}

// ImprovementCases intentionally omits both regression and held-out partitions.
// The improver may inspect selected improvement answers, not evaluation guards.
func (s Suite) ImprovementCases() ([]Case, error) {
	spec, err := s.spec()
	if err != nil {
		return nil, err
	}
	result := []Case{}
	for _, c := range spec.Cases {
		if c.Partition == Improvement {
			result = append(result, c)
		}
	}
	return result, nil
}

type TrialInput struct {
	CaseID string          `json:"case_id"`
	Input  json.RawMessage `json:"input"`
}

// Trial is for the trusted evaluation runner. The executing agent receives only
// the input, never partition labels, scoring schemas, expected answers or source
// metadata. Authorization must prevent an improver from requesting held-out trials.
func (s Suite) Trial(caseID string) (TrialInput, error) {
	spec, err := s.spec()
	if err != nil {
		return TrialInput{}, err
	}
	for _, c := range spec.Cases {
		if c.ID == caseID {
			return TrialInput{CaseID: c.ID, Input: c.Input}, nil
		}
	}
	return TrialInput{}, fmt.Errorf("evaluation case not found")
}
