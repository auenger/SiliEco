package slack

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// ---- fakes ----

type fakeSlashQueries struct {
	inst      db.ChannelInstallation
	instErr   error
	binding   db.ChannelUserBinding
	bindErr   error
	memberErr error
	gotAppID  string
}

func (f *fakeSlashQueries) GetChannelInstallationByAppID(_ context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.gotAppID = arg.AppID
	return f.inst, f.instErr
}

func (f *fakeSlashQueries) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}

func (f *fakeSlashQueries) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memberErr
}

// fakeQuickCreate records the last EnqueueQuickCreateTask call so tests can
// assert the prompt is passed through verbatim and attributed correctly.
type fakeQuickCreate struct {
	agentRun  db.AgentRun
	err   error
	calls int

	workspaceID pgtype.UUID
	requesterID pgtype.UUID
	agentID     pgtype.UUID
	squadID     pgtype.UUID
	prompt      string
}

func (f *fakeQuickCreate) EnqueueQuickCreateRun(_ context.Context, workspaceID, requesterID, agentID, squadID pgtype.UUID, prompt, _, _ string, _, _, _, _ pgtype.UUID, _ []pgtype.UUID) (db.AgentRun, error) {
	f.calls++
	f.workspaceID = workspaceID
	f.requesterID = requesterID
	f.agentID = agentID
	f.squadID = squadID
	f.prompt = prompt
	return f.agentRun, f.err
}

func slashTestUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

// newTestSlashProcessor builds a processor over fakes and returns it plus a
// pointer to the last ephemeral reply text and the reply count.
func newTestSlashProcessor(q slashQueries, agentRuns quickCreateEnqueuer, binding bindingMinter) (*SlashCommandProcessor, *string, *int) {
	captured := new(string)
	count := new(int)
	p := &SlashCommandProcessor{
		q:           q,
		agentRuns:       agentRuns,
		binding:     binding,
		appURL:      "https://app.example",
		bindingPath: "/slack/bind",
		logger:      slog.Default(),
	}
	p.respond = func(_ context.Context, _ string, text string) error {
		*count++
		*captured = text
		return nil
	}
	return p, captured, count
}

func activeSlashInstallation() db.ChannelInstallation {
	return db.ChannelInstallation{
		ID:              slashTestUUID(1),
		WorkspaceID:     slashTestUUID(2),
		AgentID:         slashTestUUID(3),
		InstallerUserID: slashTestUUID(4),
		Status:          "active",
		Config:          []byte(`{"app_id":"A1","team_id":"T1"}`),
	}
}

func taskSlashCmd() slack.SlashCommand {
	return slack.SlashCommand{
		Command:     "/task",
		Text:        "Fix login",
		APIAppID:    "A1",
		TeamID:      "T1",
		UserID:      "U1",
		ChannelID:   "C1",
		ResponseURL: "https://hooks.slack.test/response",
	}
}

// ---- tests ----

func TestSlashHandle_EnqueuesQuickCreateAndAcks(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{SiliecoUserID: slashTestUUID(9)},
	}
	agentRuns := &fakeQuickCreate{}
	p, captured, count := newTestSlashProcessor(q, agentRuns, &fakeBindingMinter{})

	p.Handle(context.Background(), taskSlashCmd())

	if agentRuns.calls != 1 {
		t.Fatalf("expected 1 quick-create enqueue, got %d", agentRuns.calls)
	}
	if *count != 1 {
		t.Fatalf("expected 1 ephemeral reply, got %d", *count)
	}
	if *captured != slashQueuedText {
		t.Fatalf("expected queued ack, got %q", *captured)
	}
	if q.gotAppID != "A1" {
		t.Errorf("installation lookup used app id %q, want A1", q.gotAppID)
	}
	if agentRuns.prompt != "Fix login" {
		t.Errorf("quick-create prompt = %q, want Fix login", agentRuns.prompt)
	}
	if agentRuns.workspaceID != slashTestUUID(2) {
		t.Errorf("quick-create workspace is not the installation workspace")
	}
	if agentRuns.agentID != slashTestUUID(3) {
		t.Errorf("quick-create not dispatched to the installation agent")
	}
	if agentRuns.requesterID != slashTestUUID(9) {
		t.Errorf("quick-create requester is not the bound member")
	}
	if agentRuns.squadID.Valid {
		t.Errorf("slash-command quick-create must not carry a squad id")
	}
}

func TestSlashHandle_MultilinePromptPassedThrough(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{SiliecoUserID: slashTestUUID(9)},
	}
	agentRuns := &fakeQuickCreate{}
	p, _, _ := newTestSlashProcessor(q, agentRuns, &fakeBindingMinter{})

	cmd := taskSlashCmd()
	cmd.Text = "  Title\nline one\nline two  "
	p.Handle(context.Background(), cmd)

	// The whole (trimmed) natural-language text is the prompt — no title/body
	// split; the agent authors the well-formed task from it.
	if agentRuns.prompt != "Title\nline one\nline two" {
		t.Errorf("prompt = %q, want the full trimmed text", agentRuns.prompt)
	}
}

func TestSlashHandle_EmptyPromptIsUsage(t *testing.T) {
	agentRuns := &fakeQuickCreate{}
	p, captured, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, agentRuns, &fakeBindingMinter{})

	cmd := taskSlashCmd()
	cmd.Text = "   "
	p.Handle(context.Background(), cmd)

	if agentRuns.calls != 0 {
		t.Fatalf("empty prompt must not enqueue a task")
	}
	if *count != 1 || *captured != slashUsageText {
		t.Fatalf("expected usage reply, got %q", *captured)
	}
}

func TestSlashHandle_UnboundUserGetsLink(t *testing.T) {
	q := &fakeSlashQueries{inst: activeSlashInstallation(), bindErr: pgx.ErrNoRows}
	agentRuns := &fakeQuickCreate{}
	bind := &fakeBindingMinter{raw: "TOKEN123"}
	p, captured, _ := newTestSlashProcessor(q, agentRuns, bind)

	p.Handle(context.Background(), taskSlashCmd())

	if agentRuns.calls != 0 {
		t.Fatalf("unbound user must not enqueue a task")
	}
	if bind.calls != 1 {
		t.Fatalf("expected a binding token to be minted, got %d", bind.calls)
	}
	if !strings.Contains(*captured, "link your account") || !strings.Contains(*captured, "TOKEN123") {
		t.Fatalf("reply missing bind link: %q", *captured)
	}
}

func TestSlashHandle_NonMemberDropped(t *testing.T) {
	q := &fakeSlashQueries{
		inst:      activeSlashInstallation(),
		binding:   db.ChannelUserBinding{SiliecoUserID: slashTestUUID(9)},
		memberErr: pgx.ErrNoRows,
	}
	agentRuns := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(q, agentRuns, &fakeBindingMinter{})

	p.Handle(context.Background(), taskSlashCmd())

	if agentRuns.calls != 0 {
		t.Fatalf("non-member must not enqueue a task")
	}
	if *captured != slashNotMemberText {
		t.Fatalf("expected not-member reply, got %q", *captured)
	}
}

func TestSlashHandle_InactiveInstallation(t *testing.T) {
	inst := activeSlashInstallation()
	inst.Status = "revoked"
	agentRuns := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: inst}, agentRuns, &fakeBindingMinter{})

	p.Handle(context.Background(), taskSlashCmd())

	if agentRuns.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("inactive install: calls=%d reply=%q", agentRuns.calls, *captured)
	}
}

func TestSlashHandle_TeamMismatchTreatedAsDisconnected(t *testing.T) {
	agentRuns := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, agentRuns, &fakeBindingMinter{})

	cmd := taskSlashCmd()
	cmd.TeamID = "T2" // config team is T1
	p.Handle(context.Background(), cmd)

	if agentRuns.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("team mismatch: calls=%d reply=%q", agentRuns.calls, *captured)
	}
}

func TestSlashHandle_EnqueueFailureIsInternalError(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{SiliecoUserID: slashTestUUID(9)},
	}
	agentRuns := &fakeQuickCreate{err: errors.New("agent has no runtime")}
	p, captured, _ := newTestSlashProcessor(q, agentRuns, &fakeBindingMinter{})

	p.Handle(context.Background(), taskSlashCmd())

	if agentRuns.calls != 1 {
		t.Fatalf("expected the enqueue to be attempted once, got %d", agentRuns.calls)
	}
	if *captured != slashInternalErrorText {
		t.Fatalf("expected internal-error reply, got %q", *captured)
	}
}

func TestSlashHandle_IgnoresOtherCommands(t *testing.T) {
	agentRuns := &fakeQuickCreate{}
	p, _, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, agentRuns, &fakeBindingMinter{})

	cmd := taskSlashCmd()
	cmd.Command = "/other"
	p.Handle(context.Background(), cmd)

	if agentRuns.calls != 0 || *count != 0 {
		t.Fatalf("non-/task command must be ignored: calls=%d replies=%d", agentRuns.calls, *count)
	}
}
