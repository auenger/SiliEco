package workflowcontract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRetryPolicyValidation(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"max_attempts":1}`, `{"max_attempts":10,"timeout_seconds":86400,"backoff_seconds":5,"backoff_multiplier":2,"max_backoff_seconds":60}`} {
		if _, err := ParseRetryPolicy([]byte(raw)); err != nil {
			t.Errorf("valid %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"max_attempts":0}`, `{"max_attempts":11}`, `{"max_attempts":1.5}`, `{"max_attempts":"2"}`, `{"timeout_seconds":0}`, `{"timeout_seconds":86401}`, `{"backoff_seconds":-1}`, `{"backoff_seconds":3601}`, `{"backoff_multiplier":2}`, `{"backoff_seconds":10,"max_backoff_seconds":5}`, `{"unknown":1}`, `{} {}`} {
		if _, err := ParseRetryPolicy([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestRetryPolicyExplicitLimitOverridesProviderDefault(t *testing.T) {
	policy, err := ParseRetryPolicy([]byte(`{"max_attempts":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := policy.AttemptCeiling(3); got != 2 {
		t.Fatalf("explicit limit widened: %d", got)
	}
	if got := (RetryPolicy{}).AttemptCeiling(3); got != 3 {
		t.Fatalf("default changed: %d", got)
	}
}

func TestRetryPolicyBackoffIsBounded(t *testing.T) {
	policy, err := ParseRetryPolicy([]byte(`{"backoff_seconds":5,"backoff_multiplier":2,"max_backoff_seconds":30}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		attempt int32
		want    time.Duration
	}{{1, 5 * time.Second}, {2, 10 * time.Second}, {3, 20 * time.Second}, {4, 30 * time.Second}, {2147483647, 30 * time.Second}} {
		if got := policy.RetryDelay(tc.attempt, time.Second); got != tc.want {
			t.Errorf("attempt %d: %v != %v", tc.attempt, got, tc.want)
		}
	}
	zero, err := ParseRetryPolicy([]byte(`{"backoff_seconds":0,"backoff_multiplier":10}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := zero.RetryDelay(2147483647, time.Second); got != 0 {
		t.Fatalf("zero delay: %v", got)
	}
	if got := (RetryPolicy{}).RetryDelay(2, 5*time.Second); got != 5*time.Second {
		t.Fatalf("default changed: %v", got)
	}
}

func TestRetryPolicyRoundTripPreservesExplicitImmediateRetry(t *testing.T) {
	policy, err := ParseRetryPolicy([]byte(`{"max_attempts":1,"backoff_seconds":0}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"max_attempts":1,"backoff_seconds":0}` {
		t.Fatalf("lost explicit policy: %s", raw)
	}
}
