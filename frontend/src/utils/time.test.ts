import { describe, expect, it } from "vitest";
import { spelledOutDurationFromMilliseconds } from "./time";

describe("spelledOutDurationFromMilliseconds", () => {
  it.each([
    [0, "0 milliseconds"],
    [1, "1 millisecond"],
    [999, "999 milliseconds"],
    [1_000, "1 second"],
    [3_002, "3 seconds 2 milliseconds"],
    [59_999, "59 seconds 999 milliseconds"],
    [60_000, "1 minute"],
    [61_499, "1 minute 1 second"],
    [61_500, "1 minute 2 seconds"],
    [3_600_000, "1 hour"],
    [3_723_000, "1 hour 2 minutes 3 seconds"],
    [90_061_000, "1 day 1 hour 1 minute 1 second"],
    [2 * 86_400_000 + 5_000, "2 days 5 seconds"],
  ])("%d ms -> %s", (milliseconds, expected) => {
    expect(spelledOutDurationFromMilliseconds(milliseconds)).toBe(expected);
  });
});
