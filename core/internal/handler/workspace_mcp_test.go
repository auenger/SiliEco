package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestRestrictMCPServersForStage(t *testing.T) {
	first := parseUUID("11111111-1111-1111-1111-111111111111")
	second := parseUUID("22222222-2222-2222-2222-222222222222")
	unassigned := parseUUID("33333333-3333-3333-3333-333333333333")
	rows := []db.WorkspaceMcpServer{{ID: first, Name: "first"}, {ID: second, Name: "second"}}

	t.Run("nil stage allowlist inherits agent assignments", func(t *testing.T) {
		got, err := restrictMCPServersForStage(rows, nil, false)
		if err != nil || len(got) != 2 {
			t.Fatalf("got %d servers, err %v", len(got), err)
		}
	})

	t.Run("empty stage allowlist denies all", func(t *testing.T) {
		got, err := restrictMCPServersForStage(rows, nil, true)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %d servers, err %v", len(got), err)
		}
	})

	t.Run("stage can narrow agent assignments", func(t *testing.T) {
		got, err := restrictMCPServersForStage(rows, []pgtype.UUID{second}, true)
		if err != nil || len(got) != 1 || got[0].Name != "second" {
			t.Fatalf("got %#v, err %v", got, err)
		}
	})

	t.Run("stage cannot expand agent assignments", func(t *testing.T) {
		if _, err := restrictMCPServersForStage(rows, []pgtype.UUID{unassigned}, true); err == nil {
			t.Fatal("expected an unassigned MCP server to be rejected")
		}
	})
}

func TestValidateWorkspaceMCPRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		wantErr bool
	}{
		{name: "stdio", config: `{"command":"npx","args":["server"]}`},
		{name: "http", config: `{"url":"https://mcp.example.test"}`},
		{name: "empty", config: `{}`, wantErr: true},
		{name: "blank command", config: `{"command":" "}`, wantErr: true},
		{name: "array", config: `[]`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateWorkspaceMCPRequest(workspaceMCPServerRequest{Name: "server", Config: json.RawMessage(tc.config)})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
