package relational

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Operation identifies an aggregate deletion. Parameters have the same order
// as the corresponding sqlc Delete query. The locks and delete MUST share one
// read-committed transaction; holding locks on a pooled connection is insufficient.
type Operation string

const (
	DeleteAgentRuntime                    Operation = "DeleteAgentRuntime"
	DeleteAgentRuntimesByProfile          Operation = "DeleteAgentRuntimesByProfile"
	DeleteArchivedAgentsByRuntime         Operation = "DeleteArchivedAgentsByRuntime"
	DeleteAutopilotTrigger                Operation = "DeleteAutopilotTrigger"
	DeleteChatSession                     Operation = "DeleteChatSession"
	DeleteComment                         Operation = "DeleteComment"
	DeleteLabel                           Operation = "DeleteLabel"
	DeleteMember                          Operation = "DeleteMember"
	DeleteProject                         Operation = "DeleteProject"
	DeleteSkill                           Operation = "DeleteSkill"
	DeleteSquadsByArchivedAgentsOnRuntime Operation = "DeleteSquadsByArchivedAgentsOnRuntime"
	DeleteSystemAgentByID                 Operation = "DeleteSystemAgentByID"
	DeleteSystemAgentsByRuntime           Operation = "DeleteSystemAgentsByRuntime"
	DeleteTask                            Operation = "DeleteTask"
	DeleteUserChatMessageByRun            Operation = "DeleteUserChatMessageByRun"
	DeleteWorkspace                       Operation = "DeleteWorkspace"
)

type scope struct{ table, predicate string }

// Archive state is mutable. Lock all agents/squads in the immutable runtime
// scope, including active rows that could otherwise become new delete targets
// after a cleanup snapshot. The final SQL still deletes only archived rows.
var scopes = map[Operation]scope{
	DeleteAgentRuntime:                    {"agent_runtime", "id = $1"},
	DeleteAgentRuntimesByProfile:          {"agent_runtime", "profile_id = $1 AND workspace_id = $2"},
	DeleteArchivedAgentsByRuntime:         {"agent", "runtime_id = $1"},
	DeleteAutopilotTrigger:                {"autopilot_trigger", "id = $1"},
	DeleteChatSession:                     {"chat_session", "id = $1 AND workspace_id = $2"},
	DeleteComment:                         {"comment", "id = $1 AND workspace_id = $2"},
	DeleteLabel:                           {"task_label", "id = $1 AND workspace_id = $2"},
	DeleteMember:                          {"member", "id = $1"},
	DeleteProject:                         {"project", "id = $1 AND workspace_id = $2"},
	DeleteSkill:                           {"skill", "id = $1 AND workspace_id = $2"},
	DeleteSquadsByArchivedAgentsOnRuntime: {"squad", "leader_id IN (SELECT id FROM agent WHERE runtime_id = $1)"},
	DeleteSystemAgentByID:                 {"agent", "id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'"},
	DeleteSystemAgentsByRuntime:           {"agent", "runtime_id = $1 AND kind = 'system'"},
	DeleteTask:                            {"task", "task.id = $1 AND task.workspace_id = $2"},
	DeleteUserChatMessageByRun:            {"chat_message", "run_id = $1 AND role = 'user'"},
	DeleteWorkspace:                       {"workspace", "workspace.id = $1"},
}

// These are the 107 relationships from the final pre-unification schema.
// c = dependent deletion; n = null surviving references; a/r = restrict.
// They are application metadata, not database constraints or triggers.
type relation struct{ child, parent, columns, parentColumns, action string }

var relations = []relation{
	{"activity_log", "task", "task_id", "id", "c"},
	{"activity_log", "workspace", "workspace_id", "id", "c"},
	{"agent", "agent_runtime", "runtime_id", "id", "r"},
	{"agent", "user", "archived_by", "id", "a"},
	{"agent", "user", "owner_id", "id", "a"},
	{"agent", "workspace", "workspace_id", "id", "c"},
	{"agent_run", "agent", "agent_id", "id", "c"},
	{"agent_run", "agent_run", "parent_run_id", "id", "n"},
	{"agent_run", "agent_runtime", "runtime_id", "id", "c"},
	{"agent_run", "autopilot_run", "autopilot_run_id", "id", "n"},
	{"agent_run", "chat_session", "chat_session_id", "id", "n"},
	{"agent_run", "comment", "trigger_comment_id", "id", "n"},
	{"agent_run", "task", "task_id", "id", "c"},
	{"agent_run_message", "agent_run", "run_id", "id", "c"},
	{"agent_run_token", "agent", "agent_id", "id", "c"},
	{"agent_run_token", "agent_run", "run_id", "id", "c"},
	{"agent_run_token", "user", "user_id", "id", "c"},
	{"agent_run_token", "workspace", "workspace_id", "id", "c"},
	{"agent_run_usage", "agent_run", "run_id", "id", "c"},
	{"agent_runtime", "user", "owner_id", "id", "a"},
	{"agent_runtime", "workspace", "workspace_id", "id", "c"},
	{"agent_skill", "agent", "agent_id", "id", "c"},
	{"agent_skill", "skill", "skill_id", "id", "c"},
	{"attachment", "chat_message", "chat_message_id", "id", "c"},
	{"attachment", "chat_session", "chat_session_id", "id", "c"},
	{"attachment", "comment", "comment_id", "id", "c"},
	{"attachment", "task", "task_id", "id", "c"},
	{"attachment", "workspace", "workspace_id", "id", "c"},
	{"autopilot", "project", "project_id", "id", "n"},
	{"autopilot", "workspace", "workspace_id", "id", "c"},
	{"autopilot_run", "agent_run", "run_id", "id", "n"},
	{"autopilot_run", "autopilot", "autopilot_id", "id", "c"},
	{"autopilot_run", "autopilot_trigger", "trigger_id", "id", "n"},
	{"autopilot_run", "squad", "squad_id", "id", "n"},
	{"autopilot_run", "task", "task_id", "id", "n"},
	{"autopilot_trigger", "autopilot", "autopilot_id", "id", "c"},
	{"chat_message", "chat_session", "chat_session_id", "id", "c"},
	{"chat_session", "agent", "agent_id", "id", "c"},
	{"chat_session", "agent_runtime", "runtime_id", "id", "n"},
	{"chat_session", "user", "creator_id", "id", "c"},
	{"chat_session", "workspace", "workspace_id", "id", "c"},
	{"comment", "comment", "parent_id", "id", "c"},
	{"comment", "task", "task_id", "id", "c"},
	{"comment", "workspace", "workspace_id", "id", "c"},
	{"comment_reaction", "comment", "comment_id", "id", "c"},
	{"comment_reaction", "workspace", "workspace_id", "id", "c"},
	{"daemon_connection", "agent", "agent_id", "id", "c"},
	{"daemon_token", "workspace", "workspace_id", "id", "c"},
	{"feedback", "user", "user_id", "id", "c"},
	{"feedback", "workspace", "workspace_id", "id", "n"},
	{"github_installation", "user", "connected_by_id", "id", "n"},
	{"github_installation", "workspace", "workspace_id", "id", "c"},
	{"github_pull_request", "workspace", "workspace_id", "id", "c"},
	{"github_pull_request_check_suite", "github_pull_request", "pr_id", "id", "c"},
	{"inbox_item", "task", "task_id", "id", "c"},
	{"inbox_item", "workspace", "workspace_id", "id", "c"},
	{"lark_binding_token", "lark_installation", "installation_id", "id", "c"},
	{"lark_binding_token", "workspace", "workspace_id", "id", "c"},
	{"lark_chat_session_binding", "chat_session", "chat_session_id", "id", "c"},
	{"lark_chat_session_binding", "lark_installation", "installation_id", "id", "c"},
	{"lark_inbound_audit", "lark_installation", "installation_id", "id", "n"},
	{"lark_inbound_message_dedup", "lark_installation", "installation_id", "id", "c"},
	{"lark_installation", "agent", "agent_id", "id", "c"},
	{"lark_installation", "user", "installer_user_id", "id", "r"},
	{"lark_installation", "workspace", "workspace_id", "id", "c"},
	{"lark_outbound_card_message", "agent_run", "run_id", "id", "n"},
	{"lark_outbound_card_message", "chat_session", "chat_session_id", "id", "c"},
	{"lark_user_binding", "lark_installation", "installation_id,workspace_id", "id,workspace_id", "c"},
	{"lark_user_binding", "member", "workspace_id,silieco_user_id", "workspace_id,user_id", "c"},
	{"member", "user", "user_id", "id", "c"},
	{"member", "workspace", "workspace_id", "id", "c"},
	{"notification_preference", "user", "user_id", "id", "c"},
	{"notification_preference", "workspace", "workspace_id", "id", "c"},
	{"personal_access_token", "user", "user_id", "id", "c"},
	{"pinned_item", "user", "user_id", "id", "c"},
	{"pinned_item", "workspace", "workspace_id", "id", "c"},
	{"project", "workspace", "workspace_id", "id", "c"},
	{"project_resource", "project", "project_id", "id", "c"},
	{"project_resource", "workspace", "workspace_id", "id", "c"},
	{"skill", "user", "created_by", "id", "a"},
	{"skill", "workspace", "workspace_id", "id", "c"},
	{"skill_file", "skill", "skill_id", "id", "c"},
	{"squad", "agent", "leader_id", "id", "r"},
	{"squad", "workspace", "workspace_id", "id", "c"},
	{"squad_member", "squad", "squad_id", "id", "c"},
	{"task", "project", "project_id", "id", "n"},
	{"task", "task", "parent_task_id", "id", "n"},
	{"task", "workspace", "workspace_id", "id", "c"},
	{"task_dependency", "task", "depends_on_task_id", "id", "c"},
	{"task_dependency", "task", "task_id", "id", "c"},
	{"task_label", "workspace", "workspace_id", "id", "c"},
	{"task_pull_request", "github_pull_request", "pull_request_id", "id", "c"},
	{"task_pull_request", "task", "task_id", "id", "c"},
	{"task_reaction", "task", "task_id", "id", "c"},
	{"task_reaction", "workspace", "workspace_id", "id", "c"},
	{"task_subscriber", "task", "task_id", "id", "c"},
	{"task_to_label", "task", "task_id", "id", "c"},
	{"task_to_label", "task_label", "label_id", "id", "c"},
	{"webhook_delivery", "autopilot", "autopilot_id", "id", "c"},
	{"webhook_delivery", "autopilot_run", "autopilot_run_id", "id", "n"},
	{"webhook_delivery", "autopilot_trigger", "trigger_id", "id", "c"},
	{"webhook_delivery", "webhook_delivery", "replayed_from_delivery_id", "id", "n"},
	{"webhook_delivery", "workspace", "workspace_id", "id", "c"},
	{"workflow_instance", "user", "archived_by", "id", "a"},
	{"workspace_invitation", "user", "invitee_user_id", "id", "a"},
	{"workspace_invitation", "user", "inviter_id", "id", "a"},
	{"workspace_invitation", "workspace", "workspace_id", "id", "c"},
}

func ident(name string) string { return `"` + name + `"` }
func relationCondition(r relation) string {
	children, parents := strings.Split(r.columns, ","), strings.Split(r.parentColumns, ",")
	clauses := make([]string, len(children))
	for i, child := range children {
		clauses[i] = "child." + ident(child) + " = parent." + ident(parents[i])
	}
	return strings.Join(clauses, " AND ")
}

// LockForDelete freezes every parent row that can accept a new dependent during
// aggregate cleanup. Each lock SELECT is its own statement, so a child committed
// while a prior lock was waiting becomes visible to the next snapshot. Repeat
// until the closure is stable; only then may the caller run dependent cleanup.
// Restrict/no-action children participate in locking (not in deletion), so
// archive/restore cannot change a runtime's restriction set during checks.
// The caller retains its domain-specific restriction/authorization checks.
func LockForDelete(ctx context.Context, tx pgx.Tx, operation Operation, args ...any) error {
	root, ok := scopes[operation]
	if !ok {
		return fmt.Errorf("unknown cleanup operation %q", operation)
	}
	var isolation string
	if err := tx.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != "read committed" {
		return fmt.Errorf("%s cleanup requires read committed isolation, got %s", operation, isolation)
	}
	locked := make(map[string]struct{})
	lock := func(table, sql string) (int, error) {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return 0, fmt.Errorf("%s lock %s: %w", operation, table, err)
		}
		defer rows.Close()
		added := 0
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return 0, err
			}
			key := table + ":" + id
			if _, exists := locked[key]; !exists {
				locked[key] = struct{}{}
				added++
			}
		}
		return added, rows.Err()
	}
	// Group deletion predicates include mutable archive state and runtime
	// membership. Freeze their containing runtime/profile before finding roots;
	// guarded creation/rebinding cannot add a new root after this barrier.
	switch operation {
	case DeleteArchivedAgentsByRuntime, DeleteSystemAgentsByRuntime, DeleteSquadsByArchivedAgentsOnRuntime:
		if _, err := lock("agent_runtime", "SELECT id::text FROM agent_runtime WHERE id=$1 FOR UPDATE"); err != nil {
			return err
		}
		if operation == DeleteSquadsByArchivedAgentsOnRuntime {
			// New squads/rebindings lock their leader, not the runtime directly.
			// Freeze every eligible leader before enumerating the squad root set.
			if _, err := lock("agent", "SELECT id::text FROM agent WHERE runtime_id=$1 ORDER BY id FOR UPDATE"); err != nil {
				return err
			}
		}
	case DeleteAgentRuntimesByProfile:
		if _, err := lock("runtime_profile", "SELECT id::text FROM runtime_profile WHERE id=$1 AND workspace_id=$2 FOR UPDATE"); err != nil {
			return err
		}
	}
	// The root is locked before any dependent snapshot is taken.
	if _, err := lock(root.table, "SELECT "+root.table+".id::text FROM "+ident(root.table)+" "+root.table+" WHERE "+root.predicate+" ORDER BY "+root.table+".id FOR UPDATE"); err != nil {
		return err
	}
	ctes, tables := lockTargets(root)
	for {
		added := 0
		for _, table := range tables {
			n, err := lock(table, ctes+" SELECT locked.id::text FROM "+ident(table)+" locked WHERE locked.id IN (SELECT id FROM rel_target_"+table+") ORDER BY locked.id FOR UPDATE")
			if err != nil {
				return err
			}
			added += n
		}
		if added == 0 {
			return nil
		}
	}
}

func lockTargets(root scope) (string, []string) {
	parents := make(map[string]bool)
	for _, r := range relations {
		parents[r.parent] = true
	}
	reached := map[string]bool{root.table: true}
	for changed := true; changed; {
		changed = false
		for _, r := range relations {
			if r.action != "n" && reached[r.parent] && !reached[r.child] && parents[r.child] {
				reached[r.child] = true
				changed = true
			}
		}
	}
	tables := make([]string, 0, len(reached))
	for table := range reached {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	tables = parentFirst(tables)
	ctes := make([]string, 0, len(tables))
	for _, table := range tables {
		var query string
		if table == root.table {
			query = "SELECT " + table + ".* FROM " + ident(table) + " " + table + " WHERE " + root.predicate
		} else {
			conditions := []string{}
			for _, r := range relations {
				if r.action != "n" && r.child == table && r.parent != table && reached[r.parent] {
					conditions = append(conditions, "EXISTS (SELECT 1 FROM rel_target_"+r.parent+" parent WHERE "+relationCondition(r)+")")
				}
			}
			query = "SELECT child.* FROM " + ident(table) + " child WHERE " + strings.Join(conditions, " OR ")
		}
		for _, r := range relations {
			if r.action == "c" && r.child == table && r.parent == table {
				query += " UNION SELECT child.* FROM " + ident(table) + " child JOIN rel_target_" + table + " parent ON " + relationCondition(r)
			}
		}
		ctes = append(ctes, "rel_target_"+table+" AS MATERIALIZED ("+query+")")
	}
	return "WITH RECURSIVE " + strings.Join(ctes, ", "), tables
}

// Keep aggregate cleanup compatible with the send/finalize lock order. Only
// dependent/restrict edges and these acyclic ordering edges participate; mutual SET NULL
// references between runs and autopilot history must not introduce cycles.
func parentFirst(tables []string) []string {
	dependencies := map[string]map[string]bool{}
	for _, r := range relations {
		if r.action != "n" && r.parent != r.child {
			if dependencies[r.child] == nil {
				dependencies[r.child] = map[string]bool{}
			}
			dependencies[r.child][r.parent] = true
		}
	}
	for _, edge := range [][2]string{{"agent_runtime", "agent"}, {"project", "task"}, {"chat_session", "agent_run"}, {"comment", "agent_run"}, {"autopilot_run", "agent_run"}} {
		if dependencies[edge[1]] == nil {
			dependencies[edge[1]] = map[string]bool{}
		}
		dependencies[edge[1]][edge[0]] = true
	}
	pending := map[string]bool{}
	for _, table := range tables {
		pending[table] = true
	}
	ordered := make([]string, 0, len(tables))
	for len(pending) > 0 {
		chosen := ""
		for _, table := range tables {
			if !pending[table] {
				continue
			}
			ready := true
			for parent := range dependencies[table] {
				if pending[parent] {
					ready = false
					break
				}
			}
			if ready {
				chosen = table
				break
			}
		}
		if chosen == "" {
			panic("cyclic aggregate cleanup lock order")
		}
		ordered = append(ordered, chosen)
		delete(pending, chosen)
	}
	return ordered
}
