"use client";
import { useState } from "react";
import {
  useCheckProjectDirectory,
  useCreateProjectResource,
} from "@silieco/core/projects";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import type { RuntimeMachine } from "../../runtimes/components/runtime-machines";
import { useT } from "../../i18n";

export function ProjectDirectoryBinding({
  wsId,
  projectId,
  machines,
  repositories,
}: {
  wsId: string;
  projectId: string;
  machines: RuntimeMachine[];
  repositories: { id: string; label: string }[];
}) {
  const { t } = useT("resources");
  const [open, setOpen] = useState(false);
  const [daemonId, setDaemonId] = useState("");
  const [path, setPath] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  const [error, setError] = useState("");
  const check = useCheckProjectDirectory(projectId);
  const create = useCreateProjectResource(wsId, projectId);
  const busy = check.isPending || create.isPending;
  const selectedDaemon = daemonId || machines[0]?.daemonId || "";
  async function bind() {
    setError("");
    try {
      await check.mutateAsync({ daemonId: selectedDaemon, path: path.trim() });
      await create.mutateAsync({
        resource_type: "local_directory",
        resource_ref: {
          daemon_id: selectedDaemon,
          local_path: path.trim(),
          ...(repositoryId ? { repository_resource_id: repositoryId } : {}),
        },
      });
      setOpen(false);
      setPath("");
    } catch (e) {
      setError(
        e instanceof Error ? e.message : t(($) => $.directory_bind_failed),
      );
    }
  }
  return (
    <div className="py-2">
      <Button size="sm" variant="outline" onClick={() => setOpen(!open)}>
        {t(($) => $.directory_bind)}
      </Button>
      {open && (
        <div className="mt-2 space-y-2 rounded border p-3">
          <label className="block text-caption">
            {t(($) => $.directory_machine)}
            <select
              className="mt-1 block w-full rounded border bg-background p-2"
              value={selectedDaemon}
              disabled={busy}
              onChange={(e) => setDaemonId(e.target.value)}
            >
              <option value="" disabled>
                {t(($) => $.directory_machine)}
              </option>
              {machines.map((m) => (
                <option key={m.id} value={m.daemonId ?? ""}>
                  {m.title}
                </option>
              ))}
            </select>
          </label>
          <label className="block text-caption">
            {t(($) => $.directory_path)}
            <Input
              className="mt-1"
              value={path}
              disabled={busy}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/Users/name/projects/example"
            />
          </label>
          {repositories.length > 0 && (
            <label className="block text-caption">
              {t(($) => $.directory_repository)}
              <select
                className="mt-1 block w-full rounded border bg-background p-2"
                value={repositoryId}
                disabled={busy}
                onChange={(e) => setRepositoryId(e.target.value)}
              >
                <option value="">{t(($) => $.directory_standalone)}</option>
                {repositories.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          <p className="text-caption text-muted-foreground">
            {t(($) => $.directory_binding_hint)}
          </p>
          {error && (
            <p role="alert" className="text-caption text-destructive">
              {error}
            </p>
          )}
          <Button
            size="sm"
            disabled={busy || !selectedDaemon || !path.trim()}
            onClick={() => void bind()}
          >
            {busy
              ? t(($) => $.directory_loading)
              : t(($) => $.directory_bind_confirm)}
          </Button>
        </div>
      )}
    </div>
  );
}
