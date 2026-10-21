package workflowcontract

import "testing"

func TestReworkPolicyValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `{"max_reworks":0}`, `{"max_reworks":100}`} {
		if _, err := ParseReworkPolicy([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"max_reworks":-1}`, `{"max_reworks":101}`, `{"max_reworks":1.5}`, `{"max_reworks":"3"}`, `{"other":1}`, `{} {}`} {
		if _, err := ParseReworkPolicy([]byte(raw)); err == nil {
			t.Errorf("invalid policy accepted: %s", raw)
		}
	}
}
