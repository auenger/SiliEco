package workflowcontract

import "testing"

func TestFromSpec(t *testing.T) {
	for _, test := range []struct {
		name, raw                string
		required, valid, present bool
	}{
		{"descriptive only", `{"default_artifact":"report.md"}`, false, true, false},
		{"missing required", `{}`, true, false, false},
		{"versioned contract", `{"default_artifact":"report.md","contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}`, true, true, true},
		{"null spec", `null`, false, false, false},
		{"null contract", `{"contract":null}`, false, false, false},
		{"unknown version", `{"contract":{"schema_version":"future","data":{"type":"object"}}}`, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract, err := FromSpec([]byte(test.raw), test.required)
			if (err == nil) != test.valid {
				t.Fatalf("unexpected error: %v", err)
			}
			if (contract != nil) != test.present {
				t.Fatalf("unexpected contract: %+v", contract)
			}
		})
	}
}
