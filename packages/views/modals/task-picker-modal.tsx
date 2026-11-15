"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import type { Task } from "@silieco/core/types";
import { api } from "@silieco/core/api";
import {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@silieco/ui/components/ui/command";
import { StatusIcon } from "../tasks/components/status-icon";
import { useT } from "../i18n";

interface TaskPickerModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  excludeIds: string[];
  onSelect: (task: Task) => void;
}

export function TaskPickerModal({
  open,
  onOpenChange,
  title,
  description,
  excludeIds,
  onSelect,
}: TaskPickerModalProps) {
  const { t } = useT("modals");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Task[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const abortRef = useRef<AbortController>(undefined);

  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setIsLoading(false);
    }
  }, [open]);

  const search = useCallback(
    (q: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();

      if (!q.trim()) {
        setResults([]);
        setIsLoading(false);
        return;
      }

      setIsLoading(true);
      debounceRef.current = setTimeout(async () => {
        const controller = new AbortController();
        abortRef.current = controller;
        try {
          const res = await api.searchTasks({
            q: q.trim(),
            limit: 20,
            include_closed: true,
            signal: controller.signal,
          });
          if (!controller.signal.aborted) {
            setResults(res.tasks.filter((i) => !excludeIds.includes(i.id)));
            setIsLoading(false);
          }
        } catch {
          if (!controller.signal.aborted) {
            setIsLoading(false);
          }
        }
      }, 300);
    },
    [excludeIds],
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder={t(($) => $.task_picker.search_placeholder)}
          value={query}
          onValueChange={(v) => {
            setQuery(v);
            search(v);
          }}
        />
        <CommandList>
          {isLoading && (
            <div className="py-6 text-center text-body text-muted-foreground">
              {t(($) => $.task_picker.searching)}
            </div>
          )}
          {!isLoading && query.trim() && results.length === 0 && (
            <CommandEmpty>{t(($) => $.task_picker.no_results)}</CommandEmpty>
          )}
          {!isLoading && !query.trim() && (
            <div className="py-6 text-center text-body text-muted-foreground">
              {t(($) => $.task_picker.prompt_to_search)}
            </div>
          )}
          {results.length > 0 && (
            <CommandGroup>
              {results.map((task) => (
                <CommandItem
                  key={task.id}
                  value={task.id}
                  onSelect={() => {
                    onSelect(task);
                    onOpenChange(false);
                  }}
                >
                  <StatusIcon status={task.status} className="h-3.5 w-3.5 shrink-0" />
                  <span className="text-muted-foreground shrink-0">{task.identifier}</span>
                  <span className="truncate">{task.title}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
