package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/realtime"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// scopeAuthQuerier is the narrow subset of db.Queries used by the scope
// authorizer. Declared as an interface so the authorizer can be unit tested
// with an in-memory fake (no DB required).
type scopeAuthQuerier interface {
	GetAgentRun(ctx context.Context, id pgtype.UUID) (db.AgentRun, error)
	GetTask(ctx context.Context, id pgtype.UUID) (db.Task, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	IsChatSessionMemberParticipant(ctx context.Context, arg db.IsChatSessionMemberParticipantParams) (bool, error)
}

// dbScopeAuthorizer implements realtime.ScopeAuthorizer for the per-task and
// per-chat scopes (workspace/user scopes are validated by the hub itself
// against the connection identity). It returns true only when the requested
// resource exists, belongs to the caller's workspace, and — for chat
// resources — was created by the caller (mirroring the HTTP creator-only
// access model).
type dbScopeAuthorizer struct{ q scopeAuthQuerier }

func newScopeAuthorizer(q scopeAuthQuerier) *dbScopeAuthorizer { return &dbScopeAuthorizer{q: q} }

// scopeLookupErr converts a scope-resource query error into an authorizer
// result. A missing resource (pgx.ErrNoRows) is a legitimate denial — the
// HTTP layer treats not-found as 404 rather than 403, so the realtime layer
// reports it as a plain "forbidden" refusal. Any other error (pool
// exhaustion, a cancelled context, a network blip) is a transient lookup
// failure and must propagate so handleSubscribe reports "lookup_failed"
// instead of masking a database outage as a wave of permission denials.
func scopeLookupErr(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (a *dbScopeAuthorizer) AuthorizeScope(ctx context.Context, userID, workspaceID, scopeType, scopeID string) (bool, error) {
	if workspaceID == "" || scopeID == "" {
		return false, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false, nil
	}
	idUUID, err := util.ParseUUID(scopeID)
	if err != nil {
		return false, nil
	}
	switch scopeType {
	case realtime.ScopeRun:
		agentRun, err := a.q.GetAgentRun(ctx, idUUID)
		if err != nil {
			return scopeLookupErr(err)
		}
		// Task tasks: visible to any workspace member.
		if agentRun.TaskID.Valid {
			task, err := a.q.GetTask(ctx, agentRun.TaskID)
			if err != nil {
				return scopeLookupErr(err)
			}
			return task.WorkspaceID == wsUUID, nil
		}
		// Chat tasks: only the chat session's creator may subscribe, mirroring
		// the HTTP layer's creator-only access on chat resources.
		if agentRun.ChatSessionID.Valid {
			sess, err := a.q.GetChatSession(ctx, agentRun.ChatSessionID)
			if err != nil {
				return scopeLookupErr(err)
			}
			if sess.WorkspaceID != wsUUID {
				return false, nil
			}
			return a.authorizeChatMember(ctx, sess, userID)
		}
		return false, nil
	case realtime.ScopeChat:
		sess, err := a.q.GetChatSession(ctx, idUUID)
		if err != nil {
			return scopeLookupErr(err)
		}
		if sess.WorkspaceID != wsUUID {
			return false, nil
		}
		// Chat sessions are private to their creator (see handler/chat.go:
		// GetChatSession / SendChatMessage / MarkChatSessionRead all enforce
		// CreatorID == userID). The realtime layer must not weaken this:
		// otherwise any workspace member who learns a session_id could
		// subscribe to chat:message / chat:done / chat:session_read for a
		// peer's private chat.
		return a.authorizeChatMember(ctx, sess, userID)
	default:
		return false, nil
	}
}

func (a *dbScopeAuthorizer) authorizeChatMember(ctx context.Context, session db.ChatSession, userID string) (bool, error) {
	uidUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false, nil
	}
	if session.Kind != "group" {
		return session.CreatorID == uidUUID, nil
	}
	allowed, err := a.q.IsChatSessionMemberParticipant(ctx, db.IsChatSessionMemberParticipantParams{
		ChatSessionID: session.ID,
		ParticipantID: uidUUID,
	})
	if err != nil {
		return scopeLookupErr(err)
	}
	return allowed, nil
}
