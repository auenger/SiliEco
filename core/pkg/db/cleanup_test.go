package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestTaskAndWorkspaceCleanupWithoutForeignKeys(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("requires initialized PostgreSQL at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id := func(sql string, args ...any) pgtype.UUID {
		t.Helper()
		var value pgtype.UUID
		if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	actor := id(`INSERT INTO "user" (email,name) VALUES (gen_random_uuid()::text || '@cleanup.test','Cleanup') RETURNING id`)
	ws := id(`INSERT INTO workspace(name,slug) VALUES ('Cleanup',gen_random_uuid()::text) RETURNING id`)
	other := id(`INSERT INTO workspace(name,slug) VALUES ('Other',gen_random_uuid()::text) RETURNING id`)
	agent := id(`INSERT INTO agent(workspace_id,name,runtime_mode,owner_id) VALUES ($1,'Cleanup','local',$2) RETURNING id`, ws, actor)
	task := id(`INSERT INTO task(workspace_id,title,creator_type,creator_id,number) VALUES ($1,'Parent','member',$2,1) RETURNING id`, ws, actor)
	child := id(`INSERT INTO task(workspace_id,title,creator_type,creator_id,number,parent_task_id) VALUES ($1,'Child','member',$2,2,$3) RETURNING id`, ws, actor, task)
	comment := id(`INSERT INTO comment(workspace_id,task_id,author_type,author_id,content) VALUES ($1,$2,'member',$3,'Parent comment') RETURNING id`, ws, task, actor)
	reply := id(`INSERT INTO comment(workspace_id,task_id,author_type,author_id,content,parent_id) VALUES ($1,$2,'member',$3,'Reply',$4) RETURNING id`, ws, task, actor, comment)
	exec(`INSERT INTO comment_reaction(workspace_id,comment_id,actor_type,actor_id,emoji) VALUES ($1,$2,'member',$3,'ok')`, ws, reply, actor)
	run := id(`INSERT INTO agent_run(agent_id,task_id,status,completed_at) VALUES ($1,$2,'completed',now()) RETURNING id`, agent, task)
	childRun := id(`INSERT INTO agent_run(agent_id,task_id,status,parent_run_id,trigger_comment_id,completed_at) VALUES ($1,$2,'completed',$3,$4,now()) RETURNING id`, agent, child, run, reply)
	exec(`INSERT INTO agent_run_message(run_id,seq,type,content) VALUES ($1,1,'text','finished')`, run)
	exec(`INSERT INTO agent_run_usage(run_id,model) VALUES ($1,'test')`, run)
	exec(`INSERT INTO agent_run_token(run_id,agent_id,workspace_id,user_id,token_hash,expires_at) VALUES ($1,$2,$3,$4,gen_random_uuid()::text,now()+interval '1 hour')`, run, agent, ws, actor)
	q := db.New(tx)
	if err := q.DeleteTask(ctx, db.DeleteTaskParams{ID: task, WorkspaceID: other}); err != nil {
		t.Fatal(err)
	}
	assertCount(t, ctx, tx, `SELECT count(*) FROM agent_run WHERE id=$1`, run, 1)
	if err := q.DeleteTask(ctx, db.DeleteTaskParams{ID: task, WorkspaceID: ws}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"agent_run_message", "agent_run_usage", "agent_run_token"} {
		assertCount(t, ctx, tx, "SELECT count(*) FROM "+table+" WHERE run_id=$1", run, 0)
	}
	assertCount(t, ctx, tx, `SELECT count(*) FROM comment WHERE task_id=$1`, task, 0)
	assertCount(t, ctx, tx, `SELECT count(*) FROM comment_reaction WHERE comment_id=$1`, reply, 0)
	assertCount(t, ctx, tx, `SELECT count(*) FROM task WHERE id=$1 AND parent_task_id IS NULL`, child, 1)
	assertCount(t, ctx, tx, `SELECT count(*) FROM agent_run WHERE id=$1 AND parent_run_id IS NULL AND trigger_comment_id IS NULL`, childRun, 1)
	if err := q.DeleteWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	assertCount(t, ctx, tx, `SELECT count(*) FROM task WHERE workspace_id=$1`, ws, 0)
	assertCount(t, ctx, tx, `SELECT count(*) FROM agent_run WHERE id=$1`, childRun, 0)
	assertCount(t, ctx, tx, `SELECT count(*) FROM agent WHERE workspace_id=$1`, ws, 0)
	assertCount(t, ctx, tx, `SELECT count(*) FROM workspace WHERE id=$1`, other, 1)
	assertCount(t, ctx, tx, `SELECT count(*) FROM "user" WHERE id=$1`, actor, 1)
}

func assertCount(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, id pgtype.UUID, want int) {
	t.Helper()
	var got int
	if err := tx.QueryRow(ctx, sql, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s: got %d, want %d", sql, got, want)
	}
}
