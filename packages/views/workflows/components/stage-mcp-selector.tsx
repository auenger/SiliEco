"use client";

import { Server } from "lucide-react";
import type { WorkspaceMCPServer } from "@silieco/core/types";
import { Badge } from "@silieco/ui/components/ui/badge";
import { Checkbox } from "@silieco/ui/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@silieco/ui/components/ui/select";
import { useT } from "../../i18n";

type MCPAccessMode = "inherit" | "none" | "selected";

function accessMode(ids: string[] | null): MCPAccessMode {
  if (ids === null) return "inherit";
  return ids.length === 0 ? "none" : "selected";
}

export function StageMCPSelector({
  servers,
  selectedIds,
  disabled = false,
  onChange,
}: {
  servers: readonly WorkspaceMCPServer[];
  selectedIds: string[] | null;
  disabled?: boolean;
  onChange: (ids: string[] | null) => void;
}) {
  const { t } = useT("workflows");
  const mode = accessMode(selectedIds);
  const selected = new Set(selectedIds ?? []);

  const changeMode = (next: MCPAccessMode) => {
    if (next === "inherit") onChange(null);
    else if (next === "none") onChange([]);
    else onChange(selectedIds?.length ? selectedIds : servers.map((server) => server.id));
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <span className="flex items-center gap-1.5 text-caption font-medium">
          <Server className="size-3.5 text-muted-foreground" />
          {t(($) => $.designer.mcp_access)}
        </span>
        <Select
          items={[
            { value: "inherit", label: t(($) => $.designer.mcp_inherit) },
            { value: "none", label: t(($) => $.designer.mcp_none) },
            { value: "selected", label: t(($) => $.designer.mcp_selected) },
          ]}
          value={mode}
          disabled={disabled}
          onValueChange={(value) => changeMode(value as MCPAccessMode)}
        >
          <SelectTrigger className="w-48"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="inherit">{t(($) => $.designer.mcp_inherit)}</SelectItem>
            <SelectItem value="none">{t(($) => $.designer.mcp_none)}</SelectItem>
            <SelectItem value="selected">{t(($) => $.designer.mcp_selected)}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <p className="text-micro leading-4 text-muted-foreground">
        {mode === "inherit"
          ? t(($) => $.designer.mcp_inherit_hint)
          : mode === "none"
            ? t(($) => $.designer.mcp_none_hint)
            : t(($) => $.designer.mcp_selected_hint)}
      </p>
      {mode === "selected" && (
        <div className="rounded-lg border border-border/80 bg-muted/10">
          {servers.length === 0 ? (
            <p className="px-3 py-4 text-caption text-muted-foreground">{t(($) => $.designer.mcp_empty)}</p>
          ) : (
            <ul className="divide-y">
              {servers.map((server) => (
                <li key={server.id} className="flex items-center gap-2.5 px-3 py-2">
                  <Checkbox
                    checked={selected.has(server.id)}
                    disabled={disabled}
                    aria-label={t(($) => $.designer.mcp_toggle_aria, { name: server.name })}
                    onCheckedChange={(checked) =>
                      onChange(
                        checked === true
                          ? [...new Set([...(selectedIds ?? []), server.id])]
                          : (selectedIds ?? []).filter((id) => id !== server.id),
                      )
                    }
                  />
                  <span className="min-w-0 flex-1 truncate text-caption font-medium">{server.name}</span>
                  <Badge variant="outline">MCP</Badge>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
