"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@silieco/ui/lib/utils";
import { UnicodeSpinner } from "@silieco/ui/components/common/unicode-spinner";
import type { AgentAvailability } from "@silieco/core/agents";
import type { ChatPendingRun, AgentRunMessagePayload } from "@silieco/core/types";
import { formatElapsedSecs } from "../lib/format";
import { useT } from "../../i18n";

interface Props {
  /** Server-authoritative pending-run snapshot (`created_at` anchors the timer). */
  pendingRun: ChatPendingRun;
  /** Live run-message stream — the latest non-error entry decides the running-stage label. */
  agentRunMessages: readonly AgentRunMessagePayload[];
  /** Resolved presence; pass `undefined` to suppress availability hints. */
  availability: AgentAvailability | undefined;
}

interface Stage {
  label: string;
  static?: boolean;
}

type StageKey =
  | "offline"
  | "reconnecting"
  | "queued"
  | "waiting_local_directory"
  | "starting_up"
  | "thinking"
  | "typing";

type ToolKey =
  | "running_command"
  | "reading_files"
  | "searching_code"
  | "making_edits"
  | "searching_web"
  | "fallback";

// Tool slug → translation key. Unknown tools fall back to "Working".
const TOOL_KEY_BY_SLUG: Record<string, Exclude<ToolKey, "fallback">> = {
  bash: "running_command",
  exec: "running_command",
  read: "reading_files",
  glob: "reading_files",
  grep: "searching_code",
  write: "making_edits",
  edit: "making_edits",
  multi_edit: "making_edits",
  multiedit: "making_edits",
  web_search: "searching_web",
  websearch: "searching_web",
};

// Pure stage decision returning translation keys. The hook below maps these
// keys into localized labels — keeping the decision pure makes it easy to
// follow the priority rules without translation noise.
export function pickStageKeys(
  status: string | undefined,
  agentRunMessages: readonly AgentRunMessagePayload[],
  availability: AgentAvailability | undefined,
): { stageKey: StageKey; toolKey?: ToolKey; static?: boolean } {
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { stageKey: "offline", static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { stageKey: "reconnecting" };
  }
  // Daemon-emitted hold state for the local_directory flow: the project is
  // pinned to a path that another run currently owns. The daemon publishes
  // this status string when it dequeues a run but can't acquire the path
  // lock; the renderer surfaces a dedicated label so the user understands
  // why a queued run isn't moving.
  if (status === "waiting_local_directory") {
    return { stageKey: "waiting_local_directory", static: true };
  }
  if (status === "queued") return { stageKey: "queued" };
  if (status === "dispatched") return { stageKey: "starting_up" };

  // running: latest meaningful message decides the label.
  let latest: AgentRunMessagePayload | null = null;
  for (let i = agentRunMessages.length - 1; i >= 0; i--) {
    const m = agentRunMessages[i];
    if (m && m.type !== "error" && m.type !== "tool_result") {
      latest = m;
      break;
    }
  }

  if (!latest) return { stageKey: "thinking" };
  if (latest.type === "thinking") return { stageKey: "thinking" };
  if (latest.type === "text") return { stageKey: "typing" };
  if (latest.type === "tool_use") {
    const tool = (latest.tool ?? "").toLowerCase();
    const toolKey = TOOL_KEY_BY_SLUG[tool] ?? "fallback";
    // tool_use is technically still "thinking + tool" — surface the tool
    // label in the toolKey channel; main stage label uses the tool one.
    return { stageKey: "thinking", toolKey };
  }
  return { stageKey: "thinking" };
}

function useResolveStage(): (
  status: string | undefined,
  agentRunMessages: readonly AgentRunMessagePayload[],
  availability: AgentAvailability | undefined,
) => Stage {
  const { t } = useT("chat");
  return (status, agentRunMessages, availability) => {
    const decision = pickStageKeys(status, agentRunMessages, availability);
    if (decision.toolKey) {
      return {
        label: t(($) => $.status_pill.tools[decision.toolKey!]),
      };
    }
    return {
      label: t(($) => $.status_pill.stages[decision.stageKey]),
      static: decision.static,
    };
  };
}

export function RunStatusPill({
  pendingRun,
  agentRunMessages,
  availability,
}: Props) {
  const resolveStage = useResolveStage();
  // Anchor: locked on first render. Once set we never reassign — otherwise
  // the timer would visibly snap backwards when an optimistic-seeded
  // `Date.now()` anchor is later replaced by a server-side created_at that
  // happened a few hundred ms earlier. Monotonic elapsed > strict accuracy.
  const anchorRef = useRef<number | null>(null);
  if (anchorRef.current === null) {
    if (pendingRun.created_at) {
      const t = Date.parse(pendingRun.created_at);
      anchorRef.current = Number.isFinite(t) ? t : Date.now();
    } else {
      anchorRef.current = Date.now();
    }
  }
  const anchor = anchorRef.current;

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  // Effective status — defense-in-depth derive on top of the cache. If any
  // task_message has streamed in, the daemon has by definition started
  // running; we trust that observation over a stale cache.
  const status = agentRunMessages.length > 0 ? "running" : pendingRun.status;
  const elapsedSecs = Math.max(0, Math.floor((now - anchor) / 1000));
  const stage = resolveStage(status, agentRunMessages, availability);

  return (
    <div
      className="flex items-center gap-1.5 px-1 text-caption text-muted-foreground"
      aria-live="polite"
    >
      {!stage.static && (
        <UnicodeSpinner name="breathe" className="opacity-70" />
      )}
      <span className="truncate">
        <span className={cn(!stage.static && "animate-chat-text-shimmer")}>
          {stage.label}
        </span>
        <span className="opacity-70 tabular-nums"> · {formatElapsedSecs(elapsedSecs)}</span>
      </span>
    </div>
  );
}
