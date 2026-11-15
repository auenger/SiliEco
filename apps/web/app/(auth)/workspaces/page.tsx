"use client";
import { useEffect } from "react";
import { useAuthStore } from "@silieco/core/auth";
import { paths } from "@silieco/core/paths";
import { useNavigation } from "@silieco/views/navigation";
import { WorkspacesPage } from "@silieco/views/workspace/workspaces-page";
export default function Page() {
  const user = useAuthStore((s) => s.user);
  const loading = useAuthStore((s) => s.isLoading);
  const { replace } = useNavigation();
  useEffect(() => { if (!loading && !user) replace(paths.login()); }, [loading, user, replace]);
  return !loading && user ? <WorkspacesPage /> : null;
}
