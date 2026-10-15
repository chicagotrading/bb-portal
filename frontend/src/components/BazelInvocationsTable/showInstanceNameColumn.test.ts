import { expect, test } from "vitest";
import { showInstanceNameColumn } from "./showInstanceNameColumn";

const inv = (name?: string | null) => ({
  instanceName: name === undefined ? null : { name },
});

test("hides the column when every row has the same instance name", () => {
  expect(showInstanceNameColumn([], false)).toBe(false);
  expect(showInstanceNameColumn([inv("a"), inv("a")], false)).toBe(false);
});

test("hides the column when no row has an instance name", () => {
  expect(showInstanceNameColumn([inv(), inv(""), inv(null)], false)).toBe(
    false,
  );
});

test("shows the column when instance names differ", () => {
  expect(showInstanceNameColumn([inv("a"), inv("b")], false)).toBe(true);
  expect(showInstanceNameColumn([inv("a"), inv()], false)).toBe(true);
});

test("shows the column while its filter is active", () => {
  expect(showInstanceNameColumn([], true)).toBe(true);
  expect(showInstanceNameColumn([inv("a"), inv("a")], true)).toBe(true);
});
