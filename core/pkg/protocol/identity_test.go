package protocol

import (
	"encoding/json"
	"testing"
)

func TestRunPayloadKeepsTaskAndRunIdentityDistinct(t *testing.T) {
	for _, payload := range []any{
		RunDispatchPayload{RunID: "execution-id", TaskID: "business-id"},
		AgentRunMessagePayload{RunID: "execution-id", TaskID: "business-id", Seq: 1},
	} {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["run_id"] != "execution-id" || decoded["task_id"] != "business-id" {
			t.Fatalf("identity changed: %s", raw)
		}
		if _, ok := decoded["issue_id"]; ok {
			t.Fatalf("old identity leaked: %s", raw)
		}
	}
}
