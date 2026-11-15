"use client";

import { create } from "zustand";

type ModalType =
  | "create-workspace"
  | "create-task"
  | "quick-create-task"
  | "create-project"
  | "create-squad"
  | "feedback"
  | "task-set-parent"
  | "task-add-child"
  | "task-delete-confirm"
  | "task-run-confirm"
  | null;

interface ModalStore {
  modal: ModalType;
  data: Record<string, unknown> | null;
  open: (modal: NonNullable<ModalType>, data?: Record<string, unknown> | null) => void;
  close: () => void;
}

export const useModalStore = create<ModalStore>((set) => ({
  modal: null,
  data: null,
  open: (modal, data = null) => set({ modal, data }),
  close: () => set({ modal: null, data: null }),
}));
