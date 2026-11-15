package relational

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("requires initialized PostgreSQL at DATABASE_URL")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAllCleanupScopesLockWithoutMutation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	for operation, scope := range scopes {
		t.Run(string(operation), func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			count := 0
			for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(scope.predicate, -1) {
				n, _ := strconv.Atoi(m[1])
				if n > count {
					count = n
				}
			}
			args := make([]any, count)
			for i := range args {
				args[i] = "00000000-0000-0000-0000-000000000001"
			}
			if err := LockForDelete(ctx, tx, operation, args...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCleanupLocksParentBeforeRun(t *testing.T) {
	_, tables := lockTargets(scopes[DeleteWorkspace])
	positions := map[string]int{}
	for i, table := range tables {
		positions[table] = i
	}
	for _, parent := range []string{"task", "comment", "chat_session", "agent", "agent_runtime"} {
		if positions[parent] >= positions["agent_run"] {
			t.Fatalf("%s must precede agent_run: %v", parent, tables)
		}
	}
}

// Reproduces the lost-child window: cleanup starts while a new run message
// holds KEY SHARE on its run. The aggregate lock must wait, refresh its snapshot,
// and delete the message committed during that wait.
func TestDeleteTaskWaitsForDescendantWriter(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ws, actor, agent, task, run pgtype.UUID
	insert := func(query string, args ...any) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ws = insert(`INSERT INTO workspace(name,slug) VALUES ('Cleanup race',gen_random_uuid()::text) RETURNING id`)
	actor = insert(`INSERT INTO "user"(email,name) VALUES (gen_random_uuid()::text||'@cleanup-race.test','Race') RETURNING id`)
	agent = insert(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id) VALUES($1,'Race','local',$2) RETURNING id`, ws, actor)
	task = insert(`INSERT INTO task(workspace_id,title,creator_type,creator_id,number) VALUES($1,'Race','member',$2,1) RETURNING id`, ws, actor)
	run = insert(`INSERT INTO agent_run(agent_id,task_id,status,completed_at) VALUES($1,$2,'completed',now()) RETURNING id`, agent, task)
	defer func() {
		cleanupCtx := context.Background()
		tx, e := pool.Begin(cleanupCtx)
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(cleanupCtx)
		if e = LockForDelete(cleanupCtx, tx, DeleteWorkspace, ws); e == nil {
			e = db.New(tx).DeleteWorkspace(cleanupCtx, ws)
		}
		if e == nil {
			_, e = tx.Exec(cleanupCtx, `DELETE FROM "user" WHERE id=$1`, actor)
		}
		if e == nil {
			e = tx.Commit(cleanupCtx)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(ctx)
	if _, err = db.New(writer).CreateAgentRunMessage(ctx, db.CreateAgentRunMessageParams{RunID: run, Seq: 1, Type: "text", Content: pgtype.Text{String: "racing", Valid: true}}); err != nil {
		t.Fatal(err)
	}
	deleter, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Rollback(ctx)
	pid := deleter.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() { done <- LockForDelete(ctx, deleter, DeleteTask, task, ws) }()
	// Observe PostgreSQL's lock wait rather than relying on a scheduling sleep.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, pid).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("aggregate cleanup did not block on descendant writer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = db.New(deleter).DeleteTask(ctx, db.DeleteTaskParams{ID: task, WorkspaceID: ws}); err != nil {
		t.Fatal(err)
	}
	if err = deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM agent_run_message WHERE run_id=$1`, run).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan messages after deletion: %d", count)
	}
	if _, err = db.New(pool).CreateAgentRunMessage(ctx, db.CreateAgentRunMessageParams{RunID: run, Seq: 2, Type: "text"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted parent write: got %v, want ErrNoRows", err)
	}
}

func TestMissingParentGuardsAndRuntimeTenantScope(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	insert := func(query string, args ...any) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := tx.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ws := insert(`INSERT INTO workspace(name,slug) VALUES ('Guard',gen_random_uuid()::text) RETURNING id`)
	other := insert(`INSERT INTO workspace(name,slug) VALUES ('Other',gen_random_uuid()::text) RETURNING id`)
	actor := insert(`INSERT INTO "user"(email,name) VALUES (gen_random_uuid()::text||'@guards.test','Guard') RETURNING id`)
	runtime := insert(`INSERT INTO agent_runtime(workspace_id,daemon_id,name,runtime_mode,provider) VALUES($1,gen_random_uuid()::text,'Guard','local','claude') RETURNING id`, other)
	missing := insert(`SELECT gen_random_uuid()`)
	q := db.New(tx)
	if _, err = q.CreateAgentBuilder(ctx, db.CreateAgentBuilderParams{WorkspaceID: ws, RuntimeID: runtime, OwnerID: actor, Name: "Guard", RuntimeMode: "local", SystemKey: pgtype.Text{String: "agent_builder:guard", Valid: true}}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace runtime: %v", err)
	}
	task, err := q.CreateTask(ctx, db.CreateTaskParams{WorkspaceID: ws, Title: "Guard", Status: "todo", Priority: "none", CreatorType: "member", CreatorID: actor, Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.UpdateTask(ctx, db.UpdateTaskParams{ID: task.ID, ParentTaskID: missing}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing parent update: %v", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE task SET updated_at=now()-interval '1 day' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.CreateComment(ctx, db.CreateCommentParams{WorkspaceID: ws, TaskID: task.ID, AuthorType: "member", AuthorID: actor, Content: "Guard", Type: "comment", ParentID: missing}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing reply parent: %v", err)
	}
	var touched bool
	if err = tx.QueryRow(ctx, `SELECT updated_at>now()-interval '1 hour' FROM task WHERE id=$1`, task.ID).Scan(&touched); err != nil {
		t.Fatal(err)
	}
	if touched {
		t.Fatal("missing parent must not touch task updated_at")
	}
	if _, err = q.CreateAgentRun(ctx, db.CreateAgentRunParams{AgentID: missing, TaskID: task.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing agent: %v", err)
	}
}

// A comment touches Task.updated_at. It must acquire the workspace guard before
// that UPDATE, or workspace deletion and comment creation lock each other in
// reverse order (workspace -> task versus task -> workspace).
func TestWorkspaceDeleteAndCommentCreateLockOrder(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insert := func(query string, args ...any) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ws := insert(`INSERT INTO workspace(name,slug) VALUES ('Comment race',gen_random_uuid()::text) RETURNING id`)
	actor := insert(`INSERT INTO "user"(email,name) VALUES (gen_random_uuid()::text||'@comment-race.test','Race') RETURNING id`)
	task := insert(`INSERT INTO task(workspace_id,title,creator_type,creator_id,number) VALUES($1,'Race','member',$2,1) RETURNING id`, ws, actor)
	defer func() {
		cleanupCtx := context.Background()
		tx, e := pool.Begin(cleanupCtx)
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(cleanupCtx)
		if e = LockForDelete(cleanupCtx, tx, DeleteWorkspace, ws); e == nil {
			e = db.New(tx).DeleteWorkspace(cleanupCtx, ws)
		}
		if e == nil {
			_, e = tx.Exec(cleanupCtx, `DELETE FROM "user" WHERE id=$1`, actor)
		}
		if e == nil {
			e = tx.Commit(cleanupCtx)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	deleter, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Rollback(ctx)
	if _, err = deleter.Exec(ctx, `SELECT id FROM workspace WHERE id=$1 FOR UPDATE`, ws); err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(ctx)
	done := make(chan error, 1)
	go func() {
		_, e := db.New(writer).CreateComment(ctx, db.CreateCommentParams{WorkspaceID: ws, TaskID: task, AuthorType: "member", AuthorID: actor, Content: "Race", Type: "comment"})
		done <- e
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, writer.Conn().PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("comment writer did not wait for workspace")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = LockForDelete(ctx, deleter, DeleteWorkspace, ws); err != nil {
		t.Fatal(err)
	}
	if err = db.New(deleter).DeleteWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err = deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted workspace comment: got %v, want ErrNoRows", err)
	}
}

// Runtime profile deletion checks active rows before deleting archived rows.
// Both states must be locked: an active row cannot become an unprotected new
// deletion target between the usage check and cleanup's fresh snapshot.
func TestRuntimeCleanupFreezesActiveArchiveCandidates(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insert := func(query string, args ...any) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ws := insert(`INSERT INTO workspace(name,slug) VALUES ('Archive race',gen_random_uuid()::text) RETURNING id`)
	actor := insert(`INSERT INTO "user"(email,name) VALUES (gen_random_uuid()::text||'@archive-race.test','Race') RETURNING id`)
	runtime := insert(`INSERT INTO agent_runtime(workspace_id,daemon_id,name,runtime_mode,provider) VALUES($1,gen_random_uuid()::text,'Race','local','claude') RETURNING id`, ws)
	agent := insert(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id,runtime_id) VALUES($1,'Race','local',$2,$3) RETURNING id`, ws, actor, runtime)
	squad := insert(`INSERT INTO squad(workspace_id,name,leader_id,creator_id) VALUES($1,'Race',$2,$3) RETURNING id`, ws, agent, actor)
	defer func() {
		cleanupCtx := context.Background()
		tx, e := pool.Begin(cleanupCtx)
		if e != nil {
			t.Error(e)
			return
		}
		defer tx.Rollback(cleanupCtx)
		if e = LockForDelete(cleanupCtx, tx, DeleteWorkspace, ws); e == nil {
			e = db.New(tx).DeleteWorkspace(cleanupCtx, ws)
		}
		if e == nil {
			_, e = tx.Exec(cleanupCtx, `DELETE FROM "user" WHERE id=$1`, actor)
		}
		if e == nil {
			e = tx.Commit(cleanupCtx)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	for _, operation := range []Operation{DeleteArchivedAgentsByRuntime, DeleteSquadsByArchivedAgentsOnRuntime, DeleteAgentRuntime} {
		t.Run(string(operation), func(t *testing.T) {
			deleter, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer deleter.Rollback(ctx)
			if err = LockForDelete(ctx, deleter, operation, runtime); err != nil {
				t.Fatal(err)
			}
			targets := []struct {
				table string
				id    pgtype.UUID
			}{{"squad", squad}, {"agent", agent}}

			for _, target := range targets {
				writer, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = writer.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
					writer.Rollback(ctx)
					t.Fatal(err)
				}
				_, err = writer.Exec(ctx, `UPDATE `+target.table+` SET archived_at=now() WHERE id=$1`, target.id)
				writer.Rollback(ctx)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
					t.Fatalf("%s must block active %s archive, got %v", operation, target.table, err)
				}
			}
		})
	}
}
