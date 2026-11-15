import { describe, expect, it } from "vitest";
import {
  preprocessTaskIdentifiers,
  isTaskIdentifier,
} from "@silieco/ui/markdown";

/**
 * Pure detector for the Linear-style task-identifier autolink. Lives in
 * @silieco/ui/markdown (no test runner there), exercised here where views'
 * vitest can reach it.
 */
describe("preprocessTaskIdentifiers", () => {
  it("rewrites a bare identifier into a canonical mention link", () => {
    expect(preprocessTaskIdentifiers("Related to SILI-1745")).toBe(
      "Related to [SILI-1745](mention://task/SILI-1745)",
    );
  });

  it("rewrites multiple identifiers in one string", () => {
    expect(preprocessTaskIdentifiers("Created TES-1 and SILI-2")).toBe(
      "Created [TES-1](mention://task/TES-1) and [SILI-2](mention://task/SILI-2)",
    );
  });

  it("links an identifier at a sentence end (trailing dot + space)", () => {
    expect(preprocessTaskIdentifiers("See SILI-1. Done.")).toBe(
      "See [SILI-1](mention://task/SILI-1). Done.",
    );
  });

  it("links identifiers wrapped in prose punctuation", () => {
    expect(preprocessTaskIdentifiers("(SILI-1) and [SILI-2]")).toContain(
      "([SILI-1](mention://task/SILI-1))",
    );
  });

  // --- skip: code -------------------------------------------------------
  it("skips identifiers inside inline code", () => {
    expect(preprocessTaskIdentifiers("use `SILI-1` here")).toBe(
      "use `SILI-1` here",
    );
  });

  it("skips identifiers inside fenced code blocks", () => {
    const input = "```\nSILI-1 in code\n```";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  // --- skip: existing links / mentions ----------------------------------
  it("does not double-process an existing mention link", () => {
    const input = "[SILI-1](mention://task/00000000-0000-0000-0000-000000000001)";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  it("skips an identifier used as a markdown link label", () => {
    const input = "[SILI-1](https://example.com/x)";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  // --- skip: urls / filenames / paths -----------------------------------
  it("skips an identifier inside a URL", () => {
    const input = "https://example.com/board/SILI-1";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  it("skips a filename token like ABC-123.ts", () => {
    const input = "open ABC-123.ts now";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  it("skips a path segment like FOO-1/bar", () => {
    const input = "path FOO-1/bar/baz";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  // --- non-matches ------------------------------------------------------
  it("ignores lowercase tokens", () => {
    const input = "some-word-1 and sili-1";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  it("ignores a token embedded in a larger word", () => {
    const input = "XSILI-1A stays";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });

  it("returns input unchanged when no candidates exist", () => {
    const input = "plain text with no identifiers";
    expect(preprocessTaskIdentifiers(input)).toBe(input);
  });
});

describe("isTaskIdentifier", () => {
  it("accepts a bare identifier", () => {
    expect(isTaskIdentifier("SILI-1745")).toBe(true);
    expect(isTaskIdentifier("TES-1")).toBe(true);
  });

  it("rejects a UUID (so real mentions are not treated as identifiers)", () => {
    expect(isTaskIdentifier("00000000-0000-0000-0000-000000000001")).toBe(
      false,
    );
  });

  it("rejects lowercase and malformed tokens", () => {
    expect(isTaskIdentifier("sili-1")).toBe(false);
    expect(isTaskIdentifier("SILI-")).toBe(false);
    expect(isTaskIdentifier("MUL1")).toBe(false);
  });
});
