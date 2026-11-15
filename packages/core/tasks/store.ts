"use client";

import { create } from "zustand";

interface TaskClientState {
  activeTaskId: string | null;
  setActiveTask: (id: string | null) => void;
}

export const useTaskStore = create<TaskClientState>((set) => ({
  activeTaskId: null,
  setActiveTask: (id) => set({ activeTaskId: id }),
}));
