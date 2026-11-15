// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import {
  myTasksRelationFromScope,
  type MyTasksScope,
} from "./my-tasks-view-store";

describe("myTasksRelationFromScope", () => {
  it.each([
    ["all", "all"],
    ["assigned", "assigned"],
    ["created", "created"],
    ["agents", "involved"],
  ] satisfies Array<[MyTasksScope, string]>)(
    "maps %s to the surface relation %s",
    (scope, relation) => {
      expect(myTasksRelationFromScope(scope)).toBe(relation);
    },
  );
});
