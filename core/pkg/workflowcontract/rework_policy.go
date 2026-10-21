package workflowcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ReworkPolicy limits explicit business rework initiated from one stage in a Run.
// An absent limit retains the existing member-authorized, unlimited behavior.
type ReworkPolicy struct {
	MaxReworks *int32 `json:"max_reworks,omitempty"`
}

func ParseReworkPolicy(raw []byte) (ReworkPolicy, error) {
	var policy ReworkPolicy
	if len(raw) == 0 {
		return policy, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return policy, fmt.Errorf("rework_policy must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("invalid rework_policy: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return policy, fmt.Errorf("rework_policy must contain one object")
	}
	if policy.MaxReworks != nil && (*policy.MaxReworks < 0 || *policy.MaxReworks > 100) {
		return policy, fmt.Errorf("max_reworks must be an integer between 0 and 100")
	}
	return policy, nil
}
