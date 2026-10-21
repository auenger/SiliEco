package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/silieco-ai/silieco/core/internal/util"
)

func TestWorkflowGateFreezesSelfReviewer(t *testing.T) {
	for _, kind := range []string{"agent", "hybrid"} {
		t.Run(kind, func(t *testing.T) {
			pool, svc, p := workflowExecutionFixture(t)
			ctx := context.Background()
			var creator string
			if err := pool.QueryRow(ctx, `SELECT created_by FROM workflow_instance WHERE id=$1`, p.InstanceID).Scan(&creator); err != nil {
				t.Fatal(err)
			}
			gate := fmt.Sprintf(`{"type":%q,"decider":"@self","human_decider":%q}`, kind, creator)
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET gate=$2 WHERE id=$1`, p.StageID, gate); err != nil {
				t.Fatal(err)
			}
			e, err := svc.EnsureStageExecution(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Gate struct {
					Decider     string `json:"decider"`
					DeciderType string `json:"decider_type"`
				} `json:"gate"`
			}
			if err := json.Unmarshal(e.ConfigSnapshot, &config); err != nil {
				t.Fatal(err)
			}
			if config.Gate.Decider != util.UUIDToString(p.ExecutorID) || config.Gate.DeciderType != "agent" {
				t.Fatalf("unresolved reviewer: %s", e.ConfigSnapshot)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET gate='{"type":"agent","decider":"invalid"}' WHERE id=$1`, p.StageID); err != nil {
				t.Fatal(err)
			}
			retry, err := svc.EnsureStageExecution(ctx, p)
			if err != nil || string(retry.ConfigSnapshot) != string(e.ConfigSnapshot) {
				t.Fatalf("reviewer changed on retry: %+v %v", retry, err)
			}
		})
	}
}

func TestWorkflowGateRejectsUnavailableReviewers(t *testing.T) {
	for _, gate := range []string{`{"type":"agent"}`, `{"type":"agent","decider":"invalid"}`, fmt.Sprintf(`{"type":"agent","decider":%q}`, uuid.NewString()), `{"type":"hybrid","decider":"@self"}`, fmt.Sprintf(`{"type":"hybrid","decider":"@self","human_decider":%q}`, uuid.NewString())} {
		t.Run(gate, func(t *testing.T) {
			pool, svc, p := workflowExecutionFixture(t)
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET gate=$2 WHERE id=$1`, p.StageID, gate); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EnsureStageExecution(ctx, p); err == nil {
				t.Fatal("unavailable reviewer accepted")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid gate persisted execution: %d %v", count, err)
			}
		})
	}
}
