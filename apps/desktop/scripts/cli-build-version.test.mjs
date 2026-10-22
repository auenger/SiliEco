import { test } from "node:test";
import assert from "node:assert/strict";
import { cliBuildVersion } from "./cli-build-version.mjs";

test("tagless builds retain commit identity and dirty state in dev-describe format", () => {
  assert.equal(cliBuildVersion("e39b4d4", "12"), "v0.0.0-12-ge39b4d4");
  assert.equal(cliBuildVersion("e39b4d4-dirty", "12"), "v0.0.0-12-ge39b4d4-dirty");
});
test("release versions and tagged development builds are unchanged", () => {
  for (const version of ["v0.4.9", "v0.4.10", "v0.5.0-2-gabc1234-dirty"]) {
    assert.equal(cliBuildVersion(version, "12"), version);
  }
});
test("missing or invalid git metadata is not promoted to a supported version", () => {
  assert.equal(cliBuildVersion("", ""), "dev");
  assert.equal(cliBuildVersion("abc1234", ""), "abc1234");
  assert.equal(cliBuildVersion("unknown", "12"), "unknown");
});
