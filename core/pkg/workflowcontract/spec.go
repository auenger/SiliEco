package workflowcontract

import (
	"encoding/json"
	"fmt"
)

// FromSpec extracts the executable contract from a stage's input_spec or
// output_spec. Descriptive fields remain alongside it for the shared designer.
func FromSpec(raw []byte, required bool) (*Contract, error) {
	var spec map[string]json.RawMessage
	if len(raw) > 0 {
		if err := strictDecode(raw, &spec); err != nil {
			return nil, err
		}
		if spec == nil {
			return nil, fmt.Errorf("stage spec must be an object")
		}
	}
	contractRaw, exists := spec["contract"]
	if !exists {
		if required {
			return nil, fmt.Errorf("agent stage output_spec.contract is required")
		}
		return nil, nil
	}
	contract, err := Parse(contractRaw)
	if err != nil {
		return nil, err
	}
	return &contract, nil
}
