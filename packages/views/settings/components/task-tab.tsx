"use client";

import { Switch } from "@silieco/ui/components/ui/switch";
import {
  MANUAL_CREATE_FIELDS,
  QUICK_CREATE_FIELDS,
  useTaskCreateSettingsStore,
} from "@silieco/core/tasks/stores/task-create-settings-store";
import { toast } from "sonner";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Task settings — its own tab under "My Account". One group per create-task
 * mode (agent quick create / manual create), each a switch list of the fields
 * that mode keeps on its dialog toolbar. Persisted client-side per workspace;
 * a field toggled off stays reachable from the dialog's ⋯ overflow and
 * re-surfaces automatically while it holds a value, so hiding is never
 * destructive.
 */
export function TaskTab() {
  const { t } = useT("settings");
  const quickFields = useTaskCreateSettingsStore((s) => s.quickCreateFields);
  const setQuickVisible = useTaskCreateSettingsStore((s) => s.setQuickCreateFieldVisible);
  const manualFields = useTaskCreateSettingsStore((s) => s.manualCreateFields);
  const setManualVisible = useTaskCreateSettingsStore((s) => s.setManualCreateFieldVisible);

  const savedToast = () =>
    toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.task)}
      description={t(($) => $.task.description)}
    >
      <SettingsSection
        title={t(($) => $.task.quick_create_title)}
        description={t(($) => $.task.quick_create_description)}
      >
        <SettingsCard>
          {QUICK_CREATE_FIELDS.map((field) => (
            <SettingsRow key={field} label={t(($) => $.task.fields[field])}>
              <Switch
                checked={quickFields.includes(field)}
                onCheckedChange={(checked) => {
                  setQuickVisible(field, checked);
                  savedToast();
                }}
                aria-label={t(($) => $.task.fields[field])}
              />
            </SettingsRow>
          ))}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.task.manual_create_title)}
        description={t(($) => $.task.manual_create_description)}
      >
        <SettingsCard>
          {MANUAL_CREATE_FIELDS.map((field) => (
            <SettingsRow key={field} label={t(($) => $.task.fields[field])}>
              <Switch
                checked={manualFields.includes(field)}
                onCheckedChange={(checked) => {
                  setManualVisible(field, checked);
                  savedToast();
                }}
                aria-label={t(($) => $.task.fields[field])}
              />
            </SettingsRow>
          ))}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
