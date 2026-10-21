package workflowcontract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const exampleContract = `{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["confirmed","temperature","sources"],"properties":{"confirmed":{"type":"boolean","enum":[true]},"temperature":{"type":"number","minimum":0,"maximum":100},"sources":{"type":"array","min_items":1,"items":{"type":"object","required":["page"],"properties":{"page":{"type":"integer","minimum":1}}}}}}}`

func TestContractData(t *testing.T) {
	contract, err := Parse([]byte(exampleContract))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, data, path, code string }{
		{"valid", `{"confirmed":true,"temperature":25,"sources":[{"page":1}]}`, "", ""},
		{"unconfirmed", `{"confirmed":false,"temperature":25,"sources":[{"page":1}]}`, "/data/confirmed", "enum"},
		{"missing", `{"confirmed":true,"sources":[{"page":1}]}`, "/data/temperature", "required"},
		{"null", `{"confirmed":true,"temperature":null,"sources":[{"page":1}]}`, "/data/temperature", "type"},
		{"coercion", `{"confirmed":true,"temperature":"25","sources":[{"page":1}]}`, "/data/temperature", "type"},
		{"range", `{"confirmed":true,"temperature":101,"sources":[{"page":1}]}`, "/data/temperature", "range"},
		{"no_sources", `{"confirmed":true,"temperature":25,"sources":[]}`, "/data/sources", "item_count"},
		{"fractional_page", `{"confirmed":true,"temperature":25,"sources":[{"page":1.1}]}`, "/data/sources/0/page", "type"},
		{"integer_exponent", `{"confirmed":true,"temperature":25,"sources":[{"page":1e1}]}`, "", ""},
		{"unknown", `{"confirmed":true,"temperature":25,"sources":[{"page":1}],"approved":true}`, "/data/approved", "unknown_field"},
		{"duplicate", `{"confirmed":false,"confirmed":true,"temperature":25,"sources":[{"page":1}]}`, "/data", "invalid_json"},
		{"huge_exponent", `{"confirmed":true,"temperature":1e999999999,"sources":[{"page":1}]}`, "/data/temperature", "number_range"},
		{"multiple_values", `{} {}`, "/data", "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, err := contract.ValidateData([]byte(tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if tt.code == "" {
				if len(violations) != 0 {
					t.Fatalf("unexpected violations: %+v", violations)
				}
				return
			}
			for _, v := range violations {
				if v.Path == tt.path && v.Code == tt.code {
					return
				}
			}
			t.Fatalf("missing %s at %s: %+v", tt.code, tt.path, violations)
		})
	}
}

func TestRejectUnsupportedContracts(t *testing.T) {
	for _, data := range []string{
		`null`, `[]`, `{}`, strings.Replace(exampleContract, "stage/v1", "stage/v2", 1),
		strings.Replace(exampleContract, `"minimum":0`, `"pattern":".*"`, 1),
		strings.Replace(exampleContract, `"minimum":0`, `"minimum":101`, 1),
		strings.Replace(exampleContract, `"minimum":0`, `"minimum":1e999999999`, 1),
		strings.Replace(exampleContract, `"min_items":1`, `"min_items":-1`, 1),
		strings.Replace(exampleContract, `"enum":[true]`, `"enum":[]`, 1),
		strings.Replace(exampleContract, `"enum":[true]`, `"enum":["true"]`, 1),
		strings.Replace(exampleContract, `"type":"array"`, `"type":"string"`, 1),
		strings.Replace(exampleContract, `"required":["page"]`, `"required":["page","page"]`, 1),
		strings.Replace(exampleContract, `"required":["page"]`, `"required":["missing"]`, 1),
		strings.Replace(exampleContract, `"type":"boolean"`, `"type":"boolean","type":"boolean"`, 1),
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("accepted invalid contract: %s", data)
		}
	}
}

func TestExactNumbersAndPointerPaths(t *testing.T) {
	c, err := Parse([]byte(`{"schema_version":"silieco.stage/v1","data":{"type":"object","properties":{"a/b~c":{"type":"integer","enum":[9007199254740993]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	v, err := c.ValidateData([]byte(`{"a/b~c":9007199254740992}`))
	if err != nil || len(v) != 1 || v[0].Path != "/data/a~1b~0c" || v[0].Code != "enum" {
		t.Fatalf("rounded integer or invalid path: %+v, %v", v, err)
	}
	v, err = c.ValidateData([]byte(`{"a/b~c":9007199254740993.0}`))
	if err != nil || len(v) != 0 {
		t.Fatalf("equivalent number rejected: %+v, %v", v, err)
	}
}

func TestArtifactManifest(t *testing.T) {
	c := Contract{SchemaVersion: Version, Data: Schema{Type: "object"}, Artifacts: []ArtifactRequirement{{Name: "report", Kind: "report", MediaType: "text/markdown", SchemaVersion: "thermal.report/v1", Required: true}}}
	valid := Artifact{Name: "report", Kind: "report", MediaType: "text/markdown", SchemaVersion: "thermal.report/v1", AttachmentID: "12345678-1234-1234-1234-123456789abc", Checksum: Checksum([]byte("report"))}
	v, err := c.ValidateArtifacts([]Artifact{valid})
	if err != nil || len(v) != 0 {
		t.Fatalf("valid manifest: %+v, %v", v, err)
	}
	tests := []struct {
		name      string
		artifacts []Artifact
		code      string
	}{
		{"missing", nil, "required_artifact"},
		{"duplicate", []Artifact{valid, valid}, "duplicate_artifact"},
	}
	for _, field := range []string{"name", "kind", "media_type", "schema_version", "attachment_id", "checksum"} {
		raw, _ := json.Marshal(valid)
		var object map[string]any
		_ = json.Unmarshal(raw, &object)
		object[field] = "invalid"
		raw, _ = json.Marshal(object)
		var changed Artifact
		_ = json.Unmarshal(raw, &changed)
		code := field
		if field == "name" {
			code = "unknown_artifact"
		}
		tests = append(tests, struct {
			name      string
			artifacts []Artifact
			code      string
		}{field, []Artifact{changed}, code})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := c.ValidateArtifacts(tt.artifacts)
			if err != nil {
				t.Fatal(err)
			}
			for _, violation := range v {
				if violation.Code == tt.code {
					return
				}
			}
			t.Fatalf("missing %s: %+v", tt.code, v)
		})
	}
}

func TestThermalFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/thermal-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"normal", "failed", "reworked"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/thermal-" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			v, err := c.ValidateData(data)
			if err != nil {
				t.Fatal(err)
			}
			if (len(v) > 0) != (name == "failed") {
				t.Fatalf("unexpected fixture validation: %+v", v)
			}
		})
	}
}

func FuzzParseContract(f *testing.F) {
	f.Add(exampleContract)
	f.Add(`null`)
	f.Fuzz(func(t *testing.T, input string) {
		contract, err := Parse([]byte(input))
		if err == nil {
			_, _ = contract.ValidateData([]byte(input))
			_, _ = contract.ValidateArtifacts(nil)
		}
	})
}

func TestSubmissionCannotBypassRequiredArtifacts(t *testing.T) {
	raw, err := os.ReadFile("testdata/thermal-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/thermal-normal.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(Submission{Data: data, Artifacts: []Artifact{}})
	if err != nil {
		t.Fatal(err)
	}
	violations, err := contract.ValidateSubmission(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 {
		t.Fatalf("data success bypassed artifact barrier: %+v", violations)
	}
	for _, v := range violations {
		if v.Code != "required_artifact" {
			t.Fatalf("unexpected violation: %+v", v)
		}
	}
}

func TestContractResourceLimits(t *testing.T) {
	schema := `{"type":"string"}`
	for range 34 {
		schema = `{"type":"array","items":` + schema + `}`
	}
	if _, err := Parse([]byte(`{"schema_version":"silieco.stage/v1","data":` + schema + `}`)); err == nil {
		t.Fatal("accepted excessive schema nesting")
	}
	if _, err := Parse([]byte(strings.Repeat(" ", 1<<20) + exampleContract)); err == nil {
		t.Fatal("accepted oversized contract")
	}
	contract, err := Parse([]byte(exampleContract))
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)
	violations, err := contract.ValidateData([]byte(payload))
	if err != nil || len(violations) != 1 || violations[0].Code != "invalid_json" {
		t.Fatalf("accepted deeply nested payload: %+v %v", violations, err)
	}
}
