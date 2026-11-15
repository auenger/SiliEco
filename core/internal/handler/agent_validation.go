package handler

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/silieco-ai/silieco/core/internal/agentconfig"
)

func validateAgentMaxConcurrentRuns(value int32) error {
	if err := agentconfig.ValidateMaxConcurrentRuns(value); err != nil {
		return fmt.Errorf("max_concurrent_runs %w", err)
	}
	return nil
}

func defaultAndValidateAgentMaxConcurrentRuns(rawFields map[string]json.RawMessage, value *int32) error {
	raw, provided := rawFields["max_concurrent_runs"]
	if !provided || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*value = agentconfig.DefaultMaxConcurrentRuns
		return nil
	}
	return validateAgentMaxConcurrentRuns(*value)
}
