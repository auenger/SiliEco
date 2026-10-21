package handler

import (
	"encoding/json"
	"testing"
)

func TestWorkflowInputBindingsConfiguration(t *testing.T) {
	contract := json.RawMessage(`{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","properties":{"limit":{"type":"number"}}}}}`)
	first := WorkflowStageInput{Name: "First", StableKey: "first"}
	second := WorkflowStageInput{Name: "Second", StableKey: "second", InputSpec: contract}
	for _, tc := range []struct {
		source string
		valid  bool
	}{
		{`{"kind":"run_input","pointer":"/limit"}`, true},
		{`{"kind":"stage_result","stage_key":"first","pointer":"/limit"}`, true},
		{`{"kind":"stage_result","stage_key":"second","pointer":"/limit"}`, false},
		{`{"kind":"stage_result","stage_key":"future","pointer":"/limit"}`, false},
		{`{"kind":"latest","pointer":"/limit"}`, false},
	} {
		second.InputBindings = json.RawMessage(`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"limit","source":` + tc.source + `}]}`)
		_, err := normalizeWorkflowStages([]WorkflowStageInput{first, second})
		if (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.source, err)
		}
	}
	second.InputBindings = json.RawMessage(`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"limit","source":{"kind":"run_input","pointer":"/limit"}}]}`)
	second.InputSpec = nil
	if _, err := normalizeWorkflowStages([]WorkflowStageInput{first, second}); err == nil {
		t.Fatal("bindings without contract accepted")
	}
}
