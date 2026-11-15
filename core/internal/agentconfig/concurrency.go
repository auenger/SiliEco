package agentconfig

import "fmt"

const (
	DefaultMaxConcurrentRuns int32 = 6
	MinMaxConcurrentRuns     int32 = 1
	MaxMaxConcurrentRuns     int32 = 50
)

func IsValidMaxConcurrentRuns(value int32) bool {
	return value >= MinMaxConcurrentRuns && value <= MaxMaxConcurrentRuns
}

func ValidateMaxConcurrentRuns(value int32) error {
	if !IsValidMaxConcurrentRuns(value) {
		return fmt.Errorf(
			"must be between %d and %d",
			MinMaxConcurrentRuns,
			MaxMaxConcurrentRuns,
		)
	}
	return nil
}
