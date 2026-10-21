package workflowcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// RetryPolicy is frozen with each stage execution. Nil fields retain the task
// service defaults; explicit limits must never be widened by provider heuristics.
type RetryPolicy struct {
	MaxAttempts       *int32 `json:"max_attempts,omitempty"`
	TimeoutSeconds    *int32 `json:"timeout_seconds,omitempty"`
	BackoffSeconds    *int32 `json:"backoff_seconds,omitempty"`
	BackoffMultiplier *int32 `json:"backoff_multiplier,omitempty"`
	MaxBackoffSeconds *int32 `json:"max_backoff_seconds,omitempty"`
}

func ParseRetryPolicy(raw []byte) (RetryPolicy, error) {
	var policy RetryPolicy
	if len(raw) == 0 {
		return policy, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return policy, fmt.Errorf("retry_policy must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("invalid retry_policy: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return policy, fmt.Errorf("retry_policy must contain one object")
	}
	for _, field := range []struct {
		name     string
		value    *int32
		min, max int32
	}{
		{"max_attempts", policy.MaxAttempts, 1, 10},
		{"timeout_seconds", policy.TimeoutSeconds, 1, 86400},
		{"backoff_seconds", policy.BackoffSeconds, 0, 3600},
		{"backoff_multiplier", policy.BackoffMultiplier, 1, 10},
		{"max_backoff_seconds", policy.MaxBackoffSeconds, 0, 3600},
	} {
		if field.value != nil && (*field.value < field.min || *field.value > field.max) {
			return policy, fmt.Errorf("%s must be an integer between %d and %d", field.name, field.min, field.max)
		}
	}
	if policy.BackoffSeconds == nil && (policy.BackoffMultiplier != nil || policy.MaxBackoffSeconds != nil) {
		return policy, fmt.Errorf("backoff_seconds is required for a custom backoff schedule")
	}
	if policy.BackoffSeconds != nil && policy.MaxBackoffSeconds != nil && *policy.MaxBackoffSeconds < *policy.BackoffSeconds {
		return policy, fmt.Errorf("max_backoff_seconds must be at least backoff_seconds")
	}
	return policy, nil
}

// AttemptCeiling applies an explicit total-call budget, including the first call.
func (p RetryPolicy) AttemptCeiling(defaultCeiling int32) int32 {
	if p.MaxAttempts != nil {
		return *p.MaxAttempts
	}
	return defaultCeiling
}

// RetryDelay returns the delay before retrying a failed 1-based attempt. Custom
// schedules are bounded at one hour even when a stored attempt counter is large.
func (p RetryPolicy) RetryDelay(failedAttempt int32, defaultDelay time.Duration) time.Duration {
	if p.BackoffSeconds == nil {
		return defaultDelay
	}
	delay := int64(*p.BackoffSeconds)
	capSeconds := int64(3600)
	if p.MaxBackoffSeconds != nil {
		capSeconds = int64(*p.MaxBackoffSeconds)
	}
	multiplier := int64(1)
	if p.BackoffMultiplier != nil {
		multiplier = int64(*p.BackoffMultiplier)
	}
	if delay == 0 || multiplier == 1 {
		return time.Duration(delay) * time.Second
	}
	for attempt := int32(1); attempt < failedAttempt && delay < capSeconds; attempt++ {
		delay *= multiplier
		if delay > capSeconds {
			delay = capSeconds
		}
	}
	return time.Duration(delay) * time.Second
}
