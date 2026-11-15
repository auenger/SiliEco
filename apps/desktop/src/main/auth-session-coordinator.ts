import type { AuthSessionUserId } from "../shared/auth-session";

export {
  AUTH_SESSION_STATE_CHANNEL,
  parseAuthSessionUserId,
} from "../shared/auth-session";

/**
 * Keeps dedicated task windows bound to the main window's authenticated
 * account. Tokens never cross renderer boundaries: only the resolved user id
 * is compared, and stale task windows are closed on logout/account switch.
 */
export class AuthSessionCoordinator<T> {
  private mainUserId: AuthSessionUserId | undefined;
  private readonly taskUserIds = new Map<
    T,
    AuthSessionUserId | undefined
  >();

  constructor(private readonly closeTaskWindow: (window: T) => void) {}

  registerTaskWindow(window: T): void {
    this.taskUserIds.set(window, undefined);
  }

  unregisterTaskWindow(window: T): void {
    this.taskUserIds.delete(window);
  }

  hasActiveMainSession(): boolean {
    return typeof this.mainUserId === "string";
  }

  isCurrentTaskSession(window: T): boolean {
    return (
      typeof this.mainUserId === "string" &&
      this.taskUserIds.get(window) === this.mainUserId
    );
  }

  reportMain(userId: AuthSessionUserId): boolean {
    const becameLoggedOut = userId === null && this.mainUserId !== null;
    const accountChanged =
      this.mainUserId !== undefined && this.mainUserId !== userId;
    this.mainUserId = userId;

    if (userId === null || accountChanged) {
      this.closeAllTaskWindows();
      return becameLoggedOut || accountChanged;
    }

    for (const [window, taskUserId] of this.taskUserIds) {
      if (taskUserId !== undefined && taskUserId !== userId) {
        this.closeTask(window);
      }
    }
    return false;
  }

  reportTask(window: T, userId: AuthSessionUserId): void {
    if (!this.taskUserIds.has(window)) return;
    this.taskUserIds.set(window, userId);
    if (
      userId === null ||
      (this.mainUserId !== undefined && this.mainUserId !== userId)
    ) {
      this.closeTask(window);
    }
  }

  private closeAllTaskWindows(): void {
    for (const window of [...this.taskUserIds.keys()]) {
      this.closeTask(window);
    }
  }

  private closeTask(window: T): void {
    this.taskUserIds.delete(window);
    this.closeTaskWindow(window);
  }
}
