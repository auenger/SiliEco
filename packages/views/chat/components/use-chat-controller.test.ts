import { describe, it, expect } from "vitest";
import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import { chatKeys } from "@silieco/core/chat/queries";
import type { ChatMessage, ChatMessagesPage, ChatPendingRun } from "@silieco/core/types";
import {
  hasInFlightPendingRun,
  isStillOnComposeTarget,
  planProjectContextChange,
} from "./use-chat-controller";

// hasInFlightPendingRun is the discriminator the stale-session self-heal uses
// to EXEMPT a just-created session (awaiting the list refetch) from being
// dropped as dangling. Post-SILI-5181 the send is await-then-render, so the
// exemption keys purely on the pending run `handleSend` seeds from the send
// response — NOT on "has cached messages": a session deleted elsewhere can still
// hold real cached history and must remain eligible for self-heal.
const sid = "session-1";

function msg(id: string): ChatMessage {
  return {
    id,
    chat_session_id: sid,
    role: "user",
    content: "hi",
    run_id: null,
    created_at: "2026-07-08T00:00:00Z",
  };
}

describe("hasInFlightPendingRun", () => {
  it("is false with an empty cache", () => {
    expect(hasInFlightPendingRun(new QueryClient(), sid)).toBe(false);
  });

  it("is false when only real cached history exists (no in-flight run)", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(sid), [msg("real-1"), msg("real-2")]);
    qc.setQueryData<InfiniteData<ChatMessagesPage>>(chatKeys.messagesPage(sid), {
      pages: [{ messages: [msg("real-1")], limit: 50, has_more: false, next_cursor: null }],
      pageParams: [null],
    });
    // A completed session sets pendingRun to {} (no run_id) — cached history
    // alone must not exempt it from the self-heal.
    qc.setQueryData<ChatPendingRun>(chatKeys.pendingRun(sid), {} as ChatPendingRun);
    expect(hasInFlightPendingRun(qc, sid)).toBe(false);
  });

  it("is true while a pending run is in flight (run_id set)", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatPendingRun>(chatKeys.pendingRun(sid), {
      run_id: "11111111-1111-4111-8111-111111111111",
      status: "queued",
      created_at: "2026-07-08T00:00:00Z",
    });
    expect(hasInFlightPendingRun(qc, sid)).toBe(true);
  });
});

// The post-send "scrub the composer?" rule, shared by BOTH send chains (the
// chat tab's controller and the floating ChatWindow) so they cannot drift.
// SILI-4864: the new-chat composer is one box per workspace, so the selected
// agent is NOT part of compose-target identity — only the session is.
describe("isStillOnComposeTarget", () => {
  it("is true when the user never left the session they sent from", () => {
    expect(isStillOnComposeTarget(sid, sid)).toBe(true);
  });

  it("is true for a new chat the user is still sitting in", () => {
    // Both null: ensureSession creates the row but does not publish it as
    // active, so a user who stayed put is still looking at the new-chat box.
    expect(isStillOnComposeTarget(null, null)).toBe(true);
  });

  it("is false once the user opens a different session mid-send", () => {
    expect(isStillOnComposeTarget("session-2", sid)).toBe(false);
  });

  it("is false when the user starts a new chat mid-send from a session", () => {
    expect(isStillOnComposeTarget(null, sid)).toBe(false);
  });
});

// The project-switch decision, shared by BOTH chat surfaces (the chat tab's
// controller and the floating ChatWindow) so the stale-agent rule cannot drift.
// Regression (SILI-5150 review): switching an existing session to another
// project opens a fresh chat, and that chat MUST bind to the open session's
// agent — not the stored `selectedAgentId`, which can be a stale preference for
// a different agent (open session belongs to agent B while the persisted pick
// is still agent A). Without pinning, the lazily-created session and its first
// send would land on agent A.
describe("planProjectContextChange", () => {
  const sessionB = { id: "sB", agent_id: "agent-b" };

  it.each(["project-x", null])("keeps group identity and members when changing project to %s", (projectId) => {
    expect(planProjectContextChange({
      targetProjectId: projectId,
      activeSessionId: "group-session",
      currentSession: { id: "group-session", agent_id: "", kind: "group" },
    })).toEqual({ kind: "updateGroup", sessionId: "group-session", projectId });
  });

  it("waits when an active session id is set but its row has not loaded yet", () => {
    expect(
      planProjectContextChange({
        targetProjectId: "project-x",
        activeSessionId: "sB",
        currentSession: null,
      }),
    ).toEqual({ kind: "awaitSession" });
  });

  it("detaches in place when the current session's project is removed", () => {
    expect(
      planProjectContextChange({
        targetProjectId: null,
        activeSessionId: "sB",
        currentSession: sessionB,
      }),
    ).toEqual({ kind: "detachCurrent", sessionId: "sB" });
  });

  it("starts a fresh chat pinned to the open session's agent, ignoring a stale selectedAgentId", () => {
    // The stale `selectedAgentId` never reaches this function — the plan pins
    // the fresh chat to the open session's agent by construction, which is
    // exactly what stops the switch from binding to the wrong agent.
    expect(
      planProjectContextChange({
        targetProjectId: "project-x",
        activeSessionId: "sB",
        currentSession: sessionB,
      }),
    ).toEqual({ kind: "startFreshChat", agentId: "agent-b", projectId: "project-x" });
  });

  it("only adjusts the new-chat draft project when there is no open session", () => {
    expect(
      planProjectContextChange({
        targetProjectId: "project-x",
        activeSessionId: null,
        currentSession: null,
      }),
    ).toEqual({ kind: "setDraftProject", projectId: "project-x" });
  });
});
