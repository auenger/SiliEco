import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  openCreateTaskWithPreference,
  useCreateModeStore,
} from "./create-mode-store";
import { useModalStore } from "../../modals";

describe("openCreateTaskWithPreference", () => {
  const initialMode = useCreateModeStore.getState().lastMode;

  beforeEach(() => {
    useModalStore.getState().close();
  });

  afterEach(() => {
    useCreateModeStore.getState().setLastMode(initialMode);
    useModalStore.getState().close();
  });

  it("opens quick-create-task when last mode is agent", () => {
    useCreateModeStore.getState().setLastMode("agent");
    openCreateTaskWithPreference();
    expect(useModalStore.getState().modal).toBe("quick-create-task");
    expect(useModalStore.getState().data).toBeNull();
  });

  it("opens create-task when last mode is manual", () => {
    useCreateModeStore.getState().setLastMode("manual");
    openCreateTaskWithPreference();
    expect(useModalStore.getState().modal).toBe("create-task");
  });

  it("forwards seed data to whichever modal is opened", () => {
    useCreateModeStore.getState().setLastMode("manual");
    openCreateTaskWithPreference({ project_id: "p1" });
    expect(useModalStore.getState().modal).toBe("create-task");
    expect(useModalStore.getState().data).toEqual({ project_id: "p1" });

    useCreateModeStore.getState().setLastMode("agent");
    openCreateTaskWithPreference({ project_id: "p2" });
    expect(useModalStore.getState().modal).toBe("quick-create-task");
    expect(useModalStore.getState().data).toEqual({ project_id: "p2" });
  });
});
