"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { FolderKanban, Mail, RefreshCw } from "lucide-react";
import { useAuthStore } from "@silieco/core/auth";
import { useConfigStore } from "@silieco/core/config";
import { workspaceListOptions, myInvitationListOptions } from "@silieco/core/workspace/queries";
import { setCurrentWorkspace } from "@silieco/core/platform";
import { paths } from "@silieco/core/paths";
import { Button } from "@silieco/ui/components/ui/button";
import { AppLink, useNavigation } from "../navigation";
import { DragStrip } from "../platform";
import { useLogout } from "../auth";
import { useT } from "../i18n";

export function WorkspacesPage() {
  const { t } = useT("workspace");
  const { replace } = useNavigation();
  const logout = useLogout();
  const user = useAuthStore((s) => s.user);
  const creationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const workspaces = useQuery({ ...workspaceListOptions(), refetchInterval: 15000, enabled: !!user });
  const invitations = useQuery({ ...myInvitationListOptions(), refetchInterval: 15000, enabled: !!user });
  useEffect(() => {
    setCurrentWorkspace(null, null);
    document.cookie = "last_workspace_slug=; path=/; max-age=0; SameSite=Lax";
  }, []);
  const first = workspaces.data?.[0];
  useEffect(() => {
    if (first) replace(paths.workspace(first.slug).tasks());
  }, [first, replace]);
  return (
    <div className="flex min-h-svh flex-col bg-background">
      <DragStrip />
      <header className="flex items-center justify-between gap-4 border-b px-6 py-4">
        <span className="font-medium">Silieco</span>
        <div className="flex min-w-0 items-center gap-3">
          <span className="truncate text-caption text-muted-foreground">{user?.email}</span>
          <Button variant="ghost" size="sm" onClick={logout}>{t(($) => $.new_page.log_out)}</Button>
        </div>
      </header>
      <main className="flex flex-1 flex-col items-center justify-center gap-5 px-6 pb-16 text-center">
        <div className="rounded-2xl bg-muted p-4"><FolderKanban className="size-8 text-muted-foreground" /></div>
        <h1 className="text-display-sm font-semibold">{t(($) => $.empty.title)}</h1>
        <p className="max-w-md text-body text-muted-foreground">{t(($) => $.empty.description)}</p>
        {(workspaces.isError || invitations.isError) && <p role="alert" className="text-caption text-destructive">{t(($) => $.empty.load_failed)}</p>}
        {!!invitations.data?.length && <AppLink href={paths.invitations()} className="inline-flex items-center gap-2 rounded-md border px-4 py-3 font-medium hover:bg-accent">
          <Mail className="size-4" />{t(($) => $.empty.invitations, { count: invitations.data.length })}
        </AppLink>}
        <div className="flex flex-wrap justify-center gap-3">
          {!creationDisabled && <AppLink href={paths.newWorkspace()} className="rounded-md bg-primary px-4 py-2 text-body font-medium text-primary-foreground hover:opacity-90">{t(($) => $.create_form.submit)}</AppLink>}
          <Button variant="outline" disabled={workspaces.isFetching || invitations.isFetching} onClick={() => { void workspaces.refetch(); void invitations.refetch(); }}>
            <RefreshCw className="size-4" />{t(($) => $.empty.refresh)}
          </Button>
        </div>
        <p className="max-w-md text-caption text-muted-foreground">{t(($) => $.empty.hint)}</p>
      </main>
    </div>
  );
}
