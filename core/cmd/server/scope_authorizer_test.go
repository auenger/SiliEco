package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/realtime"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// fakeScopeQuerier implements scopeAuthQuerier with in-memory maps.
type fakeScopeQuerier struct {
	agentRuns         map[[16]byte]db.AgentRun
	tasks             map[[16]byte]db.Task
	sessions          map[[16]byte]db.ChatSession
	groupParticipants map[[32]byte]bool
}

func (f *fakeScopeQuerier) GetAgentRun(_ context.Context, id pgtype.UUID) (db.AgentRun, error) {
	if t, ok := f.agentRuns[id.Bytes]; ok {
		return t, nil
	}
	return db.AgentRun{}, pgx.ErrNoRows
}
func (f *fakeScopeQuerier) GetTask(_ context.Context, id pgtype.UUID) (db.Task, error) {
	if i, ok := f.tasks[id.Bytes]; ok {
		return i, nil
	}
	return db.Task{}, pgx.ErrNoRows
}
func (f *fakeScopeQuerier) GetChatSession(_ context.Context, id pgtype.UUID) (db.ChatSession, error) {
	if s, ok := f.sessions[id.Bytes]; ok {
		return s, nil
	}
	return db.ChatSession{}, pgx.ErrNoRows
}
func (f *fakeScopeQuerier) IsChatSessionMemberParticipant(_ context.Context, arg db.IsChatSessionMemberParticipantParams) (bool, error) {
	var key [32]byte
	copy(key[:16], arg.ChatSessionID.Bytes[:])
	copy(key[16:], arg.ParticipantID.Bytes[:])
	return f.groupParticipants[key], nil
}

func mustUUID(t *testing.T) (string, pgtype.UUID) {
	t.Helper()
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	return u.String(), pgtype.UUID{Bytes: u, Valid: true}
}

// TestScopeAuthorizer_ChatRequiresCreator pins must-fix #2 from PR #1429:
// ScopeChat MUST verify CreatorID == userID. A workspace peer that knows the
// session_id must NOT be able to subscribe to chat:message / chat:done /
// chat:session_read for that private session.
func TestScopeAuthorizer_ChatRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	otherWsStrOnly, otherWsUUID := mustUUID(t)
	_ = otherWsStrOnly

	q := &fakeScopeQuerier{
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	// Creator in matching workspace → allowed.
	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed: ok=%v err=%v", ok, err)
	}

	// Same workspace, different (peer) member → must be denied.
	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied: ok=%v err=%v", ok, err)
	}

	// Cross-workspace creator (e.g. session in workspace A, request in
	// workspace B) → must be denied even though creator matches.
	ok, err = a.AuthorizeScope(ctx, creatorStr, otherWsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
	_ = otherWsUUID

	// Empty userID → must be denied (defensive).
	ok, err = a.AuthorizeScope(ctx, "", wsStr, realtime.ScopeChat, sessStr)
	if err != nil || ok {
		t.Fatalf("empty userID must be denied: ok=%v err=%v", ok, err)
	}

	// Unknown session → denied.
	_, missingStr := mustUUID(t)
	_ = missingStr
	missingUUID, _ := uuid.NewRandom()
	ok, err = a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeChat, missingUUID.String())
	if err != nil || ok {
		t.Fatalf("unknown session must be denied: ok=%v err=%v", ok, err)
	}
}

func TestScopeAuthorizer_GroupChatAllowsParticipantsOnly(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	memberStr, memberUUID := mustUUID(t)
	peerStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	var participantKey [32]byte
	copy(participantKey[:16], sessUUID.Bytes[:])
	copy(participantKey[16:], memberUUID.Bytes[:])

	q := &fakeScopeQuerier{
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID: sessUUID, WorkspaceID: wsUUID, CreatorID: creatorUUID, Kind: "group",
			},
		},
		groupParticipants: map[[32]byte]bool{participantKey: true},
	}
	a := newScopeAuthorizer(q)

	if ok, err := a.AuthorizeScope(context.Background(), memberStr, wsStr, realtime.ScopeChat, sessStr); err != nil || !ok {
		t.Fatalf("group participant should be allowed: ok=%v err=%v", ok, err)
	}
	if ok, err := a.AuthorizeScope(context.Background(), peerStr, wsStr, realtime.ScopeChat, sessStr); err != nil || ok {
		t.Fatalf("workspace peer outside the group must be denied: ok=%v err=%v", ok, err)
	}
	if ok, err := a.AuthorizeScope(context.Background(), creatorStr, wsStr, realtime.ScopeChat, sessStr); err != nil || ok {
		t.Fatalf("group creator without a participant row must be denied: ok=%v err=%v", ok, err)
	}
}

// TestScopeAuthorizer_ChatTaskRequiresCreator pins must-fix #2 for the
// task-scope path of chat tasks (task.ChatSessionID set, no TaskID): only
// the chat session creator may subscribe to that task's stream, since
// run:message for chat tasks contains assistant chat content.
func TestScopeAuthorizer_ChatRunRequiresCreator(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	creatorStr, creatorUUID := mustUUID(t)
	otherStr, _ := mustUUID(t)
	sessStr, sessUUID := mustUUID(t)
	runStr, runUUID := mustUUID(t)
	_ = sessStr

	q := &fakeScopeQuerier{
		agentRuns: map[[16]byte]db.AgentRun{
			runUUID.Bytes: {
				ID:            runUUID,
				ChatSessionID: sessUUID,
			},
		},
		sessions: map[[16]byte]db.ChatSession{
			sessUUID.Bytes: {
				ID:          sessUUID,
				WorkspaceID: wsUUID,
				CreatorID:   creatorUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, creatorStr, wsStr, realtime.ScopeRun, runStr)
	if err != nil || !ok {
		t.Fatalf("creator should be allowed for chat run: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, otherStr, wsStr, realtime.ScopeRun, runStr)
	if err != nil || ok {
		t.Fatalf("peer must be denied for chat run: ok=%v err=%v", ok, err)
	}
}

// TestScopeAuthorizer_TaskRunWorkspaceOnly verifies task tasks remain
// workspace-scoped (any member who can see the task may subscribe).
func TestScopeAuthorizer_TaskRunWorkspaceOnly(t *testing.T) {
	wsStr, wsUUID := mustUUID(t)
	memberStr, _ := mustUUID(t)
	otherWsStr, _ := mustUUID(t)
	runStr, runUUID := mustUUID(t)
	_, taskUUID := mustUUID(t)

	q := &fakeScopeQuerier{
		agentRuns: map[[16]byte]db.AgentRun{
			runUUID.Bytes: {
				ID:     runUUID,
				TaskID: taskUUID,
			},
		},
		tasks: map[[16]byte]db.Task{
			taskUUID.Bytes: {
				ID:          taskUUID,
				WorkspaceID: wsUUID,
			},
		},
	}
	a := newScopeAuthorizer(q)
	ctx := context.Background()

	ok, err := a.AuthorizeScope(ctx, memberStr, wsStr, realtime.ScopeRun, runStr)
	if err != nil || !ok {
		t.Fatalf("member in workspace should be allowed: ok=%v err=%v", ok, err)
	}

	ok, err = a.AuthorizeScope(ctx, memberStr, otherWsStr, realtime.ScopeRun, runStr)
	if err != nil || ok {
		t.Fatalf("cross-workspace must be denied: ok=%v err=%v", ok, err)
	}
}

// failingScopeQuerier returns a non-ErrNoRows error from every lookup,
// simulating a transient database failure (pool exhaustion, cancelled
// context, network blip). Such errors must propagate out of AuthorizeScope
// so handleSubscribe reports "lookup_failed" rather than "forbidden".
type failingScopeQuerier struct{}

func (failingScopeQuerier) GetAgentRun(context.Context, pgtype.UUID) (db.AgentRun, error) {
	return db.AgentRun{}, errors.New("connection reset by peer")
}
func (failingScopeQuerier) GetTask(context.Context, pgtype.UUID) (db.Task, error) {
	return db.Task{}, errors.New("connection reset by peer")
}
func (failingScopeQuerier) GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error) {
	return db.ChatSession{}, errors.New("connection reset by peer")
}
func (failingScopeQuerier) IsChatSessionMemberParticipant(context.Context, db.IsChatSessionMemberParticipantParams) (bool, error) {
	return false, errors.New("connection reset by peer")
}

// errOnInnerQuerier succeeds for GetAgentTask (so the task path reaches its
// inner lookups) but fails GetTask / GetChatSession with a non-ErrNoRows
// error. This isolates the inner lookup points from the outer GetAgentTask.
type errOnInnerQuerier struct {
	agentRun db.AgentRun
}

func (q *errOnInnerQuerier) GetAgentRun(_ context.Context, _ pgtype.UUID) (db.AgentRun, error) {
	return q.agentRun, nil
}
func (*errOnInnerQuerier) GetTask(context.Context, pgtype.UUID) (db.Task, error) {
	return db.Task{}, errors.New("connection reset by peer")
}
func (*errOnInnerQuerier) GetChatSession(context.Context, pgtype.UUID) (db.ChatSession, error) {
	return db.ChatSession{}, errors.New("connection reset by peer")
}
func (*errOnInnerQuerier) IsChatSessionMemberParticipant(context.Context, db.IsChatSessionMemberParticipantParams) (bool, error) {
	return false, errors.New("connection reset by peer")
}

// TestScopeAuthorizer_DoesNotSwallowQueryErrors pins #6037: a real database
// error at any of the four lookup points must be returned to the caller as a
// non-nil error, not silently converted to a (false, nil) "forbidden" denial.
// Only a missing resource (pgx.ErrNoRows) stays a plain denial.
func TestScopeAuthorizer_DoesNotSwallowQueryErrors(t *testing.T) {
	wsStr, _ := mustUUID(t)
	userStr, _ := mustUUID(t)
	runStr, runUUID := mustUUID(t)
	_, taskUUID := mustUUID(t)
	_, sessUUID := mustUUID(t)
	chatScopeStr, _ := mustUUID(t)

	a := newScopeAuthorizer(failingScopeQuerier{})
	ctx := context.Background()

	// Point 1: GetAgentTask fails on the task path.
	if _, err := a.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeRun, runStr); err == nil {
		t.Fatalf("task-path GetAgentTask error must propagate, got err=nil")
	}
	// Point 4: GetChatSession fails on the chat path.
	if _, err := a.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeChat, chatScopeStr); err == nil {
		t.Fatalf("chat-path GetChatSession error must propagate, got err=nil")
	}

	// Point 2: GetTask fails (task resolves to an task task).
	taskAuth := newScopeAuthorizer(&errOnInnerQuerier{
		agentRun: db.AgentRun{ID: runUUID, TaskID: taskUUID},
	})
	if _, err := taskAuth.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeRun, runStr); err == nil {
		t.Fatalf("task-path GetTask error must propagate, got err=nil")
	}

	// Point 3: GetChatSession fails on the task path (task resolves to a chat task).
	chatRunAuth := newScopeAuthorizer(&errOnInnerQuerier{
		agentRun: db.AgentRun{ID: runUUID, ChatSessionID: sessUUID},
	})
	if _, err := chatRunAuth.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeRun, runStr); err == nil {
		t.Fatalf("task-path GetChatSession error must propagate, got err=nil")
	}
}

// TestScopeAuthorizer_MissingResourceIsPlainDenial pins the other half of
// #6037: a missing resource (pgx.ErrNoRows) must remain a (false, nil)
// denial — not-found is reported as "forbidden", matching the HTTP layer's
// 404-not-403 convention.
func TestScopeAuthorizer_MissingResourceIsPlainDenial(t *testing.T) {
	wsStr, _ := mustUUID(t)
	userStr, _ := mustUUID(t)
	missingRun, _ := mustUUID(t)
	missingChat, _ := mustUUID(t)

	a := newScopeAuthorizer(&fakeScopeQuerier{})
	ctx := context.Background()

	if ok, err := a.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeRun, missingRun); err != nil || ok {
		t.Fatalf("missing task must be a plain denial: ok=%v err=%v", ok, err)
	}
	if ok, err := a.AuthorizeScope(ctx, userStr, wsStr, realtime.ScopeChat, missingChat); err != nil || ok {
		t.Fatalf("missing chat session must be a plain denial: ok=%v err=%v", ok, err)
	}
}
