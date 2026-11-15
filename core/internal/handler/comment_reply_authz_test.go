package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestTaskCoversReplyParent pins the comment-reply authorization allow-list
// (SILI-4348): a comment-triggered task may reply under its trigger comment OR
// under any earlier comment it coalesced, and nothing else. This is what lets
// a coalesced cross-thread run answer each thread in its own thread instead of
// being rejected with "parent_id must equal this task's trigger comment id".
func TestRunCoversReplyParent(t *testing.T) {
	trigger := "11111111-1111-1111-1111-111111111111"
	c1 := "22222222-2222-2222-2222-222222222222"
	c2 := "33333333-3333-3333-3333-333333333333"
	stranger := "99999999-9999-9999-9999-999999999999"

	agentRun := db.AgentRun{
		TriggerCommentID: util.MustParseUUID(trigger),
		CoalescedCommentIds: []pgtype.UUID{
			util.MustParseUUID(c1),
			util.MustParseUUID(c2),
		},
	}

	cases := []struct {
		name   string
		parent pgtype.UUID
		want   bool
	}{
		{"trigger comment allowed", util.MustParseUUID(trigger), true},
		{"first coalesced comment allowed", util.MustParseUUID(c1), true},
		{"second coalesced comment allowed", util.MustParseUUID(c2), true},
		{"unrelated comment rejected", util.MustParseUUID(stranger), false},
		{"invalid/empty parent rejected", pgtype.UUID{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runCoversReplyParent(agentRun, tc.parent); got != tc.want {
				t.Errorf("taskCoversReplyParent(%s) = %v, want %v", uuidToString(tc.parent), got, tc.want)
			}
		})
	}

	// An assignment-triggered task (no trigger comment, no coalesced set) covers
	// nothing here; the caller only consults this when TriggerCommentID is valid.
	empty := db.AgentRun{}
	if runCoversReplyParent(empty, util.MustParseUUID(trigger)) {
		t.Errorf("a task with no trigger/coalesced comments must not authorize any parent")
	}
}
