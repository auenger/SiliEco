import { create } from "zustand";
import { ApiError } from "../api";

export interface RevisionConflictPayload {
  kind: "issue" | "comment";
  current: Record<string, unknown>;
  attempted: Record<string, unknown>;
  retry: () => Promise<unknown>;
  refresh: () => Promise<unknown> | void;
}

interface RevisionConflictState {
  conflict: RevisionConflictPayload | null;
  open: (conflict: RevisionConflictPayload) => void;
  close: () => void;
}

export const useRevisionConflictStore = create<RevisionConflictState>((set) => ({
  conflict: null,
  open: (conflict) => set({ conflict }),
  close: () => set({ conflict: null }),
}));

export function revisionConflictBody(error: unknown): {
  current: Record<string, unknown>;
  attempted: Record<string, unknown>;
} | null {
  if (!(error instanceof ApiError) || error.status !== 409 || !error.body || typeof error.body !== "object") return null;
  const body = error.body as Record<string, unknown>;
  if (body.code !== "revision_conflict" || !body.current || typeof body.current !== "object" || !body.attempted || typeof body.attempted !== "object") return null;
  return {
    current: body.current as Record<string, unknown>,
    attempted: body.attempted as Record<string, unknown>,
  };
}

export function isRevisionConflictError(error: unknown): boolean {
  return revisionConflictBody(error) !== null;
}
