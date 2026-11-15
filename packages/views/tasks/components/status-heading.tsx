import type { TaskStatus } from "@silieco/core/types";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";

export function StatusHeading({
  status,
  count,
}: {
  status: TaskStatus;
  count: number;
}) {
  const { t } = useT("tasks");
  return (
    <div className="flex items-center gap-2">
      <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
        <StatusIcon status={status} className="h-3 w-3" />
        {t(($) => $.status[status])}
      </span>
      <span className="text-caption text-muted-foreground">{count}</span>
    </div>
  );
}
