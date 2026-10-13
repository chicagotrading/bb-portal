import { describe, expect, it } from "vitest";
import {
  ActionChangeCause,
  ActionInputGroupKind,
} from "@/graphql/__generated__/graphql";
import { DigestFunction_Value } from "@/lib/grpc-client/build/bazel/remote/execution/v2/remote_execution";
import { invocationWindowWhere } from "@/utils/invocationWindows";
import { compareActionLink } from "./compare";
import {
  avgDuration,
  CHURN_FIELDS,
  type ChurnField,
  causeSegments,
  causeSummary,
  changesPerAction,
  coverageParts,
  describeInvocations,
  emptyReason,
  executionsPerAction,
  fitText,
  formatPercent,
  formatRate,
  groupKind,
  groupTargetLabel,
  groupText,
  HEAT_COLORS,
  type HeatTreeNode,
  heatColor,
  heatLeaves,
  heatScale,
  heatTree,
  historyPoints,
  IMPACT_FIELDS,
  type InsightsFilters,
  insightsWhere,
  type MapRow,
  mapCells,
  queueShare,
  refOptions,
  splitLabel,
  splitOutputPath,
  textColorOn,
  truncatedNote,
  zoomTrail,
} from "./utils";

describe("groupKind", () => {
  it.each([
    ["cmd", ActionInputGroupKind.Command],
    ["src:cpp/foo/foo.cc", ActionInputGroupKind.Source],
    [
      "src:bazel-out/k8-opt/bin/cpp/foo/foo.cppmap",
      ActionInputGroupKind.Source,
    ],
    ["//cpp/baz (sources)", ActionInputGroupKind.Source],
    ["@ctc_bazel_tools++conda+env", ActionInputGroupKind.External],
    ["//cpp/foo (k8-opt)", ActionInputGroupKind.Generated],
    ["//cpp/foo:test (k8-opt)", ActionInputGroupKind.Generated],
    ["@@openssl+//:ssl", ActionInputGroupKind.Generated],
    ["bazel-out:volatile-status.txt", ActionInputGroupKind.Generated],
  ])("%s -> %s", (name, kind) => {
    expect(groupKind(name)).toBe(kind);
  });
});

describe("groupText and groupTargetLabel", () => {
  it.each([
    ["cmd", "command line", undefined],
    ["src:cpp/foo/foo.cc", "cpp/foo/foo.cc", undefined],
    ["//cpp/baz (sources)", "//cpp/baz (sources)", undefined],
    ["@repo", "@repo", undefined],
    // Package groups link to the targets of the package.
    ["//cpp/foo (k8-opt)", "//cpp/foo (k8-opt)", "//cpp/foo:"],
    ["// (k8-opt)", "// (k8-opt)", "//:"],
    ["//cpp/foo:test (k8-opt)", "//cpp/foo:test (k8-opt)", "//cpp/foo:test"],
    ["@@openssl+//:ssl", "@@openssl+//:ssl", "@@openssl+//:ssl"],
    ["bazel-out:k8-opt/bin/x", "bazel-out:k8-opt/bin/x", undefined],
  ])("%s", (name, text, label) => {
    expect(groupText(name)).toBe(text);
    expect(groupTargetLabel(name)).toBe(label);
  });
});

describe("insightsWhere", () => {
  const now = new Date("2026-10-13T12:00:00Z");

  it("adds nothing without filters", () => {
    expect(insightsWhere("all", ["build"], {}, now)).toEqual({
      startedAtNotNil: true,
      commandIn: ["build"],
    });
    expect(insightsWhere("all", [], { users: [] }, now)).toEqual({
      startedAtNotNil: true,
    });
  });

  it("matches the repo and ref exactly in one source control", () => {
    expect(
      insightsWhere(
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
    expect(insightsWhere("all", [], { ref: "main" }, now)).toEqual({
      startedAtNotNil: true,
      hasSourceControlWith: [{ ref: "main" }],
    });
  });

  it("filters by users", () => {
    expect(insightsWhere("all", [], { users: ["alice", "bob"] }, now)).toEqual({
      startedAtNotNil: true,
      usernameIn: ["alice", "bob"],
    });
  });

  it("treats invocations with a source control repo as CI", () => {
    expect(
      insightsWhere("all", [], { ci: true, ref: "features/pilot" }, now),
    ).toEqual({
      startedAtNotNil: true,
      hasSourceControlWith: [{ repoNotNil: true, ref: "features/pilot" }],
    });
    expect(
      insightsWhere("all", [], { ci: false, users: ["alice"] }, now),
    ).toEqual({
      startedAtNotNil: true,
      usernameIn: ["alice"],
      not: { hasSourceControlWith: [{ repoNotNil: true }] },
    });
  });
});

describe("describeInvocations", () => {
  it.each<[InsightsFilters, string]>([
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

describe("coverage", () => {
  const coverage = {
    invocationsMatched: 100,
    invocationsAnalyzed: 40,
    invocationsPending: 6,
    invocationsFailed: 2,
    invocationsWithoutLog: 52,
  };

  it("lists the states of the matched invocations", () => {
    expect(coverageParts(coverage)).toEqual([
      "40 analyzed",
      "6 waiting for ingestion",
      "2 failed to ingest",
      "52 without an execution log",
    ]);
    expect(
      coverageParts({
        ...coverage,
        invocationsPending: 0,
        invocationsFailed: 0,
        invocationsWithoutLog: 0,
      }),
    ).toEqual(["40 analyzed"]);
  });

  it.each([
    ["analyzed", {}, undefined],
    [
      "no matches",
      { invocationsMatched: 0, invocationsAnalyzed: 0 },
      "noMatches",
    ],
    [
      "logs waiting",
      { invocationsAnalyzed: 0, invocationsFailed: 0 },
      "notIngested",
    ],
    [
      "logs failed",
      { invocationsAnalyzed: 0, invocationsPending: 0 },
      "notIngested",
    ],
    [
      "no logs",
      { invocationsAnalyzed: 0, invocationsPending: 0, invocationsFailed: 0 },
      "noLogs",
    ],
  ])("explains %s", (_, override, reason) => {
    expect(emptyReason({ ...coverage, ...override })).toBe(reason);
  });
});

describe("truncatedNote", () => {
  it("is set only when the server's row limit was reached", () => {
    expect(truncatedNote(499, 500, "total time")).toBeUndefined();
    expect(truncatedNote(500, 500, "total time")).toContain(
      "top 500 by total time",
    );
    expect(truncatedNote(500, 500, "x", "the map covers only these")).toBe(
      "Showing the top 500 by x; the map covers only these.",
    );
  });
});

describe("splitOutputPath", () => {
  it.each([
    [
      "bazel-out/k8-fastbuild/bin/cpp/foo/libfoo.a",
      { config: "k8-fastbuild", path: "cpp/foo/libfoo.a" },
    ],
    [
      "bazel-out/k8-opt-exec-ST-1a2b/bin/tools/gen",
      { config: "k8-opt-exec-ST-1a2b", path: "tools/gen" },
    ],
    [
      "bazel-out/k8-fastbuild/testlogs/foo/test.xml",
      { config: "k8-fastbuild", path: "testlogs/foo/test.xml" },
    ],
    ["external/repo/file", { path: "external/repo/file" }],
    ["", { path: "" }],
  ])("%s", (path, expected) => {
    expect(splitOutputPath(path)).toEqual(expected);
  });
});

describe("causeSegments", () => {
  it("skips empty causes and sums to 100", () => {
    expect(
      causeSegments({ direct: 1, both: 0, dependency: 3, unknown: 0 }),
    ).toEqual([
      { cause: ActionChangeCause.Direct, count: 1, percent: 25 },
      { cause: ActionChangeCause.Dependency, count: 3, percent: 75 },
    ]);
  });

  it("returns nothing without changes", () => {
    expect(
      causeSegments({ direct: 0, both: 0, dependency: 0, unknown: 0 }),
    ).toEqual([]);
  });

  it("summarizes the counts as text", () => {
    expect(
      causeSummary(
        causeSegments({ direct: 1, both: 0, dependency: 3, unknown: 2 }),
      ),
    ).toBe("1 direct, 3 via dependency, 2 unknown");
  });
});

describe("historyPoints", () => {
  it("sorts by time and drops entries without a start", () => {
    expect(
      historyPoints([
        {
          invocationId: "b",
          startedAt: "2026-10-02T00:00:00Z",
          changed: true,
          cause: ActionChangeCause.Direct,
          execution: { totalMs: 1500 },
        },
        { invocationId: "x", startedAt: null, changed: false },
        {
          invocationId: "a",
          startedAt: "2026-10-01T00:00:00Z",
          changed: false,
          cause: null,
          execution: null,
        },
      ]),
    ).toEqual([
      {
        time: Date.parse("2026-10-01T00:00:00Z"),
        invocationId: "a",
        durationMs: undefined,
        changed: false,
        cause: undefined,
      },
      {
        time: Date.parse("2026-10-02T00:00:00Z"),
        invocationId: "b",
        durationMs: 1500,
        changed: true,
        cause: ActionChangeCause.Direct,
      },
    ]);
  });
});

describe("compareActionLink", () => {
  const entry = {
    instanceName: "nebula",
    digestFunction: "sha256",
    digest: { hash: "b".repeat(64), sizeBytes: 140 },
    previousDigest: { hash: "a".repeat(64), sizeBytes: 138 },
  };

  it("opens the previous action compared with the new one", () => {
    expect(compareActionLink(entry)).toEqual({
      params: {
        _splat: `nebula/blobs/sha256/action/${"a".repeat(64)}-138`,
      },
      search: {
        comparedAction: {
          instanceName: "nebula",
          digestFunction: DigestFunction_Value.SHA256,
          digest: { hash: "b".repeat(64), sizeBytes: "140" },
        },
      },
    });
  });

  it("omits the instance name prefix when empty", () => {
    expect(
      compareActionLink({ ...entry, instanceName: "" })?.params._splat,
    ).toBe(`blobs/sha256/action/${"a".repeat(64)}-138`);
  });

  it.each([
    ["no previous digest", { previousDigest: null }],
    ["same digest", { previousDigest: entry.digest }],
    ["a computed digest", { digest: { hash: "c".repeat(64), sizeBytes: 0 } }],
    ["unknown digest function", { digestFunction: "nope" }],
  ])("is undefined with %s", (_, override) => {
    expect(compareActionLink({ ...entry, ...override })).toBeUndefined();
  });
});

describe("invocationWindowWhere", () => {
  const now = new Date("2026-10-13T12:00:00Z");

  it("filters only by start without a window or commands", () => {
    expect(invocationWindowWhere("all", [], now)).toEqual({
      startedAtNotNil: true,
    });
  });

  it("adds the window start and commands", () => {
    expect(invocationWindowWhere("24h", ["build", "test"], now)).toEqual({
      startedAtNotNil: true,
      startedAtGT: "2026-10-12T12:00:00.000Z",
      commandIn: ["build", "test"],
    });
  });
});

const churnRow = (
  keyId: string,
  targetLabel: string,
  changes: number,
  timeMs: number,
  mnemonic = "CppCompile",
) => ({
  keyId,
  targetLabel,
  mnemonic,
  primaryOutput: `bazel-out/k8-opt/bin/${keyId}.o`,
  changes,
  newVersions: changes,
  direct: changes,
  dependency: 0,
  both: 0,
  unknown: 0,
  executedAfterChange: changes,
  timeMs,
});

const churnTree = (rows: ReturnType<typeof churnRow>[]) =>
  heatTree(rows, CHURN_FIELDS);

type ChurnNode = HeatTreeNode<MapRow, ChurnField>;

const CHURN_SIZES = {
  time: (n: ChurnNode) => n.sums.timeMs,
  changes: (n: ChurnNode) => n.sums.changes,
};

describe("splitLabel", () => {
  it.each([
    ["//cpp/foo:bar", "", ["cpp", "foo"], "bar"],
    ["//cpp/foo", "", ["cpp", "foo"], "foo"],
    ["//:root", "", [], "root"],
    ["@@openssl+//:ssl", "@@openssl+", [], "ssl"],
    ["@repo//a/b:t", "@repo", ["a", "b"], "t"],
    ["", "", [], ""],
  ])("%s", (label, repo, packages, target) => {
    expect(splitLabel(label)).toEqual({ repo, packages, target });
  });
});

describe("heatTree", () => {
  const tree = churnTree([
    churnRow("a1", "//cpp/trading_services/a:a", 4, 1000),
    churnRow("a2", "//cpp/trading_services/a:a", 2, 3000, "CppLink"),
    churnRow("b1", "//cpp/trading_services/b:b", 1, 500),
    churnRow("c1", "//py/c:c", 6, 0),
    churnRow("x1", "@@openssl+//:ssl", 3, 200),
  ]);

  it("sums the fields at every level", () => {
    expect(tree.actions).toBe(5);
    expect(tree.sums).toMatchObject({ changes: 16, timeMs: 4700 });
    const cpp = tree.children.find((c) => c.name === "cpp/trading_services");
    expect(cpp?.actions).toBe(3);
    expect(cpp?.sums).toMatchObject({ changes: 7, timeMs: 4500 });
  });

  it("collapses single child chains", () => {
    expect(tree.children.map((c) => [c.name, c.kind, c.path])).toEqual([
      ["cpp/trading_services", "package", "//cpp/trading_services"],
      ["py/c:c CppCompile", "action", "bazel-out/k8-opt/bin/c1.o"],
      ["@@openssl+//:ssl CppCompile", "action", "bazel-out/k8-opt/bin/x1.o"],
    ]);
    const cpp = tree.children[0];
    expect(cpp.children.map((c) => [c.name, c.kind])).toEqual([
      ["a:a", "target"],
      ["b:b CppCompile", "action"],
    ]);
    expect(cpp.children[0].children.map((c) => c.name)).toEqual([
      "CppCompile a1.o",
      "CppLink a2.o",
    ]);
    expect(cpp.children[1].row?.keyId).toBe("b1");
  });

  it("keeps packages that hold both targets and packages", () => {
    const t = churnTree([
      churnRow("p", "//a:p", 1, 1),
      churnRow("q1", "//a/b:q", 1, 1),
      churnRow("q2", "//a/b:q", 1, 1),
    ]);
    // A single top level package is reached by zoomTrail, not merged away.
    expect(t.children.map((c) => c.name)).toEqual(["a"]);
    expect(t.children[0].children.map((c) => c.name)).toEqual([
      ":p CppCompile",
      "b:q",
    ]);
  });

  it("lists the leaves", () => {
    expect(heatLeaves(tree).map((n) => n.row?.keyId)).toEqual([
      "a1",
      "a2",
      "b1",
      "c1",
      "x1",
    ]);
  });
});

describe("zoomTrail", () => {
  const tree = churnTree([
    churnRow("p", "//a:p", 1, 1),
    churnRow("q1", "//a/b:q", 1, 1),
    churnRow("q2", "//a/b:q", 1, 1),
  ]);

  it("skips through nodes holding a single node", () => {
    expect(zoomTrail(tree, []).map((n) => n.name)).toEqual(["All", "a"]);
  });

  it("follows ids and stops at unknown ones and leaves", () => {
    expect(
      zoomTrail(tree, ["pkg://a", "target://a/b:q"]).map((n) => n.name),
    ).toEqual(["All", "a", "b:q"]);
    expect(zoomTrail(tree, ["pkg://a", "nope"]).map((n) => n.name)).toEqual([
      "All",
      "a",
    ]);
    expect(zoomTrail(tree, ["pkg://a", "p"]).map((n) => n.name)).toEqual([
      "All",
      "a",
    ]);
  });
});

describe("mapCells", () => {
  const tree = churnTree([
    churnRow("a", "//a:a", 4, 0),
    churnRow("b", "//b:b", 1, 2000),
    churnRow("c1", "//c:c", 3, 0),
    churnRow("c2", "//c:c", 1, 0),
  ]);
  const sizes = { ...CHURN_SIZES, actions: (n: ChurnNode) => n.actions };

  it("sizes by the metric and leaves out zero sizes", () => {
    expect(mapCells(tree, sizes, "time")).toEqual({
      cells: [{ id: "b", name: "b:b CppCompile", value: 2000 }],
      size: "time",
      hidden: 2,
    });
  });

  it("orders cells by size, then name", () => {
    const { cells } = mapCells(tree, sizes, "actions");
    expect(cells.map((c) => [c.name, c.value])).toEqual([
      ["c:c", 2],
      ["a:a CppCompile", 1],
      ["b:b CppCompile", 1],
    ]);
  });

  it("falls back to other sizes, then action count", () => {
    const noTime = churnTree([
      churnRow("a", "//a:a", 1, 0),
      churnRow("b", "//b:b", 2, 0),
    ]);
    expect(mapCells(noTime, CHURN_SIZES, "time")).toMatchObject({
      size: "changes",
      hidden: 0,
      cells: [{ value: 2 }, { value: 1 }],
    });
    const nothing = churnTree([
      churnRow("a", "//a:a", 0, 0),
      churnRow("b", "//b:b", 0, 0),
    ]);
    expect(mapCells(nothing, CHURN_SIZES, "time")).toMatchObject({
      size: null,
      cells: [{ value: 1 }, { value: 1 }],
    });
  });
});

describe("heat metrics", () => {
  it("computes changes per action", () => {
    const tree = churnTree([
      churnRow("c1", "//c:c", 3, 0),
      churnRow("c2", "//c:c", 1, 0),
    ]);
    expect(changesPerAction(tree)).toBe(2);
  });

  it("computes impact heats, zero without a denominator", () => {
    const row = (keyId: string, executions: number, totalMs: number) => ({
      keyId,
      targetLabel: "//a:a",
      mnemonic: "CppCompile",
      primaryOutput: keyId,
      executions,
      totalMs,
      execWallMs: totalMs,
      queueMs: totalMs / 4,
      share: 0.1,
    });
    const tree = heatTree(
      [row("x", 3, 3000), row("y", 1, 5000)],
      IMPACT_FIELDS,
    );
    expect(avgDuration(tree)).toBe(2000);
    expect(executionsPerAction(tree)).toBe(2);
    expect(queueShare(tree)).toBe(0.25);
    expect(tree.sums.share).toBeCloseTo(0.2);
    const idle = heatTree([row("z", 0, 0)], IMPACT_FIELDS);
    expect(avgDuration(idle)).toBe(0);
    expect(queueShare(idle)).toBe(0);
  });
});

describe("heat colors", () => {
  it("interpolates from cold to hot and clamps", () => {
    expect(heatColor(0)).toBe(HEAT_COLORS[0]);
    expect(heatColor(1)).toBe(HEAT_COLORS[HEAT_COLORS.length - 1]);
    expect(heatColor(-1)).toBe(HEAT_COLORS[0]);
    expect(heatColor(2)).toBe(HEAT_COLORS[HEAT_COLORS.length - 1]);
    expect(heatColor(Number.NaN)).toBe(HEAT_COLORS[0]);
    expect(heatColor(0.5)).toBe(HEAT_COLORS[4]);
    // Halfway between #ffffcc and #ffeda0.
    expect(heatColor(1 / 16)).toBe("#fff6b6");
  });

  it("scales linearly over a narrow range like prod churn", () => {
    // Prod: 22 invocations, the top churn rows all change 13 to 20 times.
    const scale = heatScale([13, 14, 15, 16, 17, 19, 20]);
    expect(scale).toMatchObject({ min: 13, max: 20, log: false });
    expect(scale.at(13)).toBe(0);
    expect(scale.at(16.5)).toBe(0.5);
    expect(scale.at(20)).toBe(1);
    expect(scale.at(25)).toBe(1);
    expect(scale.at(1)).toBe(0);
    expect(heatColor(scale.at(14))).not.toBe(heatColor(scale.at(16)));
  });

  it("spreads prod-like churn over the colors across a whole tree", () => {
    const tree = churnTree(
      [13, 14, 15, 16, 17, 19, 20].map((changes, i) =>
        churnRow(`k${i}`, `//p${i % 3}:t${i}`, changes, 1),
      ),
    );
    const scale = heatScale(heatLeaves(tree).map(changesPerAction));
    expect(scale).toMatchObject({ min: 13, max: 20 });
    const colors = new Set(
      heatLeaves(tree).map((n) => heatColor(scale.at(changesPerAction(n)))),
    );
    expect(colors.size).toBe(7);
  });

  it("scales on a log scale when values span 10x or more", () => {
    const scale = heatScale([1, 10, 100]);
    expect(scale).toMatchObject({ min: 1, max: 100, log: true });
    expect(scale.at(10)).toBeCloseTo(0.5);
    expect(scale.at(0)).toBe(0);
  });

  it("stays linear with zeros and handles degenerate domains", () => {
    const withZero = heatScale([0, 0.5, 100]);
    expect(withZero.log).toBe(false);
    expect(withZero.at(50)).toBe(0.5);
    expect(heatScale([5, 5]).at(5)).toBe(0.5);
    expect(heatScale([]).at(5)).toBe(0.5);
    expect(heatScale([Number.NaN, 2, 4]).min).toBe(2);
  });

  it("picks text that contrasts with the fill", () => {
    expect(textColorOn(HEAT_COLORS[0])).toBe("rgba(0, 0, 0, 0.88)");
    expect(textColorOn(HEAT_COLORS[4])).toBe("rgba(0, 0, 0, 0.88)");
    expect(textColorOn(HEAT_COLORS[7])).toBe("#ffffff");
    expect(textColorOn(HEAT_COLORS[8])).toBe("#ffffff");
  });
});

describe("text helpers", () => {
  it("cuts text to the width and hides it when too narrow", () => {
    expect(fitText("abcdef", 70)).toBe("abcdef");
    expect(fitText("abcdefghijkl", 70)).toBe("abcdefghi…");
    expect(fitText("abcdef", 14)).toBe("");
  });

  it("rounds rates and percentages", () => {
    expect(formatRate(1)).toBe("1");
    expect(formatRate(2.345)).toBe("2.3");
    expect(formatRate(12.6)).toBe("13");
    expect(formatPercent(0.25)).toBe("25%");
    expect(formatPercent(0.0123)).toBe("1.2%");
    expect(formatPercent(0)).toBe("0%");
  });
});
