import { describe, expect, it, vi } from "vitest";
import {
  AuthSessionCoordinator,
  parseAuthSessionUserId,
} from "./auth-session-coordinator";

describe("AuthSessionCoordinator", () => {
  it("keeps an task window whose account matches the main window", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
    coordinator.registerTaskWindow("task-1");
    coordinator.reportTask("task-1", "user-a");

    expect(close).not.toHaveBeenCalled();
    expect(coordinator.hasActiveMainSession()).toBe(true);
    expect(coordinator.isCurrentTaskSession("task-1")).toBe(true);
  });

  it("closes every task window on main logout or account switch", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
    coordinator.registerTaskWindow("task-1");
    coordinator.registerTaskWindow("task-2");

    coordinator.reportMain("user-b");

    expect(close).toHaveBeenCalledTimes(2);
    expect(close).toHaveBeenCalledWith("task-1");
    expect(close).toHaveBeenCalledWith("task-2");
  });

  it("invalidates account-scoped work once per session transition", () => {
    const coordinator = new AuthSessionCoordinator<string>(() => {});

    expect(coordinator.reportMain("user-a")).toBe(false);
    expect(coordinator.reportMain("user-a")).toBe(false);
    expect(coordinator.reportMain(null)).toBe(true);
    expect(coordinator.reportMain(null)).toBe(false);
    expect(coordinator.reportMain("user-b")).toBe(true);
  });

  it("closes an task renderer that reports a stale or missing session", () => {
    const close = vi.fn();
    const coordinator = new AuthSessionCoordinator<string>(close);
    coordinator.reportMain("user-a");
    coordinator.registerTaskWindow("stale");
    coordinator.registerTaskWindow("logged-out");

    coordinator.reportTask("stale", "user-b");
    coordinator.reportTask("logged-out", null);

    expect(close).toHaveBeenCalledTimes(2);
    expect(coordinator.isCurrentTaskSession("stale")).toBe(false);
  });
});

describe("parseAuthSessionUserId", () => {
  it("accepts a bounded user id or explicit logged-out state", () => {
    expect(parseAuthSessionUserId(" user-a ")).toBe("user-a");
    expect(parseAuthSessionUserId(null)).toBeNull();
    expect(parseAuthSessionUserId(" ")).toBeUndefined();
    expect(parseAuthSessionUserId(42)).toBeUndefined();
    expect(parseAuthSessionUserId("x".repeat(257))).toBeUndefined();
  });
});
