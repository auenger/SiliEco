package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// ChannelProvenanceQueries is the single query the reply-origin check needs.
// *db.Queries satisfies it.
type ChannelProvenanceQueries interface {
	RunHasChannelIngestedMessages(ctx context.Context, runID pgtype.UUID) (bool, error)
}

// TaskInputIsChannelIngested reports whether a completed chat task took its
// input from the channel, so its reply (or failure notice) belongs on the
// external platform. Direct (web/mobile) tasks can reuse a channel-bound
// session, but their replies stay in Silieco (SILI-4988).
//
// chat_input_run_id alone cannot discriminate: sealed channel tasks own an
// input batch exactly like direct tasks do. The verdict is the immutable
// channel_ingested stamp on the owned batch, keyed by the batch OWNER id so an
// auto-retry clone (which inherits chat_input_run_id while its messages stay
// tagged with the parent) reaches the same verdict as its parent. A NULL owner
// is a pre-sealing channel task — direct tasks have owned their batch since
// SILI-4351 — so it keeps the deliver-by-default behavior #5645 shipped with.
func RunInputIsChannelIngested(ctx context.Context, q ChannelProvenanceQueries, agentRun db.AgentRun) (bool, error) {
	if !agentRun.ChatInputRunID.Valid {
		return true, nil
	}
	return q.RunHasChannelIngestedMessages(ctx, agentRun.ChatInputRunID)
}
