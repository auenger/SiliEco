import { afterEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";
import { PROTOCOL_GENERATION } from "../protocol";

class FakeWebSocket {
  static OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readyState = 1;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  send = vi.fn();
  close = vi.fn();

  constructor(public url: string) {
    FakeWebSocket.instances.push(this);
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
  FakeWebSocket.instances = [];
});

describe("mobile realtime contract", () => {
  it("declares the protocol generation and distinguishes task and run events", () => {
    vi.stubGlobal("WebSocket", FakeWebSocket);
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "test-token",
      workspaceSlug: "team",
    });
    const onTask = vi.fn();
    const onRun = vi.fn();
    client.on("task:updated", onTask);
    client.on("run:message", onRun);
    client.connect();

    const socket = FakeWebSocket.instances[0];
    const url = new URL(socket.url);
    expect(url.searchParams.get("protocol_generation")).toBe(PROTOCOL_GENERATION);
    expect(url.searchParams.get("workspace_slug")).toBe("team");
    socket.onopen?.();
    expect(JSON.parse(socket.send.mock.calls[0][0])).toEqual({
      type: "auth",
      payload: { token: "test-token", protocol_generation: PROTOCOL_GENERATION },
    });

    socket.onmessage?.({ data: JSON.stringify({ type: "auth_ack", payload: {} }) });
    socket.onmessage?.({
      data: JSON.stringify({
        type: "run:message",
        payload: { run_id: "run-1", task_id: "task-1", seq: 1 },
      }),
    });
    expect(onRun).toHaveBeenCalledTimes(1);
    expect(onTask).not.toHaveBeenCalled();
    expect(onRun.mock.calls[0][0]).toMatchObject({ run_id: "run-1", task_id: "task-1" });
    client.disconnect();
  });
});
