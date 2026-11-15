package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/migrations"
)

func TestBaselineRejectsOldSchemaWithoutResettingIt(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	schema := fmt.Sprintf("baseline_guard_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE")
	if _, err := conn.Exec(ctx, "SET search_path TO "+ident); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "SET search_path TO public")
	table := ident + ".schema_migrations"
	if _, err := conn.Exec(ctx, "CREATE TABLE "+table+" (version text PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := validateBaseline(ctx, conn, table); err != nil {
		t.Fatalf("empty schema rejected: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE TABLE old_test_data (id int)"); err != nil {
		t.Fatal(err)
	}
	if err := validateBaseline(ctx, conn, table); err == nil || !strings.Contains(err.Error(), "empty database") {
		t.Fatalf("expected nonempty schema rejection, got %v", err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES ('001_init')"); err != nil {
		t.Fatal(err)
	}
	if err := validateBaseline(ctx, conn, table); err == nil || !strings.Contains(err.Error(), "incompatible schema") {
		t.Fatalf("expected generation rejection, got %v", err)
	}
	var present bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('old_test_data') IS NOT NULL").Scan(&present); err != nil || !present {
		t.Fatalf("guard removed existing data: %v", err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM "+table); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES ($1)", migrations.BaselineVersion); err != nil {
		t.Fatal(err)
	}
	if err := validateBaseline(ctx, conn, table); err != nil {
		t.Fatalf("current generation rejected: %v", err)
	}
}
