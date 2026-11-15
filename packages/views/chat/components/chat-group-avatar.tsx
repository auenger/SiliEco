"use client";

import { UsersRound } from "lucide-react";
import type { ChatSession } from "@silieco/core/types";
import { cn } from "@silieco/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";

/** A stable, centered mosaic of up to nine human and agent participants. */
export function ChatGroupAvatar({ participants = [], title, className }: {
  participants?: ChatSession["participants"];
  title?: string;
  className?: string;
}) {
  const unique = participants.filter((p, i) => participants.findIndex((other) => other.type === p.type && other.id === p.id) === i).slice(0, 9);
  const columns = unique.length <= 4 ? 2 : 3;
  const tileSize = unique.length === 1 ? 30 : (34 - (columns - 1) * 2) / columns;
  return (
    <span aria-label={title} className={cn("flex size-10 shrink-0 flex-wrap content-center justify-center gap-0.5 overflow-hidden rounded-lg bg-muted p-[3px] ring-1 ring-inset ring-border", className)}>
      {unique.length ? unique.map((participant) => (
        <span key={`${participant.type}:${participant.id}`} className="relative overflow-hidden" style={{ width: tileSize, height: tileSize }}>
          <span className="absolute left-0 top-0 origin-top-left" style={{ transform: `scale(${tileSize / 16})` }}>
            <ActorAvatar actorType={participant.type} actorId={participant.id} size="xs" profileLink={false} />
          </span>
        </span>
      )) : <UsersRound className="size-5 self-center text-muted-foreground" />}
    </span>
  );
}
