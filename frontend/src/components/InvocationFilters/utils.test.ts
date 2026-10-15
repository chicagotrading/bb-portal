import { describe, expect, it } from "vitest";
import {
  describeInvocations,
  type InvocationFilters,
  invocationFilterSearch,
  invocationFiltersWhere,
  parseInvocationFilterSearch,
  refOptions,
} from "./utils";

describe("invocationFiltersWhere", () => {
  const now = new Date("2026-10-13T12:00:00Z");

  it("adds nothing without filters", () => {
    expect(invocationFiltersWhere("all", ["build"], {}, now)).toEqual({
      startedAtNotNil: true,
      commandIn: ["build"],
    });
    expect(invocationFiltersWhere("all", [], { users: [] }, now)).toEqual({
      startedAtNotNil: true,
    });
  });

  it("matches the repo and ref exactly in one source control", () => {
    expect(
      invocationFiltersWhere(
        "24h",
        [],
        { repo: "tsg/nebula", ref: "features/pilot" },
        now,
      ),
    ).toEqual({
      startedAtNotNil: true,
      startedAtGT: "2026-10-12T12:00:00.000Z",
      hasSourceControlWith: [{ repo: "tsg/nebula", ref: "features/pilot" }],
    });
    expect(invocationFiltersWhere("all", [], { ref: "main" }, now)).toEqual({
      startedAtNotNil: true,
      hasSourceControlWith: [{ ref: "main" }],
    });
  });

  it("filters by users", () => {
    expect(
      invocationFiltersWhere("all", [], { users: ["alice", "bob"] }, now),
    ).toEqual({
      startedAtNotNil: true,
      usernameIn: ["alice", "bob"],
    });
  });

  it("treats invocations with a source control repo as CI", () => {
    expect(
      invocationFiltersWhere(
        "all",
        [],
        { ci: true, ref: "features/pilot" },
        now,
      ),
    ).toEqual({
      startedAtNotNil: true,
      hasSourceControlWith: [{ repoNotNil: true, ref: "features/pilot" }],
    });
    expect(
      invocationFiltersWhere("all", [], { ci: false, users: ["alice"] }, now),
    ).toEqual({
      startedAtNotNil: true,
      usernameIn: ["alice"],
      not: { hasSourceControlWith: [{ repoNotNil: true }] },
    });
  });
});

describe("describeInvocations", () => {
  it.each<[InvocationFilters, string]>([
    [{}, "invocations"],
    [{ ci: false }, "non-CI invocations"],
    [
      {
        ci: true,
        repo: "tsg/nebula",
        ref: "features/pilot",
        users: ["a", "b"],
      },
      "CI invocations of tsg/nebula on features/pilot by a, b",
    ],
    [{ users: [] }, "invocations"],
  ])("%j -> %s", (filters, text) => {
    expect(describeInvocations(filters)).toBe(text);
  });
});

describe("refOptions", () => {
  const refs = [
    { repo: "tsg/nebula", ref: "features/pilot", invocations: 5 },
    { repo: "tsg/nebula", ref: "mr-1", invocations: 2 },
    { repo: "ctc/monorepo", ref: "features/pilot", invocations: 1 },
    { repo: "ctc/monorepo", ref: "main", invocations: 3 },
    { repo: null, ref: "local", invocations: 2 },
  ];

  it("scopes refs to the repo", () => {
    expect(refOptions(refs, "ctc/monorepo")).toEqual([
      { value: "main", invocations: 3 },
      { value: "features/pilot", invocations: 1 },
    ]);
  });

  it("adds up a ref across repos", () => {
    expect(refOptions(refs, undefined)).toEqual([
      { value: "features/pilot", invocations: 6 },
      { value: "main", invocations: 3 },
      { value: "local", invocations: 2 },
      { value: "mr-1", invocations: 2 },
    ]);
  });
});

describe("invocation filter search params", () => {
  it("parses valid params", () => {
    expect(
      parseInvocationFilterSearch({
        command: ["build", "test"],
        repo: "tsg/nebula",
        ref: "features/pilot",
        user: ["alice", "bob"],
        ci: true,
        window: "24h",
      }),
    ).toEqual({
      command: ["build", "test"],
      repo: "tsg/nebula",
      ref: "features/pilot",
      user: ["alice", "bob"],
      ci: true,
    });
  });

  it("takes a single user as a string", () => {
    expect(parseInvocationFilterSearch({ user: "alice" })).toEqual({
      user: ["alice"],
    });
  });

  it("drops invalid params", () => {
    expect(
      parseInvocationFilterSearch({
        command: "build",
        repo: "",
        ref: 7,
        user: [],
        ci: "maybe",
      }),
    ).toEqual({
      command: undefined,
      repo: undefined,
      ref: undefined,
      user: undefined,
      ci: undefined,
    });
  });

  it("round trips commands and filters", () => {
    const filters: InvocationFilters = {
      repo: "tsg/nebula",
      ref: "main",
      users: ["alice"],
      ci: true,
    };
    const search = invocationFilterSearch(["build"], filters);
    expect(search).toEqual({
      command: ["build"],
      repo: "tsg/nebula",
      ref: "main",
      user: ["alice"],
      ci: true,
    });
    expect(parseInvocationFilterSearch(search)).toEqual(search);
  });

  it("leaves empty lists out of the URL", () => {
    expect(invocationFilterSearch([], { users: [], ci: false })).toEqual({
      command: undefined,
      repo: undefined,
      ref: undefined,
      user: undefined,
      ci: false,
    });
  });
});
