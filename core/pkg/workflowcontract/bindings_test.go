package workflowcontract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInputBindingsFreezeSourceRevisions(t *testing.T) {
	bindings, err := ParseInputBindings([]byte(`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"goal","source":{"kind":"run_input","pointer":"/request/a~1b/~0key"}},{"target":"limit","source":{"kind":"stage_result","stage_key":"requirements","pointer":"/limits/0"}},{"target":"confirmed","source":{"kind":"stage_result","stage_key":"requirements","pointer":"/confirmed"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	contract, err := Parse([]byte(`{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal","limit","confirmed"],"properties":{"goal":{"type":"string"},"limit":{"type":"number"},"confirmed":{"type":"boolean"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	run := RunInputSource{InstanceID: "11111111-1111-4111-8111-111111111111", PlanRevision: 2, Data: json.RawMessage(`{"request":{"a/b":{"~key":"cooling"}}}`)}
	calls := 0
	source := AcceptedInputResult{StageKey: "requirements", ExecutionID: "22222222-2222-4222-8222-222222222222", ResultID: "33333333-3333-4333-8333-333333333333", Revision: 4, Data: json.RawMessage(`{"limits":[9007199254740993],"confirmed":true}`)}
	snapshot, err := ResolveInputBindings(bindings, contract, run, func(key string) (AcceptedInputResult, error) {
		calls++
		if key != "requirements" {
			t.Fatalf("wrong key: %s", key)
		}
		copy := source
		copy.Revision += int32(calls - 1)
		return copy, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != InputSnapshotVersion || calls != 1 || !strings.Contains(string(snapshot.Data), "9007199254740993") || snapshot.InstanceID != run.InstanceID || snapshot.PlanRevision != 2 {
		t.Fatalf("binding result: %+v, calls=%d", snapshot, calls)
	}
	for _, evidence := range snapshot.Bindings[1:] {
		if evidence.ResultRevision != 4 || evidence.ResultID != source.ResultID || evidence.ExecutionID != source.ExecutionID || evidence.SourceChecksum == "" {
			t.Fatalf("mixed or missing provenance: %+v", evidence)
		}
	}
	if snapshot.Bindings[1].SourceChecksum != snapshot.Bindings[2].SourceChecksum {
		t.Fatal("same source has different checksums")
	}
	// A later change to the provider does not mutate already materialized input.
	source.Revision = 5
	source.Data[0] = '!'
	if !json.Valid(snapshot.Data) || snapshot.Bindings[1].ResultRevision != 4 {
		t.Fatal("snapshot retained a mutable source")
	}
}

func TestInputBindingsRejectAmbiguousConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":"unknown","data":[]}`,
		`{"schema_version":"silieco.input-bindings/v1"}`,
		`{"schema_version":"silieco.input-bindings/v1","data":null}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"run_input"}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"run_input","pointer":null}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"latest","pointer":""}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"stage_result","pointer":""}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"run_input","stage_key":"other","pointer":""}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"run_input","pointer":"/a~2b"}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"run_input","pointer":""}},{"target":"a","source":{"kind":"run_input","pointer":""}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","target":"b","source":{"kind":"run_input","pointer":""}}]}`,
		`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"a","source":{"kind":"stage_result","stage_key":"s","pointer":"","accepted":true}}]}`,
	} {
		if _, err := ParseInputBindings([]byte(raw)); err == nil {
			t.Errorf("accepted configuration: %s", raw)
		}
	}
}

func TestInputBindingsValidateProjectionAndContract(t *testing.T) {
	contract := Contract{SchemaVersion: Version, Data: Schema{Type: "object", Required: []string{"field"}, Properties: map[string]Schema{"field": {Type: "string"}}}}
	run := RunInputSource{InstanceID: "11111111-1111-4111-8111-111111111111", PlanRevision: 1, Data: json.RawMessage(`{"array":["value"],"number":4,"object":{"01":"object key"}}`)}
	for _, tc := range []struct {
		pointer string
		valid   bool
	}{
		{"/array/0", true}, {"/object/01", true}, {"/array/01", false}, {"/array/-", false}, {"/array/+0", false}, {"/array/1", false}, {"/array/999999999999999999999999", false}, {"/missing", false}, {"/number/x", false}, {"/number", false},
	} {
		b := InputBindings{SchemaVersion: InputBindingsVersion, Data: []InputBinding{{Target: "field", Source: InputSource{Kind: "run_input", Pointer: tc.pointer}}}}
		_, err := ResolveInputBindings(b, contract, run, nil)
		if (err == nil) != tc.valid {
			t.Errorf("%s: %v", tc.pointer, err)
		}
	}
	b := InputBindings{SchemaVersion: InputBindingsVersion, Data: []InputBinding{{Target: "undeclared", Source: InputSource{Kind: "stage_result", StageKey: "s", Pointer: ""}}}}
	called := false
	if _, err := ResolveInputBindings(b, contract, run, func(string) (AcceptedInputResult, error) { called = true; return AcceptedInputResult{}, nil }); err == nil || called {
		t.Fatal("undeclared target read external source")
	}
	b.Data[0].Target = "field"
	rejected := errors.New("source is not accepted")
	if _, err := ResolveInputBindings(b, contract, run, func(string) (AcceptedInputResult, error) { return AcceptedInputResult{}, rejected }); !errors.Is(err, rejected) {
		t.Fatalf("acceptance denial lost: %v", err)
	}
	if _, err := ResolveInputBindings(b, contract, run, func(string) (AcceptedInputResult, error) {
		return AcceptedInputResult{StageKey: "s", ExecutionID: run.InstanceID, ResultID: run.InstanceID, Revision: 0, Data: json.RawMessage(`{}`)}, nil
	}); err == nil {
		t.Fatal("unversioned result accepted")
	}
	run.Data = json.RawMessage(`{"array":[],"array":["value"]}`)
	if _, err := ResolveInputBindings(b, contract, run, nil); err == nil {
		t.Fatal("ambiguous Run inputs accepted")
	}
}

func TestRunInputCorrectionPreservesConsumedFields(t *testing.T) {
	bindings := json.RawMessage(`{"schema_version":"silieco.input-bindings/v1","data":[{"target":"goal","source":{"kind":"run_input","pointer":"/goal"}}]}`)
	snapshot := json.RawMessage(`{"schema_version":"silieco.input-snapshot/v1","instance_id":"run","plan_revision":1,"data":{"goal":"original"},"bindings":[]}`)
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"goal":"original","next":"new"}`, true},
		{`{"goal":"changed","next":"new"}`, false},
		{`{"next":"new"}`, false}, {`[]`, false}, {`{"goal":"original","goal":"changed"}`, false},
	} {
		err := CheckRunInputCorrection(bindings, snapshot, json.RawMessage(tc.input))
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.input, err)
		}
	}
	if err := CheckRunInputCorrection(nil, json.RawMessage(`{"goal":"original"}`), json.RawMessage(`{"goal":"original","next":"new"}`)); err == nil {
		t.Fatal("unbound input was changed")
	}
}
