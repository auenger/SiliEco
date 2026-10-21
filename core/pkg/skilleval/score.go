package skilleval

import (
	"encoding/json"
	"fmt"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

type Output struct {
	Data      json.RawMessage             `json:"data"`
	Artifacts []workflowcontract.Artifact `json:"artifacts"`
}

type CheckResult struct {
	ID         string                       `json:"id"`
	Critical   bool                         `json:"critical"`
	Passed     bool                         `json:"passed"`
	Violations []workflowcontract.Violation `json:"violations"`
}

type CaseResult struct {
	SuiteHash        string        `json:"suite_hash"`
	ValidatorVersion string        `json:"validator_version"`
	CaseID           string        `json:"case_id"`
	Checks           []CheckResult `json:"checks"`
	AllPassed        bool          `json:"all_passed"`
	CriticalPassed   bool          `json:"critical_passed"`
}

// Score applies the frozen structural contract and named deterministic data
// checks. Artifact bytes/provenance and domain truth beyond these checks are not
// verified here. No aggregate result grants publication authority. Case reports
// can reveal private rules and must not be returned to improvers for held-out cases.
func (s Suite) Score(caseID string, output Output) (CaseResult, error) {
	spec, err := s.spec()
	if err != nil {
		return CaseResult{}, err
	}
	var selected *Case
	for i := range spec.Cases {
		if spec.Cases[i].ID == caseID {
			selected = &spec.Cases[i]
			break
		}
	}
	if selected == nil {
		return CaseResult{}, fmt.Errorf("evaluation case not found")
	}
	result := CaseResult{SuiteHash: s.hash, ValidatorVersion: spec.ValidatorVersion, CaseID: caseID, Checks: []CheckResult{}, AllPassed: true, CriticalPassed: true}
	add := func(id string, critical bool, violations []workflowcontract.Violation) {
		if violations == nil {
			violations = []workflowcontract.Violation{}
		}
		passed := len(violations) == 0
		result.Checks = append(result.Checks, CheckResult{ID: id, Critical: critical, Passed: passed, Violations: violations})
		if !passed {
			result.AllPassed = false
			if critical {
				result.CriticalPassed = false
			}
		}
	}
	if err := workflowcontract.ValidateArtifactContent("application/json", output.Data); err != nil {
		violations := []workflowcontract.Violation{{Path: "/data", Code: "invalid_json", Message: err.Error()}}
		add("output_contract", true, violations)
		for _, check := range selected.Checks {
			add(check.ID, check.Critical, violations)
		}
		return result, nil
	}
	violations, err := selected.OutputContract.ValidateData(output.Data)
	if err != nil {
		return CaseResult{}, err
	}
	artifacts, err := selected.OutputContract.ValidateArtifacts(output.Artifacts)
	if err != nil {
		return CaseResult{}, err
	}
	add("output_contract", true, append(violations, artifacts...))
	for _, check := range selected.Checks {
		contract := workflowcontract.Contract{SchemaVersion: workflowcontract.Version, Data: check.Schema}
		violations, err := contract.ValidateData(output.Data)
		if err != nil {
			return CaseResult{}, err
		}
		add(check.ID, check.Critical, violations)
	}
	return result, nil
}
