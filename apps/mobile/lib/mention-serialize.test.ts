import { describe, expect, it } from "vitest";
import { insertMention, serializeMentions } from "./mention-serialize";

describe("canonical task mentions", () => {
  it("serializes the selected business task using the Task URI", () => {
    const inserted = insertMention("See @SILI", { start: 4, queryLength: 4 }, {
      type: "task",
      id: "task-1",
      name: "SILI-123",
    });
    expect(serializeMentions(inserted.newText, [inserted.marker])).toBe(
      "See [SILI-123](mention://task/task-1) ",
    );
  });

  it("preserves agent mentions with their distinct identity", () => {
    const inserted = insertMention("@Bot", { start: 0, queryLength: 3 }, {
      type: "agent",
      id: "agent-1",
      name: "Bot",
    });
    expect(serializeMentions(inserted.newText, [inserted.marker])).toBe(
      "[@Bot](mention://agent/agent-1) ",
    );
  });
});
