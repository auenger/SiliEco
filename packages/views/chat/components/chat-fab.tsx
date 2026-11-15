"use client";

import { MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@silieco/ui/lib/utils";
import { useChatStore } from "@silieco/core/chat";
import {
  chatSessionsOptions,
  countUnreadChatSessions,
  hasPendingChatRunsOptions,
} from "@silieco/core/chat/queries";
import { useWorkspaceId } from "@silieco/core/hooks";
import { createLogger } from "@silieco/core/logger";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@silieco/ui/components/ui/tooltip";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");

export function ChatFab() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const toggle = useChatStore((s) => s.toggle);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  // FAB only needs a boolean "is anything running", and only while the window
  // is closed (when open, ChatWindow owns the detailed pending query). Gating
  // on `enabled: !isOpen` keeps the minimised button off the per-message
  // aggregate hot path entirely (SILI-4159).
  const { data: hasPending } = useQuery({
    ...hasPendingChatRunsOptions(wsId),
    enabled: !isOpen,
  });

  if (isOpen) return null;

  const unreadSessionCount = countUnreadChatSessions(sessions);
  const isRunning = hasPending?.has_pending ?? false;

  const handleClick = () => {
    logger.info("fab.click (open chat)", { unreadSessionCount, isRunning });
    toggle();
  };

  // Tooltip text carries the running/unread state on hover; the FAB itself no
  // longer shows an unread-count badge (it duplicated the chat tab's, SILI-4374).
  const tooltip = isRunning
    ? t(($) => $.fab.running)
    : unreadSessionCount > 0
      ? t(($) => $.fab.unread, { count: unreadSessionCount })
      : t(($) => $.fab.default);

  return (
    <Tooltip>
      <TooltipTrigger
        data-chat-launcher=""
        onClick={handleClick}
        aria-label={tooltip}
        className={cn(
          // Geometry comes from the shared tokens so the clearance pages
          // reserve for this corner is derived from the same numbers.
          "absolute bottom-[var(--chat-launcher-inset)] right-[var(--chat-launcher-inset)] z-50 flex size-[var(--chat-launcher-size)] touch-manipulation items-center justify-center rounded-full bg-surface-raised text-muted-foreground shadow-[var(--floating-shadow)] ring-1 ring-surface-border transition-[background-color,color,box-shadow] hover:bg-surface-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/70 focus-visible:ring-offset-2 focus-visible:ring-offset-page-canvas active:bg-surface-hover",
          // Impulse the button itself while a chat run is running — no
          // outer ring to keep things calm.
          isRunning && "animate-chat-impulse",
        )}
      >
        <MessageCircle className="size-5" />
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={10}>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
