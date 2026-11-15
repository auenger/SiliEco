package migrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBaselineAttributionConstraint(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(realMigrationsDir(t), BaselineVersion+".up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	constraint := regexp.MustCompile(`(?m)^\s*(CONSTRAINT agent_run_accountable_matches_originator CHECK .*)[,]$`).FindStringSubmatch(string(contents))
	if len(constraint) != 2 {
		t.Fatal("missing strict attribution constraint in baseline")
	}
	if strings.Contains(constraint[1], "originator_source") {
		t.Fatal("attribution constraint must not exempt unknown origin sources")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("integration test requires DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, `CREATE TEMP TABLE attribution_baseline_test (originator_user_id uuid, accountable_user_id uuid, `+constraint[1]+`)`)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, `DROP TABLE attribution_baseline_test`)
	for _, pair := range [][2]string{{"00000000-0000-0000-0000-000000000001", ""}, {"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"}} {
		_, err = conn.Exec(ctx, `INSERT INTO attribution_baseline_test VALUES ($1::uuid,NULLIF($2,'')::uuid)`, pair[0], pair[1])
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("invalid attribution accepted: %v", err)
		}
	}
	_, err = conn.Exec(ctx, `INSERT INTO attribution_baseline_test VALUES (NULL,NULL), ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000001')`)
	if err != nil {
		t.Fatalf("valid attribution rejected: %v", err)
	}
}
