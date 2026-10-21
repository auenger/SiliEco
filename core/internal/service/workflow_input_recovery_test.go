package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	"sync"
	"testing"
)

func TestWorkflowInputRecoveryUsesFrozenContractAndSingleDispatch(t *testing.T) {
	pool, svc, p := workflowExecutionFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET input_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}}}',input_bindings='{"schema_version":"silieco.input-bindings/v1","data":[{"target":"goal","source":{"kind":"run_input","pointer":"/goal"}}]}' WHERE id=$1`, p.StageID); err != nil {
		t.Fatal(err)
	}
	e, err := svc.EnsureStageExecution(ctx, p)
	if err != nil || e.Status != "blocked" {
		t.Fatalf("block: %+v %v", e, err)
	}
	var actor pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT created_by FROM workflow_instance WHERE id=$1`, p.InstanceID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	resume := func() error {
		r, err := svc.ResumeInputBlockedExecution(ctx, p.WorkspaceID, p.InstanceID, e.ID, actor)
		if err == nil && (r.ID != e.ID || r.Attempt != 1 || r.Status != "pending") {
			t.Errorf("changed identity: %+v", r)
		}
		return err
	}
	if err := resume(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("missing input resumed: %v", err)
	}
	// A live plan edit cannot weaken the contract frozen by the blocked attempt.
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET input_spec='{}',input_bindings='{}' WHERE id=$1`, p.StageID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET inputs='{"goal":7}' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := resume(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("frozen contract bypass: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET inputs='{"goal":"deliver"}' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	constraint := "recovery_failure_" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_execution_action ADD CONSTRAINT %s CHECK(execution_id <> '%s')`, constraint, util.UUIDToString(e.ID))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "ALTER TABLE workflow_execution_action DROP CONSTRAINT IF EXISTS "+constraint) })
	if err := resume(); err == nil {
		t.Fatal("outbox failure was ignored")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_stage_execution WHERE id=$1`, e.ID).Scan(&status); err != nil || status != "blocked" {
		t.Fatalf("partial resume: %s %v", status, err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_execution_action DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- resume() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE execution_id=$1 AND kind='dispatch'`, e.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dispatch count %d %v", count, err)
	}
	var goal string
	if err := pool.QueryRow(ctx, `SELECT input_snapshot->'data'->>'goal' FROM workflow_stage_execution WHERE id=$1 AND failure_code IS NULL`, e.ID).Scan(&goal); err != nil || goal != "deliver" {
		t.Fatalf("resolved input %q %v", goal, err)
	}
}

func TestWorkflowInputCorrectionAndResumeAreAtomic(t *testing.T) {
	pool, svc, p := workflowExecutionFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET input_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}}}' WHERE id=$1`, p.StageID); err != nil {
		t.Fatal(err)
	}
	e, err := svc.EnsureStageExecution(ctx, p)
	if err != nil || e.Status != "blocked" {
		t.Fatalf("block: %+v %v", e, err)
	}
	var actor pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT created_by FROM workflow_instance WHERE id=$1`, p.InstanceID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	correct := func(expected, proposed string) error {
		_, err := svc.CorrectBlockedExecutionInputs(ctx, p.WorkspaceID, p.InstanceID, e.ID, actor, json.RawMessage(expected), json.RawMessage(proposed))
		return err
	}
	if err := correct(`{}`, `{"goal":7}`); err == nil {
		t.Fatal("invalid correction resumed")
	}
	var inputs string
	if err := pool.QueryRow(ctx, `SELECT inputs::text FROM workflow_instance WHERE id=$1`, p.InstanceID).Scan(&inputs); err != nil || inputs != "{}" {
		t.Fatalf("failed correction persisted: %s %v", inputs, err)
	}
	if err := correct(`{"old":true}`, `{"goal":"new"}`); err == nil {
		t.Fatal("stale correction accepted")
	}
	if err := correct(`{}`, `{"goal":"new"}`); err != nil {
		t.Fatal(err)
	}
	var status, goal string
	if err := pool.QueryRow(ctx, `SELECT e.status,r.inputs->>'goal' FROM workflow_stage_execution e JOIN workflow_instance r ON r.id=e.workflow_instance_id WHERE e.id=$1`, e.ID).Scan(&status, &goal); err != nil || status != "pending" || goal != "new" {
		t.Fatalf("correction not committed: %s %s %v", status, goal, err)
	}
}
