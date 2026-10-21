package skilleval

import (
	"encoding/json"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func TestScoreDistinguishesCriticalAndGeneralRegressions(t *testing.T) {
	suite, err := New(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, data    string
		all, critical bool
	}{
		{"pass", `{"answer":"yes","verified":true}`, true, true},
		{"general regression", `{"answer":"no","verified":true}`, false, true},
		{"critical regression", `{"answer":"yes","verified":false}`, false, false},
		{"missing field", `{"answer":"yes"}`, false, false},
		{"invalid JSON", `{`, false, false},
		{"duplicate output key", `{"answer":"yes","verified":true,"verified":false}`, false, false},
		{"invalid UTF-8", "{\"answer\":\"\xff\",\"verified\":true}", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := suite.Score("improvement", Output{Data: json.RawMessage(tc.data)})
			if err != nil {
				t.Fatal(err)
			}
			if result.AllPassed != tc.all || result.CriticalPassed != tc.critical || result.SuiteHash != suite.Hash() || result.ValidatorVersion != ValidatorVersion || len(result.Checks) != 3 {
				t.Fatalf("incorrect result: %+v", result)
			}
		})
	}
	if _, err = suite.Score("missing", Output{}); err == nil {
		t.Fatal("unknown case scored")
	}
	if _, err = (Suite{}).Score("improvement", Output{}); err == nil {
		t.Fatal("uninitialized suite scored")
	}
}

func TestScoreRequiresDeclaredArtifactManifests(t *testing.T) {
	spec := testSpec()
	spec.Cases[0].OutputContract.Artifacts = []workflowcontract.ArtifactRequirement{{Name: "report", Kind: "report", MediaType: "text/markdown", SchemaVersion: "report/v1", Required: true}}
	suite, err := New(spec)
	if err != nil {
		t.Fatal(err)
	}
	output := Output{Data: json.RawMessage(`{"answer":"yes","verified":true}`)}
	missing, err := suite.Score("improvement", output)
	if err != nil || missing.CriticalPassed {
		t.Fatalf("missing required artifact passed: %+v %v", missing, err)
	}
	output.Artifacts = []workflowcontract.Artifact{{Name: "report", Kind: "report", MediaType: "text/markdown", SchemaVersion: "report/v1", AttachmentID: "11111111-1111-4111-8111-111111111111", Checksum: workflowcontract.Checksum([]byte("report"))}}
	manifest, err := suite.Score("improvement", output)
	if err != nil || !manifest.AllPassed {
		t.Fatalf("valid manifest rejected: %+v %v", manifest, err)
	}
	// Passing manifests do not imply that the runner fetched or checked any bytes.
	output.Artifacts[0].MediaType = "application/json"
	wrong, err := suite.Score("improvement", output)
	if err != nil || wrong.CriticalPassed {
		t.Fatalf("wrong artifact type passed: %+v %v", wrong, err)
	}
}
