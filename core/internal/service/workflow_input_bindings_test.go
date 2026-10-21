package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func TestWorkflowInputBindingsUseLockedRunInputs(t *testing.T) {
	pool, svc, p := workflowExecutionFixture(t)
	ctx := context.Background()
	bindings := `{"schema_version":"silieco.input-bindings/v1","data":[{"target":"limit","source":{"kind":"run_input","pointer":"/requirements/limit"}}]}`
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET inputs='{"requirements":{"limit":80}}' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET input_bindings=$2,input_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["limit"],"properties":{"limit":{"type":"number"}}}}}' WHERE id=$1`, p.StageID, bindings); err != nil {
		t.Fatal(err)
	}
	p.InputSnapshot = json.RawMessage(`{"limit":999}`)
	execution, err := svc.EnsureStageExecution(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot workflowcontract.BoundInputSnapshot
	if err := json.Unmarshal(execution.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != workflowcontract.InputSnapshotVersion || string(snapshot.Data) != `{"limit": 80}` && string(snapshot.Data) != `{"limit":80}` || len(snapshot.Bindings) != 1 {
		t.Fatalf("wrong frozen input: %s", execution.InputSnapshot)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(execution.ConfigSnapshot, &config); err != nil {
		t.Fatal(err)
	}
	if len(config["input_bindings"]) == 0 {
		t.Fatal("binding configuration was not frozen")
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET inputs='{}' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	repeated, err := svc.EnsureStageExecution(ctx, p)
	if err != nil || repeated.ID != execution.ID || string(repeated.InputSnapshot) != string(execution.InputSnapshot) {
		t.Fatalf("existing execution input changed: %+v %v", repeated, err)
	}
}

func TestWorkflowInputBindingsFailurePersistsBlockedExecution(t *testing.T) {
	for _, source := range []string{`{"kind":"run_input","pointer":"/missing"}`, `{"kind":"stage_result","stage_key":"previous","pointer":"/limit"}`} {
		t.Run(source, func(t *testing.T) {
			pool, svc, p := workflowExecutionFixture(t)
			ctx := context.Background()
			bindings := `{"schema_version":"silieco.input-bindings/v1","data":[{"target":"limit","source":` + source + `}]}`
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET input_bindings=$2,input_spec='{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["limit"],"properties":{"limit":{"type":"number"}}}}}' WHERE id=$1`, p.StageID, bindings); err != nil {
				t.Fatal(err)
			}
			execution, err := svc.EnsureStageExecution(ctx, p)
			if err != nil || execution.Status != "blocked" || execution.FailureCode.String != "input_unavailable" || execution.FailureDetail.String == "" || string(execution.InputSnapshot) != "{}" {
				t.Fatalf("missing durable input blocker: %+v %v", execution, err)
			}
			repeated, err := svc.EnsureStageExecution(ctx, p)
			if err != nil || repeated.ID != execution.ID || repeated.Status != "blocked" {
				t.Fatalf("blocked retry changed attempt: %+v %v", repeated, err)
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("blocked execution: %d %v", count, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial action: %d %v", count, err)
			}
		})
	}
}

func TestWorkflowInputBindingsHandoffKeepsAcceptedPredecessor(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	var nextID string
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode,executor_id,input_spec,input_bindings,output_spec,completion_rule,gate)
 SELECT workspace_id,workflow_instance_id,'next','Next',position+1,'agent',executor_id,
 '{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}}}',
 '{"schema_version":"silieco.input-bindings/v1","data":[{"target":"goal","source":{"kind":"run_input","pointer":"/missing"}}]}',output_spec,completion_rule,'{"type":"none"}'
 FROM workflow_instance_stage WHERE id=(SELECT workflow_stage_id FROM workflow_stage_execution WHERE id=$1) RETURNING id`, p.ExecutionID).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}','{"type":"none"}') WHERE id=$1`, p.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=(SELECT main_issue_id FROM workflow_stage_execution WHERE id=$1)`, p.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=(SELECT main_task_id FROM workflow_stage_execution WHERE id=$1)`, p.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AcceptAutomaticExecution(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID); err != nil {
		t.Fatal(err)
	}
	var runStage, status, code string
	if err := pool.QueryRow(ctx, `SELECT r.current_stage_id,e.status,e.failure_code FROM workflow_instance r JOIN workflow_stage_execution e ON e.workflow_instance_id=r.id AND e.workflow_stage_id=r.current_stage_id WHERE r.id=$1`, p.InstanceID).Scan(&runStage, &status, &code); err != nil || runStage != nextID || status != "blocked" || code != "input_unavailable" {
		t.Fatalf("handoff not blocked: %s %s %s %v", runStage, status, code, err)
	}
	var accepted bool
	if err := pool.QueryRow(ctx, `SELECT status='succeeded' AND acceptance IS NOT NULL FROM workflow_stage_execution WHERE id=$1`, p.ExecutionID).Scan(&accepted); err != nil || !accepted {
		t.Fatalf("lost predecessor acceptance: %v %v", accepted, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action a JOIN workflow_stage_execution e ON e.id=a.execution_id WHERE e.workflow_stage_id=$1`, nextID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocked stage dispatched: %d %v", count, err)
	}
}
