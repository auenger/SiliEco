package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

var ErrWorkflowResultAccess = errors.New("workflow result submission is not authorized")

type WorkflowResultValidationError struct{ Violations []workflowcontract.Violation }

func (e *WorkflowResultValidationError) Error() string { return "workflow result failed validation" }

// Actor identity and source task must come from authenticated request context.
// ExpectedRevision is the last result revision read by the caller (zero initially).
type SubmitWorkflowResultParams struct {
	WorkspaceID      pgtype.UUID
	InstanceID       pgtype.UUID
	ExecutionID      pgtype.UUID
	ActorType        string
	ActorID          pgtype.UUID
	SourceTaskID     pgtype.UUID
	ExpectedRevision int32
	Submission       json.RawMessage
}

type stagedWorkflowArtifact struct {
	descriptor   workflowcontract.Artifact
	attachmentID pgtype.UUID
	uploadID     pgtype.UUID
	size         int64
}

func (s *WorkflowExecutionService) authorizeResult(ctx context.Context, q *db.Queries, p SubmitWorkflowResultParams) (db.WorkflowStageExecution, error) {
	empty := db.WorkflowStageExecution{}
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		return empty, err
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		return empty, err
	}
	if execution.WorkflowInstanceID != run.ID || execution.WorkflowStageID != run.CurrentStageID || execution.PlanRevision != run.Revision || run.Status != "active" || run.ArchivedAt.Valid || execution.ResultRevision != p.ExpectedRevision || !execution.MainIssueID.Valid || (execution.Status != "queued" && execution.Status != "running" && execution.Status != "awaiting_review") {
		return empty, ErrWorkflowExecutionConflict
	}
	// A moved/cancelled issue cannot submit against its former stage binding.
	issue, err := q.GetWorkflowResultIssue(ctx, db.GetWorkflowResultIssueParams{ID: execution.MainIssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		return empty, err
	}
	if issue.WorkflowInstanceID != run.ID || issue.WorkflowStageID != execution.WorkflowStageID || issue.Status == "cancelled" {
		return empty, ErrWorkflowExecutionConflict
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: run.CreatedBy}); err != nil {
		return empty, ErrWorkflowResultAccess
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: execution.ExecutorID, WorkspaceID: p.WorkspaceID})
	if err != nil || agent.ArchivedAt.Valid {
		return empty, ErrWorkflowResultAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, agent, run.CreatedBy)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, ErrWorkflowResultAccess
	}
	switch p.ActorType {
	case "member":
		if p.ActorID != run.CreatedBy {
			return empty, ErrWorkflowResultAccess
		}
	case "agent":
		if p.ActorID != execution.ExecutorID || !p.SourceTaskID.Valid || p.SourceTaskID != execution.MainTaskID {
			return empty, ErrWorkflowResultAccess
		}
		task, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{ID: p.SourceTaskID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return empty, ErrWorkflowResultAccess
		}
		if task.AgentID != p.ActorID || task.WorkflowStageExecutionID != execution.ID || task.IssueID != execution.MainIssueID || (task.Status != "running" && task.Status != "completed") {
			return empty, ErrWorkflowResultAccess
		}
	default:
		return empty, ErrWorkflowResultAccess
	}
	return execution, nil
}

// SubmitResult freezes validated storage bytes and commits a new immutable
// result revision. It does not accept the stage or advance the Run.
func (s *WorkflowExecutionService) SubmitResult(ctx context.Context, p SubmitWorkflowResultParams) (db.WorkflowExecutionResult, error) {
	empty := db.WorkflowExecutionResult{}
	if !p.WorkspaceID.Valid || !p.InstanceID.Valid || !p.ExecutionID.Valid || !p.ActorID.Valid || p.ExpectedRevision < 0 || p.ExpectedRevision == 2147483647 {
		return empty, ErrWorkflowResultAccess
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	execution, err := s.authorizeResult(ctx, q, p)
	if err != nil {
		return empty, err
	}
	var snapshot struct {
		OutputSpec json.RawMessage `json:"output_spec"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &snapshot); err != nil {
		return empty, err
	}
	contract, err := workflowcontract.FromSpec(snapshot.OutputSpec, true)
	if err != nil {
		return empty, err
	}
	violations, err := contract.ValidateSubmission(p.Submission)
	if err != nil {
		return empty, err
	}
	if len(violations) > 0 {
		return empty, &WorkflowResultValidationError{violations}
	}
	var submission workflowcontract.Submission
	if err := json.Unmarshal(p.Submission, &submission); err != nil {
		return empty, err
	}
	if len(submission.Artifacts) > 0 && s.Storage == nil {
		return empty, errors.New("workflow artifact storage unavailable")
	}
	// Persist upload intents under the parent locks, before performing any I/O.
	staged := make([]stagedWorkflowArtifact, 0, len(submission.Artifacts))
	attachments := make([]db.Attachment, 0, len(submission.Artifacts))
	for _, descriptor := range submission.Artifacts {
		attachmentID, err := util.ParseUUID(descriptor.AttachmentID)
		if err != nil {
			return empty, err
		}
		attachment, err := q.GetWorkflowExecutionAttachment(ctx, db.GetWorkflowExecutionAttachmentParams{AttachmentID: attachmentID, WorkspaceID: p.WorkspaceID, IssueID: execution.MainIssueID, TaskID: execution.MainTaskID})
		if err != nil {
			return empty, ErrWorkflowResultAccess
		}
		uploadID := util.MustParseUUID(uuid.NewString())
		key := "workflow-artifacts/" + uuid.UUID(p.WorkspaceID.Bytes).String() + "/" + uuid.UUID(uploadID.Bytes).String()
		if _, err := q.CreateWorkflowArtifactUpload(ctx, db.CreateWorkflowArtifactUploadParams{ID: uploadID, WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, StorageKey: key}); err != nil {
			return empty, err
		}
		staged = append(staged, stagedWorkflowArtifact{descriptor: descriptor, attachmentID: attachmentID, uploadID: uploadID})
		attachments = append(attachments, attachment)
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, artifact := range staged {
			// A commit with a lost acknowledgement must never delete committed bytes.
			_ = s.Queries.AbandonWorkflowArtifactUpload(cleanupCtx, db.AbandonWorkflowArtifactUploadParams{ID: artifact.uploadID, WorkspaceID: p.WorkspaceID})
		}
	}()
	for i, attachment := range attachments {
		content, err := s.readWorkflowArtifact(ctx, attachment, staged[i].descriptor)
		if err != nil {
			return empty, &WorkflowResultValidationError{[]workflowcontract.Violation{{Path: fmt.Sprintf("/artifacts/%d", i), Code: "artifact_content", Message: err.Error()}}}
		}
		key := "workflow-artifacts/" + uuid.UUID(p.WorkspaceID.Bytes).String() + "/" + uuid.UUID(staged[i].uploadID.Bytes).String()
		if _, err := s.Storage.Upload(ctx, key, content, staged[i].descriptor.MediaType, attachment.Filename); err != nil {
			return empty, fmt.Errorf("freeze workflow artifact: %w", err)
		}
		staged[i].size = int64(len(content))
	}
	tx, err = s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q = s.Queries.WithTx(tx)
	execution, err = s.authorizeResult(ctx, q, p)
	if err != nil {
		return empty, err
	}
	result, err := q.CreateWorkflowExecutionResult(ctx, db.CreateWorkflowExecutionResultParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ExecutionID: p.ExecutionID, Revision: p.ExpectedRevision + 1, Data: submission.Data, SubmittedByType: p.ActorType, SubmittedByID: p.ActorID})
	if err != nil {
		return empty, err
	}
	for _, item := range staged {
		count, err := q.CommitWorkflowArtifactUpload(ctx, db.CommitWorkflowArtifactUploadParams{ID: item.uploadID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return empty, err
		}
		if count != 1 {
			return empty, ErrWorkflowExecutionConflict
		}
		artifact, err := q.GetOrCreateWorkflowArtifact(ctx, db.GetOrCreateWorkflowArtifactParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: execution.WorkflowStageID, Name: item.descriptor.Name})
		if err != nil {
			return empty, err
		}
		revision, err := q.NextWorkflowArtifactRevision(ctx, db.NextWorkflowArtifactRevisionParams{WorkspaceID: p.WorkspaceID, ArtifactID: artifact.ID})
		if err != nil {
			return empty, err
		}
		if _, err := q.CreateWorkflowArtifactRevision(ctx, db.CreateWorkflowArtifactRevisionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ArtifactID: artifact.ID, ExecutionID: p.ExecutionID, ResultID: result.ID, UploadID: item.uploadID, Revision: revision, SourceAttachmentID: item.attachmentID, Kind: item.descriptor.Kind, MediaType: item.descriptor.MediaType, SchemaVersion: item.descriptor.SchemaVersion, Checksum: item.descriptor.Checksum, SizeBytes: item.size}); err != nil {
			return empty, err
		}
	}
	if _, err := q.SubmitWorkflowExecutionResult(ctx, db.SubmitWorkflowExecutionResultParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID, ResultRevision: p.ExpectedRevision + 1, ResultRevision_2: p.ExpectedRevision}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrWorkflowExecutionConflict
		}
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	committed = true
	return result, nil
}

func (s *WorkflowExecutionService) readWorkflowArtifact(ctx context.Context, attachment db.Attachment, descriptor workflowcontract.Artifact) ([]byte, error) {
	key := s.Storage.KeyFromURL(attachment.Url)
	if key == "" || key == attachment.Url || s.Storage.ObjectURL(key) != attachment.Url {
		return nil, errors.New("attachment is not in managed storage")
	}
	if attachment.SizeBytes < 1 || attachment.SizeBytes > workflowcontract.MaxArtifactBytes {
		return nil, errors.New("artifact size exceeds allowed range")
	}
	reader, err := s.Storage.GetReader(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, workflowcontract.MaxArtifactBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(content)) != attachment.SizeBytes {
		return nil, errors.New("attachment byte count does not match metadata")
	}
	if workflowcontract.Checksum(content) != descriptor.Checksum {
		return nil, errors.New("attachment checksum does not match content")
	}
	if err := workflowcontract.ValidateArtifactContent(descriptor.MediaType, content); err != nil {
		return nil, err
	}
	return content, nil
}
