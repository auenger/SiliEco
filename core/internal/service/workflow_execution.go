package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/storage"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

var ErrWorkflowExecutionConflict = errors.New("workflow execution state changed")

// WorkflowExecutionService owns the transactional SOP execution lifecycle.
// Callers must authorize workspace membership and execution access before entry.
// Dispatch workers must revalidate those permissions when delivering an intent.
type WorkflowExecutionService struct {
	Queries     *db.Queries
	TxStarter   TxStarter
	TaskService *TaskService
	Storage     storage.Storage
}

type EnsureWorkflowExecutionParams struct {
	WorkspaceID      pgtype.UUID
	InstanceID       pgtype.UUID
	StageID          pgtype.UUID
	ExpectedRevision int32
	ExecutorID       pgtype.UUID
	InputSnapshot    json.RawMessage
}

// EnsureStageExecution durably creates the first business attempt and its
// dispatch intent together. Repeated calls return the same attempt, including
// after process restart. Failed/cancelled attempts require explicit rework;
// retries of this operation never silently create another business attempt.
func (s *WorkflowExecutionService) EnsureStageExecution(ctx context.Context, params EnsureWorkflowExecutionParams) (db.WorkflowStageExecution, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkflowStageExecution{}, err
	}
	defer tx.Rollback(ctx)
	execution, err := s.EnsureStageExecutionInTx(ctx, s.Queries.WithTx(tx), params)
	if err != nil {
		return db.WorkflowStageExecution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowStageExecution{}, err
	}
	return execution, nil
}

// EnsureStageExecutionInTx shares the caller's Run creation/transition
// transaction. The caller commits only after all state and intents are ready.
func (s *WorkflowExecutionService) EnsureStageExecutionInTx(ctx context.Context, q *db.Queries, params EnsureWorkflowExecutionParams) (db.WorkflowStageExecution, error) {
	empty := db.WorkflowStageExecution{}
	if !params.WorkspaceID.Valid || !params.InstanceID.Valid || !params.StageID.Valid || !params.ExecutorID.Valid || params.ExpectedRevision < 1 {
		return empty, fmt.Errorf("workspace, Run, stage, executor and plan revision are required")
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(params.InputSnapshot, &input); err != nil || input == nil {
		return empty, fmt.Errorf("input snapshot must be a JSON object")
	}
	instance, err := lockWorkflowExecutionRun(ctx, q, params.WorkspaceID, params.InstanceID)
	if err != nil {
		return empty, err
	}
	if instance.Status != "active" || instance.ArchivedAt.Valid || instance.Revision != params.ExpectedRevision || instance.CurrentStageID != params.StageID {
		return empty, ErrWorkflowExecutionConflict
	}

	stage, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: params.StageID, WorkflowInstanceID: params.InstanceID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return empty, err
	}
	if stage.ExecutionMode != "agent" || stage.ExecutorID != params.ExecutorID {
		return empty, ErrWorkflowExecutionConflict
	}
	if _, err := workflowcontract.FromSpec(stage.OutputSpec, true); err != nil {
		return empty, err
	}
	executor, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: params.ExecutorID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return empty, fmt.Errorf("resolve stage executor: %w", err)
	}
	if executor.ArchivedAt.Valid {
		return empty, fmt.Errorf("stage executor is archived")
	}
	attempt := int32(1)
	previous, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID, WorkflowStageID: params.StageID})
	if err == nil {
		if previous.PlanRevision != params.ExpectedRevision || previous.ExecutorID != params.ExecutorID {
			return empty, ErrWorkflowExecutionConflict
		}
		if previous.Status != "cancelled" || previous.FailureCode.String != "rework_invalidated" {
			return previous, nil
		}
		if previous.Attempt == 2147483647 {
			return empty, ErrWorkflowExecutionConflict
		}
		attempt = previous.Attempt + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	frozenGate, err := freezeWorkflowExecutionGate(ctx, q, instance, params.ExecutorID, stage.Gate)
	if err != nil {
		return empty, err
	}
	executionInput, inputFailure, err := s.resolveWorkflowExecutionInput(ctx, q, instance, stage, params.InputSnapshot)
	if err != nil {
		return empty, err
	}
	// RawMessage keeps JSON configuration as objects instead of base64 strings.
	snapshot, err := json.Marshal(struct {
		StageID                pgtype.UUID     `json:"stage_id"`
		StableKey              string          `json:"stable_key"`
		Name                   string          `json:"name"`
		Description            pgtype.Text     `json:"description"`
		CompletionRule         json.RawMessage `json:"completion_rule"`
		ReworkPolicy           json.RawMessage `json:"rework_policy"`
		RetryPolicy            json.RawMessage `json:"retry_policy"`
		InputSpec              json.RawMessage `json:"input_spec"`
		InputBindings          json.RawMessage `json:"input_bindings"`
		OutputSpec             json.RawMessage `json:"output_spec"`
		Gate                   json.RawMessage `json:"gate"`
		RequiredSkills         []string        `json:"required_skills"`
		RequiredSkillSnapshots json.RawMessage `json:"required_skill_snapshots"`
		AllowedMCPServerIDs    []pgtype.UUID   `json:"allowed_mcp_server_ids"`
		RollbackStageKey       pgtype.Text     `json:"rollback_stage_key"`
		Position               int32           `json:"position"`
	}{
		StageID: stage.ID, StableKey: stage.StableKey, Name: stage.Name, Description: stage.Description,
		CompletionRule: stage.CompletionRule, ReworkPolicy: stage.ReworkPolicy, RetryPolicy: stage.RetryPolicy, InputSpec: stage.InputSpec, InputBindings: stage.InputBindings, OutputSpec: stage.OutputSpec, Gate: frozenGate,
		RequiredSkills: stage.RequiredSkills, RequiredSkillSnapshots: stage.RequiredSkillSnapshots,
		AllowedMCPServerIDs: stage.AllowedMcpServerIds, RollbackStageKey: stage.RollbackStageKey, Position: stage.Position,
	})
	if err != nil {
		return empty, fmt.Errorf("snapshot stage configuration: %w", err)
	}
	execution, err := q.CreateWorkflowStageExecution(ctx, db.CreateWorkflowStageExecutionParams{
		WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID, WorkflowStageID: params.StageID,
		Attempt: attempt, PlanRevision: instance.Revision, ExecutorID: params.ExecutorID, ConfigSnapshot: snapshot, InputSnapshot: executionInput,
	})
	if err != nil {
		return empty, err
	}
	if inputFailure != "" {
		return q.BlockWorkflowStageExecution(ctx, db.BlockWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID, FailureCode: pgtype.Text{String: "input_unavailable", Valid: true}, FailureDetail: pgtype.Text{String: inputFailure, Valid: true}})
	}
	if _, err := q.CreateWorkflowExecutionAction(ctx, db.CreateWorkflowExecutionActionParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID, ExecutionID: execution.ID, Kind: "dispatch"}); err != nil {
		return empty, err
	}
	return execution, nil
}

func lockWorkflowExecutionRun(ctx context.Context, q *db.Queries, workspaceID, instanceID pgtype.UUID) (db.WorkflowInstance, error) {
	if _, err := q.LockWorkspaceForWorkflowExecution(ctx, workspaceID); err != nil {
		return db.WorkflowInstance{}, err
	}
	// Lock parent resources before the Run, matching deletion's lock order.
	observed, err := q.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{ID: instanceID, WorkspaceID: workspaceID})
	if err != nil {
		return db.WorkflowInstance{}, err
	}
	if observed.ProjectID.Valid {
		if _, err := q.LockProjectForWorkflowExecution(ctx, db.LockProjectForWorkflowExecutionParams{ID: observed.ProjectID, WorkspaceID: workspaceID}); err != nil {
			return db.WorkflowInstance{}, err
		}
	}
	instance, err := q.LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: instanceID, WorkspaceID: workspaceID})
	if err != nil {
		return db.WorkflowInstance{}, err
	}
	if instance.ProjectID != observed.ProjectID {
		return db.WorkflowInstance{}, ErrWorkflowExecutionConflict
	}
	return instance, nil
}

// resolveWorkflowExecutionInput is shared by initial creation and recovery from
// an input blocker. Callers choose the live or frozen stage configuration.
func (s *WorkflowExecutionService) resolveWorkflowExecutionInput(ctx context.Context, q *db.Queries, instance db.WorkflowInstance, stage db.WorkflowInstanceStage, input json.RawMessage) (json.RawMessage, string, error) {
	inputContract, err := workflowcontract.FromSpec(stage.InputSpec, false)
	if err != nil {
		return nil, "", err
	}
	executionInput := input
	inputData := input
	var inputFailure string
	bindings, err := workflowcontract.OptionalInputBindings(stage.InputBindings)
	if err != nil {
		return nil, "", err
	}
	if bindings != nil {
		if inputContract == nil {
			return nil, "", fmt.Errorf("input_spec.contract is required for input bindings")
		}
		if err := bindings.CheckContract(*inputContract); err != nil {
			return nil, "", err
		}
		var sourceFailure error
		resolved, err := workflowcontract.ResolveInputBindings(*bindings, *inputContract,
			workflowcontract.RunInputSource{InstanceID: util.UUIDToString(instance.ID), PlanRevision: instance.Revision, Data: instance.Inputs},
			func(stageKey string) (workflowcontract.AcceptedInputResult, error) {
				result, err := s.resolveAcceptedInputResult(ctx, q, instance, stage, stageKey)
				sourceFailure = err
				return result, err
			})
		if err != nil {
			// Database/transport failures must roll back instead of being saved
			// as a business input blocker. Only unavailable evidence is expected.
			if sourceFailure != nil && !errors.Is(sourceFailure, pgx.ErrNoRows) && !errors.Is(sourceFailure, ErrWorkflowExecutionConflict) {
				return nil, "", err
			}
			inputFailure = err.Error()
			executionInput = json.RawMessage(`{}`)
		} else {
			inputData = resolved.Data
			executionInput, err = json.Marshal(resolved)
			if err != nil {
				return nil, "", err
			}
		}
	}
	if inputContract != nil && inputFailure == "" {
		if len(inputContract.Artifacts) > 0 {
			return nil, "", fmt.Errorf("input contract artifact bindings are not supported yet")
		}
		violations, err := inputContract.ValidateData(inputData)
		if err != nil {
			return nil, "", err
		}
		if len(violations) > 0 {
			inputFailure = fmt.Sprintf("stage input %s: %s", violations[0].Path, violations[0].Message)
			executionInput = json.RawMessage(`{}`)
		}
	}

	return executionInput, inputFailure, nil
}
