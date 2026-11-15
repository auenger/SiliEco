import { expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ChatGroupAvatar } from "./chat-group-avatar";
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => <span data-testid="tile">{actorType}:{actorId}</span> }));
it("combines human and agent identities, deduplicates and caps the mosaic at nine", () => {
  render(<ChatGroupAvatar participants={[
    { type: "member", id: "one" }, { type: "agent", id: "one" }, { type: "member", id: "one" },
    ...Array.from({ length: 10 }, (_, i) => ({ type: "agent" as const, id: `agent-${i}` })),
  ]} />);
  expect(screen.getAllByTestId("tile")).toHaveLength(9);
  expect(screen.getByText("member:one")).toBeInTheDocument();
  expect(screen.getByText("agent:one")).toBeInTheDocument();
});
