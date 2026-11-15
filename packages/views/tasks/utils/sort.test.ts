import { describe, expect, it } from "vitest";
import type { Task } from "@silieco/core/types";
import { sortTasks } from "./sort";

const propertyId = "prop-effort";

function taskWith(id: string, value?: number | string, position = 0): Task {
  return {
    id,
    position,
    properties: value === undefined ? {} : { [propertyId]: value },
  } as unknown as Task;
}

describe("sortTasks property sorts", () => {
  it("sorts number values numerically, missing values last", () => {
    const sorted = sortTasks(
      [taskWith("big", 10), taskWith("none"), taskWith("small", 2)],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["small", "big", "none"]);
  });

  it("desc reverses values but keeps missing values last", () => {
    const sorted = sortTasks(
      [taskWith("none"), taskWith("small", 2), taskWith("big", 10)],
      `property:${propertyId}`,
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["big", "small", "none"]);
  });

  it("sorts date-only strings chronologically via lexical compare", () => {
    const sorted = sortTasks(
      [taskWith("later", "2026-08-01"), taskWith("earlier", "2026-07-13")],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["earlier", "later"]);
  });

  it("falls back to position order for the static fields", () => {
    const sorted = sortTasks(
      [taskWith("b", undefined, 2), taskWith("a", undefined, 1)],
      "position",
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["a", "b"]);
  });
});
