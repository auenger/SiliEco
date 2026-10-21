package workflowcontract

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const InputBindingsVersion = "silieco.input-bindings/v1"
const InputSnapshotVersion = "silieco.input-snapshot/v1"

// InputBindings maps declared input fields to explicit source locations. Stage
// keys identify dependencies; the server resolves each to one accepted revision.
// Artifact and project-resource bindings require their own versioned references
// and are rejected here until those source resolvers are implemented.
type InputBindings struct {
	SchemaVersion string         `json:"schema_version"`
	Data          []InputBinding `json:"data"`
}

type InputBinding struct {
	Target string      `json:"target"`
	Source InputSource `json:"source"`
}

type InputSource struct {
	Kind     string `json:"kind"`
	StageKey string `json:"stage_key,omitempty"`
	Pointer  string `json:"pointer"`
}

// UnmarshalJSON requires an explicit pointer, including "" for the root.
func (s *InputSource) UnmarshalJSON(raw []byte) error {
	var value struct {
		Kind     string  `json:"kind"`
		StageKey string  `json:"stage_key,omitempty"`
		Pointer  *string `json:"pointer"`
	}
	if err := strictDecode(raw, &value); err != nil {
		return err
	}
	if value.Pointer == nil {
		return fmt.Errorf("input source pointer is required")
	}
	*s = InputSource{Kind: value.Kind, StageKey: value.StageKey, Pointer: *value.Pointer}
	return nil
}

// AcceptedInputResult must be loaded by an authorized server-side resolver that
// holds the relevant Run/dependency locks and validates current acceptance.
// This pure package cannot authorize a database row or prove business approval.
// Never deserialize this type from an agent/client as acceptance evidence.
type AcceptedInputResult struct {
	StageKey    string
	ExecutionID string
	ResultID    string
	Revision    int32
	Data        json.RawMessage
}

type RunInputSource struct {
	InstanceID   string
	PlanRevision int32
	Data         json.RawMessage
}

// BoundInputEvidence records both the requested projection and the immutable
// source identity. Values in Data are already materialized; downstream execution
// must not resolve StageKey again or substitute a newer result revision.
type BoundInputEvidence struct {
	Target         string `json:"target"`
	Kind           string `json:"kind"`
	Pointer        string `json:"pointer"`
	StageKey       string `json:"stage_key,omitempty"`
	ExecutionID    string `json:"execution_id,omitempty"`
	ResultID       string `json:"result_id,omitempty"`
	ResultRevision int32  `json:"result_revision,omitempty"`
	SourceChecksum string `json:"source_checksum"`
}

type BoundInputSnapshot struct {
	SchemaVersion string               `json:"schema_version"`
	InstanceID    string               `json:"instance_id"`
	PlanRevision  int32                `json:"plan_revision"`
	Data          json.RawMessage      `json:"data"`
	Bindings      []BoundInputEvidence `json:"bindings"`
}

func ParseInputBindings(raw []byte) (InputBindings, error) {
	var bindings InputBindings
	if err := strictDecode(raw, &bindings); err != nil {
		return InputBindings{}, err
	}
	if err := bindings.Check(); err != nil {
		return InputBindings{}, err
	}
	return bindings, nil
}

// OptionalInputBindings treats an empty object as no configured projection.
func OptionalInputBindings(raw []byte) (*InputBindings, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if err := strictDecode(raw, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("input_bindings must be an object")
	}
	if len(object) == 0 {
		return nil, nil
	}
	parsed, err := ParseInputBindings(raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (b InputBindings) CheckContract(contract Contract) error {
	if err := b.Check(); err != nil {
		return err
	}
	if err := contract.Check(); err != nil {
		return err
	}
	if len(contract.Artifacts) > 0 {
		return fmt.Errorf("input artifact bindings require a versioned artifact resolver")
	}
	for _, binding := range b.Data {
		if _, ok := contract.Data.Properties[binding.Target]; !ok {
			return fmt.Errorf("input binding target %q is not declared", binding.Target)
		}
	}
	return nil
}

func (b InputBindings) Check() error {
	if b.SchemaVersion != InputBindingsVersion {
		return fmt.Errorf("unsupported input binding schema_version %q", b.SchemaVersion)
	}
	if b.Data == nil || len(b.Data) > 128 {
		return fmt.Errorf("input bindings data must be an array with at most 128 entries")
	}
	seen := map[string]bool{}
	for i, binding := range b.Data {
		if binding.Target == "" || len(binding.Target) > 128 || seen[binding.Target] {
			return fmt.Errorf("/data/%d/target must be a unique nonempty input field", i)
		}
		seen[binding.Target] = true
		switch binding.Source.Kind {
		case "run_input":
			if binding.Source.StageKey != "" {
				return fmt.Errorf("/data/%d: run_input cannot specify stage_key", i)
			}
		case "stage_result":
			if binding.Source.StageKey == "" || len(binding.Source.StageKey) > 128 {
				return fmt.Errorf("/data/%d: stage_result requires stage_key", i)
			}
		default:
			return fmt.Errorf("/data/%d: unsupported input source kind %q", i, binding.Source.Kind)
		}
		if _, err := inputPointerTokens(binding.Source.Pointer); err != nil {
			return fmt.Errorf("/data/%d/source/pointer: %w", i, err)
		}
	}
	return nil
}

// ResolveInputBindings performs no I/O except through loadAccepted. The callback
// must authorize the same workspace/Run, reject future or invalidated stages,
// and return the exact accepted result. It is called once per distinct StageKey,
// so several projections cannot mix revisions even if the provider is mutable.
func ResolveInputBindings(bindings InputBindings, contract Contract, run RunInputSource, loadAccepted func(string) (AcceptedInputResult, error)) (BoundInputSnapshot, error) {
	empty := BoundInputSnapshot{}
	if err := bindings.CheckContract(contract); err != nil {
		return empty, err
	}
	if !canonicalUUID(run.InstanceID) || run.PlanRevision < 1 {
		return empty, fmt.Errorf("Run identity and positive plan revision are required")
	}

	var runData any
	if err := strictDecode(run.Data, &runData); err != nil {
		return empty, fmt.Errorf("Run inputs: %w", err)
	}
	if _, ok := runData.(map[string]any); !ok {
		return empty, fmt.Errorf("Run inputs must be an object")
	}
	canonicalRun, err := json.Marshal(runData)
	if err != nil {
		return empty, err
	}
	runChecksum := Checksum(canonicalRun)
	type cachedSource struct {
		result   AcceptedInputResult
		data     any
		checksum string
	}
	cache := map[string]cachedSource{}
	output := map[string]any{}
	evidence := make([]BoundInputEvidence, 0, len(bindings.Data))
	for _, binding := range bindings.Data {
		source := runData
		entry := BoundInputEvidence{Target: binding.Target, Kind: binding.Source.Kind, Pointer: binding.Source.Pointer, SourceChecksum: runChecksum}
		if binding.Source.Kind == "stage_result" {
			cached, ok := cache[binding.Source.StageKey]
			if !ok {
				if loadAccepted == nil {
					return empty, fmt.Errorf("accepted result resolver is required")
				}
				result, err := loadAccepted(binding.Source.StageKey)
				if err != nil {
					return empty, fmt.Errorf("input stage %q: %w", binding.Source.StageKey, err)
				}
				if result.StageKey != binding.Source.StageKey || !canonicalUUID(result.ExecutionID) || !canonicalUUID(result.ResultID) || result.Revision < 1 {
					return empty, fmt.Errorf("invalid accepted result identity for stage %q", binding.Source.StageKey)
				}
				var data any
				if err := strictDecode(result.Data, &data); err != nil {
					return empty, fmt.Errorf("input stage %q: %w", binding.Source.StageKey, err)
				}
				if _, ok := data.(map[string]any); !ok {
					return empty, fmt.Errorf("accepted result data must be an object")
				}
				canonical, err := json.Marshal(data)
				if err != nil {
					return empty, err
				}
				cached = cachedSource{result: result, data: data, checksum: Checksum(canonical)}
				cache[binding.Source.StageKey] = cached
			}
			source = cached.data
			entry.StageKey = cached.result.StageKey
			entry.ExecutionID = cached.result.ExecutionID
			entry.ResultID = cached.result.ResultID
			entry.ResultRevision = cached.result.Revision
			entry.SourceChecksum = cached.checksum
		}
		value, err := resolveInputPointer(source, binding.Source.Pointer)
		if err != nil {
			return empty, fmt.Errorf("input field %q: %w", binding.Target, err)
		}
		output[binding.Target] = value
		evidence = append(evidence, entry)
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return empty, err
	}
	violations, err := contract.ValidateData(raw)
	if err != nil {
		return empty, err
	}
	if len(violations) > 0 {
		return empty, fmt.Errorf("bound input %s: %s", violations[0].Path, violations[0].Message)
	}
	return BoundInputSnapshot{SchemaVersion: InputSnapshotVersion, InstanceID: run.InstanceID, PlanRevision: run.PlanRevision, Data: raw, Bindings: evidence}, nil
}

func inputPointerTokens(pointer string) ([]string, error) {
	if len(pointer) > 4096 {
		return nil, fmt.Errorf("pointer exceeds 4096 bytes")
	}
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("expected JSON Pointer beginning with /")
	}
	tokens := strings.Split(pointer[1:], "/")
	if len(tokens) > 32 {
		return nil, fmt.Errorf("pointer exceeds 32 levels")
	}
	for i, token := range tokens {
		var decoded strings.Builder
		for j := 0; j < len(token); j++ {
			if token[j] != '~' {
				decoded.WriteByte(token[j])
				continue
			}
			j++
			if j >= len(token) {
				return nil, fmt.Errorf("invalid JSON Pointer escape")
			}
			switch token[j] {
			case '0':
				decoded.WriteByte('~')
			case '1':
				decoded.WriteByte('/')
			default:
				return nil, fmt.Errorf("invalid JSON Pointer escape")
			}
		}
		tokens[i] = decoded.String()
	}
	return tokens, nil
}

func resolveInputPointer(source any, pointer string) (any, error) {
	tokens, err := inputPointerTokens(pointer)
	if err != nil {
		return nil, err
	}
	value := source
	for _, token := range tokens {
		switch current := value.(type) {
		case map[string]any:
			found, ok := current[token]
			if !ok {
				return nil, fmt.Errorf("missing source value at %q", pointer)
			}
			value = found
		case []any:
			if token == "" || (len(token) > 1 && token[0] == '0') {
				return nil, fmt.Errorf("invalid array index at %q", pointer)
			}
			for _, c := range token {
				if c < '0' || c > '9' {
					return nil, fmt.Errorf("invalid array index at %q", pointer)
				}
			}
			index, err := strconv.ParseUint(token, 10, 32)
			if err != nil || index >= uint64(len(current)) {
				return nil, fmt.Errorf("array index out of bounds at %q", pointer)
			}
			value = current[index]
		default:
			return nil, fmt.Errorf("cannot traverse scalar at %q", pointer)
		}
	}
	return value, nil
}

// CheckRunInputCorrection preserves every value consumed from Run inputs by an
// existing execution. Unbound snapshots consume the complete input object.
func CheckRunInputCorrection(bindingsRaw, snapshotRaw, proposedRaw json.RawMessage) error {
	var proposed, snapshot any
	if err := strictDecode(proposedRaw, &proposed); err != nil {
		return err
	}
	if _, ok := proposed.(map[string]any); !ok {
		return fmt.Errorf("Run inputs must be an object")
	}
	if err := strictDecode(snapshotRaw, &snapshot); err != nil {
		return err
	}
	bindings, err := OptionalInputBindings(bindingsRaw)
	if err != nil {
		return err
	}
	same := func(a, b any) bool {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return string(left) == string(right)
	}
	if bindings == nil {
		if !same(snapshot, proposed) {
			return fmt.Errorf("correction changes consumed Run input")
		}
		return nil
	}
	var frozen BoundInputSnapshot
	if err := strictDecode(snapshotRaw, &frozen); err != nil {
		return err
	}
	if frozen.SchemaVersion != InputSnapshotVersion {
		return fmt.Errorf("invalid frozen input snapshot")
	}
	var data map[string]any
	if err := strictDecode(frozen.Data, &data); err != nil {
		return err
	}
	for _, binding := range bindings.Data {
		if binding.Source.Kind != "run_input" {
			continue
		}
		value, err := resolveInputPointer(proposed, binding.Source.Pointer)
		if err != nil {
			return err
		}
		previous, ok := data[binding.Target]
		if !ok || !same(previous, value) {
			return fmt.Errorf("correction changes consumed input field %q", binding.Target)
		}
	}
	return nil
}
