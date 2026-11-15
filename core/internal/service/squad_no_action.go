package service

import (
	"context"

	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// HasSquadLeaderNoActionEvaluationForTask reports whether this exact task
// already recorded a squad leader no_action evaluation.
func HasSquadLeaderNoActionEvaluationForRun(ctx context.Context, q *db.Queries, agentRun db.AgentRun) (bool, error) {
	if q == nil || !agentRun.ID.Valid || !agentRun.TaskID.Valid || !agentRun.AgentID.Valid {
		return false, nil
	}
	return q.HasSquadLeaderNoActionEvaluationForRun(ctx, db.HasSquadLeaderNoActionEvaluationForRunParams{
		TaskID:  agentRun.TaskID,
		AgentID: agentRun.AgentID,
		RunID:   util.UUIDToString(agentRun.ID),
	})
}
