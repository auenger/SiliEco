import { useState } from "react";
import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTaskDetailScrollRestore } from "./use-task-detail-scroll-restore";

let keyCounter = 0;
let rafCallbacks: Array<{ id: number; callback: FrameRequestCallback }> = [];
let rafId = 0;
let cancelledFrameIds: number[] = [];

function nextKey(label: string) {
  keyCounter += 1;
  return `test-${keyCounter}:${label}`;
}

function Harness({
  restoreKey,
  ready = true,
  disabled = false,
}: {
  restoreKey: string;
  ready?: boolean;
  disabled?: boolean;
}) {
  const [scrollContainerEl, setScrollContainerEl] =
    useState<HTMLDivElement | null>(null);

  useTaskDetailScrollRestore({
    restoreKey,
    scrollContainerEl,
    ready,
    disabled,
  });

  return (
    <div
      ref={setScrollContainerEl}
      data-testid="scroller"
      style={{ height: 100, overflowY: "auto" }}
    >
      <div style={{ height: 2000 }} />
    </div>
  );
}

function flushNextAnimationFrame() {
  act(() => {
    const callbacks = rafCallbacks;
    rafCallbacks = [];
    callbacks.forEach(({ callback }) => callback(performance.now()));
  });
}

function setScroll(el: HTMLElement, top: number) {
  el.scrollTop = top;
  fireEvent.scroll(el);
}

describe("useTaskDetailScrollRestore", () => {
  beforeEach(() => {
    rafCallbacks = [];
    rafId = 0;
    cancelledFrameIds = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      rafId += 1;
      rafCallbacks.push({ id: rafId, callback });
      return rafId;
    });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => {
      cancelledFrameIds.push(id);
      rafCallbacks = rafCallbacks.filter((request) => request.id !== id);
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("restores the previous scrollTop for a visited task and starts unseen tasks at top", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 500);

    rerender(<Harness restoreKey={taskB} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    setScroll(scroller, 120);

    rerender(<Harness restoreKey={taskA} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(500);
  });

  it("keeps positions when navigating through a page that unmounts task detail", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const firstPage = render(<Harness restoreKey={taskA} />);
    setScroll(firstPage.getByTestId("scroller"), 610);
    firstPage.unmount();

    const secondPage = render(<Harness restoreKey={taskB} />);
    expect(secondPage.getByTestId("scroller").scrollTop).toBe(0);
    secondPage.unmount();

    const returnedPage = render(<Harness restoreKey={taskA} />);
    expect(returnedPage.getByTestId("scroller").scrollTop).toBe(610);
  });

  it("waits until the page is ready before restoring a saved scrollTop", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 640);

    rerender(<Harness restoreKey={taskB} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    rerender(<Harness restoreKey={taskA} ready={false} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    rerender(<Harness restoreKey={taskA} ready />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(640);
  });

  it("does not save a new task scroll position before that task is ready", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 500);

    rerender(<Harness restoreKey={taskB} ready={false} />);
    setScroll(scroller, 300);

    rerender(<Harness restoreKey={taskB} ready />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);
  });

  it("restores again after visiting another task that never became ready", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 410);

    rerender(<Harness restoreKey={taskB} ready={false} />);
    scroller.scrollTop = 0;

    rerender(<Harness restoreKey={taskA} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(410);
  });

  it("retries across animation frames until the restored scrollTop sticks", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 480);

    rerender(<Harness restoreKey={taskB} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    let canScroll = false;
    let storedScrollTop = 0;
    Object.defineProperty(scroller, "scrollTop", {
      configurable: true,
      get: () => storedScrollTop,
      set: (value: number) => {
        storedScrollTop = canScroll ? value : 0;
      },
    });

    rerender(<Harness restoreKey={taskA} />);
    expect(scroller.scrollTop).toBe(0);

    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    canScroll = true;
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(480);
  });

  it("restores again when a virtualized timeline resets scroll after the initial write", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;

    setScroll(scroller, 520);

    rerender(<Harness restoreKey={taskB} />);
    expect(scroller.scrollTop).toBe(0);

    rerender(<Harness restoreKey={taskA} />);
    expect(scroller.scrollTop).toBe(520);

    // Virtuoso initializes after the parent's layout effect and can reset a
    // successful synchronous restore before the next animation frame.
    scroller.scrollTop = 0;
    flushNextAnimationFrame();

    expect(scroller.scrollTop).toBe(520);
  });

  it("cancels a pending restore retry when the task key changes", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 360);

    rerender(<Harness restoreKey={taskB} />);
    flushNextAnimationFrame();

    let storedScrollTop = 0;
    Object.defineProperty(scroller, "scrollTop", {
      configurable: true,
      get: () => storedScrollTop,
      set: () => {
        storedScrollTop = 0;
      },
    });

    rerender(<Harness restoreKey={taskA} />);
    expect(rafCallbacks).toHaveLength(1);

    rerender(<Harness restoreKey={taskB} />);
    expect(cancelledFrameIds).toHaveLength(1);
    expect(rafCallbacks).toHaveLength(0);
  });

  it("keeps the comment deep-link position when its highlight clears", () => {
    const taskA = nextKey("task-a");
    const taskB = nextKey("task-b");

    const { getByTestId, rerender } = render(<Harness restoreKey={taskA} />);
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    setScroll(scroller, 720);

    rerender(<Harness restoreKey={taskB} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    rerender(<Harness restoreKey={taskA} disabled />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(0);

    scroller.scrollTop = 500;

    rerender(<Harness restoreKey={taskA} />);
    flushNextAnimationFrame();
    expect(scroller.scrollTop).toBe(500);
  });

  it("does not yank scroll to top when a same-task comment highlight clears", () => {
    const taskA = nextKey("task-a");

    const { getByTestId, rerender } = render(
      <Harness restoreKey={taskA} disabled />,
    );
    const scroller = getByTestId("scroller") as HTMLElement;
    flushNextAnimationFrame();

    scroller.scrollTop = 500;

    rerender(<Harness restoreKey={taskA} />);
    flushNextAnimationFrame();

    expect(scroller.scrollTop).toBe(500);
  });
});
