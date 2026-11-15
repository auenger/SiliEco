"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@silieco/core/auth";
import { paths } from "@silieco/core/paths";
import { workspaceListOptions } from "@silieco/core/workspace/queries";
import { NewWorkspacePage } from "@silieco/views/workspace/new-workspace-page";

export default function Page() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  // Users without membership can return to the invitation waiting page.
  const onBack =
    () => router.push(wsList.length > 0 ? paths.root() : paths.workspaces());

  return (
    <NewWorkspacePage
      onSuccess={(ws) => router.push(paths.workspace(ws.slug).tasks())}
      onBack={onBack}
    />
  );
}
