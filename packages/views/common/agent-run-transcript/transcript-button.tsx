"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Loader2, ScrollText } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { cn } from "@silieco/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@silieco/ui/components/ui/tooltip";
import { api } from "@silieco/core/api";
import {
  chatKeys,
  isAgentRunMessageRunId,
  mergeAgentRunMessagesBySeq,
  agentRunMessagesOptions,
} from "@silieco/core/chat/queries";
import type { AgentRun } from "@silieco/core/types/agent";
import type { AgentRunMessagePayload } from "@silieco/core/types/events";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import { buildTimeline, type TimelineItem } from "./build-timeline";

interface TranscriptButtonProps {
  run: AgentRun;
  agentName: string;
  /**
   * Pre-loaded timeline. When provided the button skips the fetch and opens
   * the dialog immediately — used by surfaces that already own an accumulating
   * timeline. Omit for terminal runs; the button will fetch via
   * `api.listAgentRunMessages` on the first click and cache the result. Omit for
   * live runs too: the button then subscribes to the shared run-messages
   * cache so the dialog keeps growing as new events arrive.
   */
  items?: TimelineItem[];
  isLive?: boolean;
  className?: string;
  title?: string;
  /**
   * Optional content rendered above the transcript event list. Used to
   * surface autopilot webhook payloads inline with the run history.
   */
  headerSlot?: React.ReactNode;
}

/**
 * Compact icon-button that opens the full transcript dialog. Used on any
 * surface that lists agent runs (task activity card, agent detail
 * activity tab). Owns its own dialog state and lazy-load — the parent
 * just drops it in.
 *
 * Three data modes:
 *  - Provided items: parent owns the timeline, we just render it.
 *  - Live cache: `isLive` with no provided items and a persisted run id —
 *    subscribe to the shared `["run-messages", runId]` cache (seeded by the
 *    WS `run:message` stream) so the open dialog keeps growing in real time,
 *    and force a seq-merged backfill on open to heal any WS reconnect gap.
 *  - Lazy: terminal runs fetch once on first click and cache locally.
 */
export function TranscriptButton({
  run,
  agentName,
  items: providedItems,
  isLive = false,
  className,
  title = "View transcript",
  headerSlot,
}: TranscriptButtonProps) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  // Live cache mode: the running run feeds the shared run-messages cache, so
  // we render straight off that cache instead of a one-shot local snapshot.
  const liveCacheMode =
    isLive && providedItems === undefined && isAgentRunMessageRunId(run.id);

  // Latch the live path for the duration of an open session. The parent flips
  // `isLive` to false the moment the run finishes; without the latch the
  // dialog would drop to empty `loadedItems` mid-view. Staying on the cache
  // path keeps every delivered seq on screen and lets the dialog take a final
  // authoritative backfill on the running→terminal transition.
  const [liveSession, setLiveSession] = useState(false);
  useEffect(() => {
    if (!open) setLiveSession(false);
  }, [open]);

  // Live mode renders from the cache; lazy/provided modes from local state.
  const items = providedItems ?? loadedItems ?? [];

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (liveCacheMode) {
        setLiveSession(true);
        setOpen(true);
        return;
      }
      if (providedItems !== undefined || loadedItems !== null) {
        setOpen(true);
        return;
      }
      setLoading(true);
      api
        .listAgentRunMessages(run.id)
        .then((msgs) => {
          setLoadedItems(buildTimeline(msgs));
          setOpen(true);
        })
        .catch((err) => {
          console.error(err);
          setLoadedItems([]);
          setOpen(true);
        })
        .finally(() => setLoading(false));
    },
    [liveCacheMode, providedItems, loadedItems, run.id],
  );

  useEffect(() => {
    if (!open) return;

    const handleGlobalNavigate = () => {
      setOpen(false);
    };

    window.addEventListener("silieco:navigate", handleGlobalNavigate);
    return () => {
      window.removeEventListener("silieco:navigate", handleGlobalNavigate);
    };
  }, [open]);

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={<button type="button" />}
          onClick={handleClick}
          disabled={loading}
          aria-label={title}
          className={cn(
            "flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors disabled:opacity-50",
            className,
          )}
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ScrollText className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>{title}</TooltipContent>
      </Tooltip>

      {open &&
        (liveSession ? (
          <LiveTranscriptDialog
            run={run}
            agentName={agentName}
            isLive={isLive}
            onOpenChange={setOpen}
            headerSlot={headerSlot}
          />
        ) : (
          <AgentTranscriptDialog
            open={open}
            onOpenChange={setOpen}
            run={run}
            items={items}
            agentName={agentName}
            isLive={isLive}
            headerSlot={headerSlot}
          />
        ))}
    </>
  );
}

interface LiveTranscriptDialogProps {
  run: AgentRun;
  agentName: string;
  isLive: boolean;
  onOpenChange: (open: boolean) => void;
  headerSlot?: React.ReactNode;
}

/**
 * Live transcript view backed by the shared run-messages cache. Mounted only
 * while the dialog is open, so closed live rows hold no query subscription and
 * don't widen the baseline request volume.
 *
 * The cache observer is read-only (`enabled: false`): the WS `run:message`
 * handler is the live writer, and the backfill below is the only fetch here.
 * Keeping React Query from issuing its own refetch is deliberate — its result
 * would blind-replace the cache and could drop a seq that arrived mid-flight,
 * whereas the backfill merges by seq.
 */
function LiveTranscriptDialog({
  run,
  agentName,
  isLive,
  onOpenChange,
  headerSlot,
}: LiveTranscriptDialogProps) {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    ...agentRunMessagesOptions(run.id),
    enabled: false,
  });

  // Force a backfill on open, and again when the run reaches a terminal state.
  // `agentRunMessagesOptions` is `staleTime: Infinity`, so a plain subscription
  // never refetches — a WS reconnect gap (or the final tail of messages a
  // completed task run never re-broadcasts) would otherwise leave a hole.
  // Merge by seq so the fetch and any concurrent WS append both survive.
  useEffect(() => {
    if (!isAgentRunMessageRunId(run.id)) return;
    let cancelled = false;
    api
      .listAgentRunMessages(run.id)
      .then((msgs) => {
        if (cancelled) return;
        queryClient.setQueryData<AgentRunMessagePayload[]>(
          chatKeys.agentRunMessages(run.id),
          (old = []) => mergeAgentRunMessagesBySeq(old, msgs),
        );
      })
      .catch((err) => {
        console.error(err);
      });
    return () => {
      cancelled = true;
    };
  }, [run.id, isLive, queryClient]);

  const items = useMemo(() => buildTimeline(data ?? []), [data]);

  return (
    <AgentTranscriptDialog
      open
      onOpenChange={onOpenChange}
      run={run}
      items={items}
      agentName={agentName}
      isLive={isLive}
      headerSlot={headerSlot}
    />
  );
}
