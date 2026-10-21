package skilleval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func testSchema(answer string, verified bool) workflowcontract.Schema {
	properties := map[string]workflowcontract.Schema{"answer": {Type: "string"}, "verified": {Type: "boolean"}}
	if answer != "" {
		encoded, _ := json.Marshal(answer)
		properties["answer"] = workflowcontract.Schema{Type: "string", Enum: []json.RawMessage{encoded}}
	}
	if verified {
		properties["verified"] = workflowcontract.Schema{Type: "boolean", Enum: []json.RawMessage{json.RawMessage(`true`)}}
	}
	return workflowcontract.Schema{Type: "object", Properties: properties, Required: []string{"answer", "verified"}}
}

func testSpec() SuiteSpec {
	cases := []Case{}
	for _, partition := range []Partition{Improvement, Regression, HeldOut} {
		input, _ := json.Marshal(map[string]string{"prompt": string(partition) + "-input"})
		cases = append(cases, Case{ID: string(partition), Partition: partition, Input: input, OutputContract: workflowcontract.Contract{SchemaVersion: workflowcontract.Version, Data: testSchema("", false)}, Checks: []DataCheck{{ID: "verification", Critical: true, Schema: testSchema("", true)}, {ID: "answer", Critical: false, Schema: testSchema("yes", false)}}})
	}
	return SuiteSpec{SchemaVersion: SchemaVersion, ValidatorVersion: ValidatorVersion, Name: "Report checks", Cases: cases}
}

func TestSuiteFreezesContentAndBindsHashToChecks(t *testing.T) {
	spec := testSpec()
	suite, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	original := suite.StorageJSON()
	roundtrip, err := Parse(original)
	if err != nil || roundtrip.Hash() != suite.Hash() {
		t.Fatalf("roundtrip changed version: %v", err)
	}
	spec.Cases[0].Input[0] = 'x'
	spec.Cases[0].OutputContract.Data.Properties["answer"] = workflowcontract.Schema{Type: "integer"}
	storage := suite.StorageJSON()
	storage[0] = 'x'
	improvement, err := suite.ImprovementCases()
	if err != nil {
		t.Fatal(err)
	}
	improvement[0].Checks[0].ID = "changed"
	improvement[0].Input[0] = 'x'
	if !bytes.Equal(original, suite.StorageJSON()) {
		t.Fatal("caller mutated frozen suite")
	}
	for name, mutate := range map[string]func(*SuiteSpec){
		"input":           func(s *SuiteSpec) { s.Cases[0].Input = json.RawMessage(`{"prompt":"different"}`) },
		"partition":       func(s *SuiteSpec) { s.Cases[0].Partition = HeldOut },
		"criticality":     func(s *SuiteSpec) { s.Cases[0].Checks[1].Critical = true },
		"expected answer": func(s *SuiteSpec) { s.Cases[0].Checks[1].Schema = testSchema("different", false) },
		"source": func(s *SuiteSpec) {
			s.Cases[0].Source = &CaseSource{ExecutionID: "11111111-1111-4111-8111-111111111111", ResultRevision: 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := testSpec()
			mutate(&changed)
			version, err := New(changed)
			if err != nil {
				t.Fatal(err)
			}
			if version.Hash() == suite.Hash() {
				t.Fatal("changed evaluation material retained version")
			}
		})
	}
}

func TestSuiteProjectionsDoNotDiscloseHeldOutAnswers(t *testing.T) {
	spec := testSpec()
	spec.Cases[2].Checks[1].Schema = testSchema("private-held-out-answer", false)
	suite, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	improvement, err := suite.ImprovementCases()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(improvement)
	if len(improvement) != 1 || improvement[0].Partition != Improvement || strings.Contains(string(encoded), "held_out-input") || strings.Contains(string(encoded), "private-held-out-answer") || strings.Contains(string(encoded), "regression-input") {
		t.Fatalf("private partitions leaked: %s", encoded)
	}
	trial, err := suite.Trial("held_out")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(trial)
	if !strings.Contains(string(encoded), "held_out-input") || strings.Contains(string(encoded), "private-held-out-answer") || strings.Contains(string(encoded), "checks") || strings.Contains(string(encoded), "output_contract") {
		t.Fatalf("trial carried grading material: %s", encoded)
	}
	trial.Input[0] = 'x'
	again, err := suite.Trial("held_out")
	if err != nil || !json.Valid(again.Input) {
		t.Fatal("trial mutation altered suite")
	}
	encoded, _ = json.Marshal(suite)
	if string(encoded) != "{}" {
		t.Fatalf("suite accidentally serialized private cases: %s", encoded)
	}
}

func TestSuiteRejectsAmbiguousOrUnsupportedDefinitions(t *testing.T) {
	for name, mutate := range map[string]func(*SuiteSpec){
		"validator":           func(s *SuiteSpec) { s.ValidatorVersion = "unversioned" },
		"schema":              func(s *SuiteSpec) { s.SchemaVersion = "future" },
		"empty":               func(s *SuiteSpec) { s.Cases = nil },
		"duplicate case":      func(s *SuiteSpec) { s.Cases[1].ID = s.Cases[0].ID },
		"unsafe ID":           func(s *SuiteSpec) { s.Cases[0].ID = "../path" },
		"partition":           func(s *SuiteSpec) { s.Cases[0].Partition = "train" },
		"null input":          func(s *SuiteSpec) { s.Cases[0].Input = json.RawMessage(`null`) },
		"duplicate input key": func(s *SuiteSpec) { s.Cases[0].Input = json.RawMessage(`{"x":1,"x":2}`) },
		"duplicate check":     func(s *SuiteSpec) { s.Cases[0].Checks[1].ID = s.Cases[0].Checks[0].ID },
		"reserved check":      func(s *SuiteSpec) { s.Cases[0].Checks[0].ID = "output_contract" },
		"source":              func(s *SuiteSpec) { s.Cases[0].Source = &CaseSource{ExecutionID: "not-a-uuid"} },
		"unknown check type":  func(s *SuiteSpec) { s.Cases[0].Checks[0].Schema.Type = "execute_shell" },
		"oversized input": func(s *SuiteSpec) {
			s.Cases[0].Input = json.RawMessage(`{"input":"` + strings.Repeat("a", 1<<20) + `"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := testSpec()
			mutate(&spec)
			if _, err := New(spec); err == nil {
				t.Fatal("invalid suite accepted")
			}
		})
	}
	raw, _ := json.Marshal(testSpec())
	for _, invalid := range [][]byte{append(raw, []byte(` {}`)...), bytes.Replace(raw, []byte(`"name":`), []byte(`"unknown":true,"name":`), 1), bytes.Replace(raw, []byte(`"name":`), []byte(`"name":"first","name":`), 1)} {
		if _, err := Parse(invalid); err == nil {
			t.Fatal("ambiguous wire suite accepted")
		}
	}
	if _, err := (Suite{}).Trial("case"); err == nil {
		t.Fatal("uninitialized suite accepted")
	}
}

func TestSuiteCanonicalizesInputWithoutRoundingNumbers(t *testing.T) {
	spec := testSpec()
	spec.Cases[0].Input = json.RawMessage(`{"b":9007199254740993,"a":1}`)
	first, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Cases[0].Input = json.RawMessage(`{ "a": 1, "b": 9007199254740993 }`)
	second, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() != second.Hash() {
		t.Fatal("object layout changed input identity")
	}
	trial, _ := first.Trial("improvement")
	if !strings.Contains(string(trial.Input), "9007199254740993") {
		t.Fatal("input integer was rounded")
	}
}
