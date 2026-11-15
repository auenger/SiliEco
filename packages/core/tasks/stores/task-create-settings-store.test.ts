import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_MANUAL_CREATE_FIELDS,
  DEFAULT_QUICK_CREATE_FIELDS,
  useTaskCreateSettingsStore,
} from "./task-create-settings-store";

describe("task create settings store", () => {
  beforeEach(() => {
    useTaskCreateSettingsStore.setState({
      quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
      manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
    });
  });

  it("defaults to project-only quick create and the classic manual toolbar", () => {
    expect(useTaskCreateSettingsStore.getState().quickCreateFields).toEqual(["project"]);
    expect(useTaskCreateSettingsStore.getState().manualCreateFields).toEqual([
      "status",
      "priority",
      "assignee",
      "labels",
      "project",
    ]);
  });

  it("keeps quick create fields in canonical order regardless of toggle sequence", () => {
    const { setQuickCreateFieldVisible } = useTaskCreateSettingsStore.getState();

    setQuickCreateFieldVisible("due_date", true);
    setQuickCreateFieldVisible("priority", true);

    expect(useTaskCreateSettingsStore.getState().quickCreateFields).toEqual([
      "project",
      "priority",
      "due_date",
    ]);

    setQuickCreateFieldVisible("project", false);
    expect(useTaskCreateSettingsStore.getState().quickCreateFields).toEqual([
      "priority",
      "due_date",
    ]);
  });

  it("toggles manual create fields independently of quick create", () => {
    const { setManualCreateFieldVisible } = useTaskCreateSettingsStore.getState();

    setManualCreateFieldVisible("labels", false);
    setManualCreateFieldVisible("due_date", true);

    expect(useTaskCreateSettingsStore.getState().manualCreateFields).toEqual([
      "status",
      "priority",
      "assignee",
      "project",
      "due_date",
    ]);
    expect(useTaskCreateSettingsStore.getState().quickCreateFields).toEqual(["project"]);
  });

  it("is a no-op to re-enable an already visible field", () => {
    const { setManualCreateFieldVisible } = useTaskCreateSettingsStore.getState();

    setManualCreateFieldVisible("status", true);

    expect(useTaskCreateSettingsStore.getState().manualCreateFields).toEqual(
      DEFAULT_MANUAL_CREATE_FIELDS,
    );
  });
});
