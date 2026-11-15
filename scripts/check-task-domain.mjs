#!/usr/bin/env node
// Keep the business Task / execution AgentRun contract from drifting back.
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const roots = ["apps/web", "apps/desktop", "apps/mobile", "packages", "core/cmd", "core/internal", "core/pkg", "core/migrations", "e2e"];
const skippedDirectories = new Set(["node_modules", ".next", ".expo", ".turbo", "dist", "build", "out", "coverage", "ios", "android", "archive", "archives"]);
const extensions = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".go", ".sql", ".json"]);

// Only these exact old wire literals are needed to assert their rejection.
// Identifiers and every other string in the same tests remain checked.
const negativeLiterals = new Map([
  ["core/pkg/protocol/identity_test.go", new Set(["issue_id"])],
  ["core/cmd/server/task_run_contract_test.go", new Set(["/api/issues/"])],
  ["packages/core/tasks/stores/draft-store.test.ts", new Set(["silieco_issue_draft:acme"])],
]);

function oldIdentifier(value) {
  return /^(?:agent_task_(?:queue|message|token|usage)|parent_issue_id|source_issue_id|max_concurrent_tasks|max-concurrent-tasks|SILIECO_DAEMON_MAX_CONCURRENT_TASKS|running_task_count)$/.test(value)
    || /^(?:AgentTask|agentTask)(?:[A-Z_]|$)|(?:Issue|Issues)(?=[A-Z\d_]|$)|^issue(?:[A-Z_]|$)|^issues[A-Z_]/.test(value);
}

function oldLiteral(value) {
  // Human prose, GitHub issue links, and Zod's error.issues are intentionally valid.
  return /(?:^|\W)(?:AgentTask\w*|\w+(?:Issue|Issues)(?:[A-Z]\w*)?)(?:$|\W)/.test(value)
    || /(?:^|\W)(?:issue_id|parent_issue_id|source_issue_id|task_completed|task_failed|issue_prefix|issue_status|issue_count|issues_count|agent_task_(?:queue|message|token|usage)|max_concurrent_tasks|max-concurrent-tasks|SILIECO_DAEMON_MAX_CONCURRENT_TASKS|running_task_count|SILIECO_TASK_ID|X-Task-ID)(?:$|\W)/.test(value)
    || /(?:@silieco\/(?:core|views)\/(?:[\w-]+\/)*|(?:^|["'\s])\.\.?\/(?:[\w.-]+\/)*)(?:issues?(?:[\/.-]|$))/.test(value)
    || /(?:\/api\/issues(?:\/|$)|mention:\/\/issue(?:\/|$)|(?:^|[\s"'])issue:[a-z_]+\b)/.test(value)
    || /(?:^|[\s"'])task:(?:queued|dispatch|running|waiting_local_directory|progress|completed|failed|message|cancelled)\b/.test(value)
    || /\/(?:agent-task-snapshot|pending-tasks?|active-tasks)(?:[/?"']|$)/.test(value)
    || /\b(?:FROM|JOIN|INTO|UPDATE|TABLE)\s+agent_task\b/i.test(value)
    || /silieco_issue_draft:/.test(value);
}

function inspectSource(file, source) {
  const violations = [];
  const report = (offset, message) => violations.push({ file, line: source.slice(0, offset).split("\n").length, message });
  if (/(?:^|[/_-])issues?(?:[/_.-]|$)/.test(file)) report(0, "retired entity file or directory name");
  // The scanner skips comments, preserving external issue references there.
  // Go raw strings and SQL quoted values are also recognized as string tokens.
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, true, ts.LanguageVariant.Standard, source);
  for (let token = scanner.scan(); token !== ts.SyntaxKind.EndOfFileToken; token = scanner.scan()) {
    if (token === ts.SyntaxKind.Identifier) {
      const value = scanner.getTokenText();
      if (oldIdentifier(value)) report(scanner.getTokenPos(), `retired identifier ${value}`);
    } else if ([ts.SyntaxKind.StringLiteral, ts.SyntaxKind.NoSubstitutionTemplateLiteral, ts.SyntaxKind.TemplateHead, ts.SyntaxKind.TemplateMiddle, ts.SyntaxKind.TemplateTail].includes(token)) {
      const value = scanner.getTokenValue();
      if (!negativeLiterals.get(file)?.has(value) && oldLiteral(value)) report(scanner.getTokenPos(), "retired entity path, event, field, or identifier in string");
    }
  }
  return violations;
}

function inspectJSON(file, source) {
  const json = ts.parseJsonText(file, source);
  const violations = json.parseDiagnostics.map(diagnostic => ({ file, line: json.getLineAndCharacterOfPosition(diagnostic.start ?? 0).line + 1, message: ts.flattenDiagnosticMessageText(diagnostic.messageText, " ") }));
  function visit(node) {
    if (ts.isObjectLiteralExpression(node)) {
      const keys = new Set();
      for (const property of node.properties) {
        if (!ts.isPropertyAssignment(property)) continue;
        const key = property.name.text;
        if (keys.has(key)) violations.push({ file, line: json.getLineAndCharacterOfPosition(property.name.getStart(json)).line + 1, message: `duplicate JSON key ${JSON.stringify(key)}` });
        keys.add(key);
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(json);
  return violations;
}

async function filesUnder(directory) {
  const files = [];
  for (const entry of await readdir(path.join(root, directory), { withFileTypes: true })) {
    if (entry.isSymbolicLink()) continue;
    const relative = `${directory}/${entry.name}`;
    if (entry.isDirectory() && !skippedDirectories.has(entry.name)) files.push(...await filesUnder(relative));
    else if (entry.isFile() && extensions.has(path.extname(entry.name))) files.push(relative);
  }
  return files;
}

function selfTest() {
  assert.equal(inspectSource("feature.ts", 'const task = response.task; const run = response.run; const details = error.issues; // GitHub Issue #1\nconst url = "https://github.com/org/repo/issues/1";').length, 0);
  for (const source of ['type AgentTask = {};', 'const getIssue = () => {};', 'const issue_id = "a";', 'import { Task } from "@silieco/core/issues";', 'const event = "task:completed";', 'const field = "issue_id";', 'const route = "/api/issues/x";', 'const field = `json:"issue_id"`;']) assert.ok(inspectSource("feature.ts", source).length, source);
  assert.equal(inspectSource("feature.ts", 'const event = "workflow_task:changed"; const token = { IssuedAt: 1 };').length, 0);
  assert.ok(inspectSource("feature.go", 'flags.Int("max-concurrent-tasks", 1, "")').length);
  assert.ok(inspectSource("feature.go", 'env("SILIECO_DAEMON_MAX_CONCURRENT_TASKS")').length);
  assert.equal(inspectSource("feature.go", 'flags.Int("max-concurrent-runs", 1, "")').length, 0);
  assert.equal(inspectSource("core/pkg/protocol/identity_test.go", 'decoded["issue_id"]').length, 0);
  assert.equal(inspectSource("core/pkg/protocol/identity_test.go", 'decoded["parent_issue_id"]').length, 1);
  assert.equal(inspectJSON("locale.json", '{"count":1,"nested":{"count":2}}').length, 0);
  assert.equal(inspectJSON("locale.json", '{"tasks_count":1,"tasks_count":2}').length, 1);
  assert.equal(inspectJSON("locale.json", '{"nested":{"count":1,"co\\u0075nt":2}}').length, 1);
  assert.ok(inspectJSON("locale.json", '{"broken":}').length);
  console.log("Task domain checker self-test passed.");
}

if (process.argv.includes("--self-test")) selfTest();
else {
  const files = (await Promise.all(roots.map(filesUnder))).flat().sort();
  const violations = [];
  for (const file of files) {
    const source = await readFile(path.join(root, file), "utf8");
    violations.push(...inspectSource(file, source));
    if (file.endsWith(".json")) violations.push(...inspectJSON(file, source));
  }
  for (const violation of violations) console.error(`${violation.file}:${violation.line}: ${violation.message}`);
  if (violations.length) {
    console.error(`Task domain check failed: ${violations.length} violation(s).`);
    process.exitCode = 1;
  } else console.log(`Task domain check passed (${files.length} source files; JSON keys checked per object).`);
}
