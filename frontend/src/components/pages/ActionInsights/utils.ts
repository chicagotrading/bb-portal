import { blue, gold, grey, purple } from "@ant-design/colors";
import {
  ActionChangeCause,
  ActionInputGroupKind,
} from "@/graphql/__generated__/graphql";

export const ACTION_INSIGHTS_TABS = ["impact", "churn", "causes"] as const;
export type ActionInsightsTab = (typeof ACTION_INSIGHTS_TABS)[number];

export const CAUSES: Record<
  ActionChangeCause,
  { label: string; color: string; description: string }
> = {
  [ActionChangeCause.Direct]: {
    label: "Direct",
    color: blue.primary ?? blue[5],
    description:
      "Only its own command line, param files or sources in its package changed",
  },
  [ActionChangeCause.Dependency]: {
    label: "Via dependency",
    color: purple.primary ?? purple[5],
    description:
      "Only outputs of other actions (its own included), sources of other packages or external repositories changed",
  },
  [ActionChangeCause.Both]: {
    label: "Both",
    color: gold.primary ?? gold[5],
    description: "Both its own inputs and inputs of dependencies changed",
  },
  [ActionChangeCause.Unknown]: {
    label: "Unknown",
    color: grey[5],
    description: "The digest changed but no input fingerprint did",
  },
};

export const GROUP_KINDS: Record<
  ActionInputGroupKind,
  { label: string; tagColor: string }
> = {
  [ActionInputGroupKind.Command]: { label: "Command", tagColor: "magenta" },
  [ActionInputGroupKind.Source]: { label: "Source", tagColor: "green" },
  [ActionInputGroupKind.Generated]: {
    label: "Generated",
    tagColor: "geekblue",
  },
  [ActionInputGroupKind.External]: { label: "External", tagColor: "gold" },
};

const SOURCES_SUFFIX = " (sources)";

// Mirrors the backend naming in pkg/execlog.
export const groupKind = (name: string): ActionInputGroupKind => {
  if (name === "cmd") return ActionInputGroupKind.Command;
  if (name.startsWith("src:") || name.endsWith(SOURCES_SUFFIX)) {
    return ActionInputGroupKind.Source;
  }
  if (name.startsWith("@") && !name.includes("//")) {
    return ActionInputGroupKind.External;
  }
  return ActionInputGroupKind.Generated;
};

export const groupText = (name: string): string => {
  if (name === "cmd") return "command line";
  if (name.startsWith("src:")) return name.slice("src:".length);
  return name;
};

// The targets page filter for a generated group: its target for test logs
// ("//pkg:test (cfg)"), else the targets of its package ("//pkg (cfg)").
export const groupTargetLabel = (name: string): string | undefined => {
  if (
    !name.includes("//") ||
    name.endsWith(SOURCES_SUFFIX) ||
    name.startsWith("bazel-out:")
  ) {
    return undefined;
  }
  const label = name.split(" ")[0];
  return label.includes(":") ? label : `${label}:`;
};

export const splitOutputPath = (
  path: string,
): { config?: string; path: string } => {
  const match = /^bazel-out\/([^/]+)\/(?:bin\/)?(.*)$/.exec(path);
  return match ? { config: match[1], path: match[2] } : { path };
};

export interface CauseSegment {
  cause: ActionChangeCause;
  count: number;
  percent: number;
}

export const causeSegments = (counts: {
  direct: number;
  both: number;
  dependency: number;
  unknown: number;
}): CauseSegment[] => {
  const entries: [ActionChangeCause, number][] = [
    [ActionChangeCause.Direct, counts.direct],
    [ActionChangeCause.Both, counts.both],
    [ActionChangeCause.Dependency, counts.dependency],
    [ActionChangeCause.Unknown, counts.unknown],
  ];
  const total = entries.reduce((sum, [, count]) => sum + count, 0);
  if (total === 0) return [];
  return entries
    .filter(([, count]) => count > 0)
    .map(([cause, count]) => ({
      cause,
      count,
      percent: (100 * count) / total,
    }));
};

export interface InsightsCoverage {
  invocationsMatched: number;
  invocationsAnalyzed: number;
  invocationsPending: number;
  invocationsFailed: number;
  invocationsWithoutLog: number;
}

// How the matched invocations split up by the state of their log.
export const coverageParts = (c: InsightsCoverage): string[] =>
  [
    `${c.invocationsAnalyzed} analyzed`,
    c.invocationsPending > 0 && `${c.invocationsPending} waiting for ingestion`,
    c.invocationsFailed > 0 && `${c.invocationsFailed} failed to ingest`,
    c.invocationsWithoutLog > 0 &&
      `${c.invocationsWithoutLog} without an execution log`,
  ].filter((part): part is string => typeof part === "string");

export type EmptyReason = "noMatches" | "notIngested" | "noLogs";

// Why nothing can be analyzed, or undefined if something can.
export const emptyReason = (c: InsightsCoverage): EmptyReason | undefined => {
  if (c.invocationsAnalyzed > 0) return undefined;
  if (c.invocationsMatched === 0) return "noMatches";
  if (c.invocationsPending > 0 || c.invocationsFailed > 0) {
    return "notIngested";
  }
  return "noLogs";
};

// Whether a table shows only the top rows the server returned.
export const truncatedNote = (
  rows: number,
  rowLimit: number,
  metric: string,
  effect = "sorting by another column only reorders these",
): string | undefined =>
  rows >= rowLimit
    ? `Showing the top ${rowLimit} by ${metric}; ${effect}.`
    : undefined;

// The counts of a cause bar as text, e.g. "3 direct, 5 via dependency".
export const causeSummary = (segments: CauseSegment[]): string =>
  segments
    .map(
      (segment) =>
        `${segment.count} ${CAUSES[segment.cause].label.toLowerCase()}`,
    )
    .join(", ");

export interface HistoryPoint {
  time: number;
  invocationId: string;
  durationMs?: number;
  changed: boolean;
  cause?: ActionChangeCause;
}

export const historyPoints = (
  entries: {
    invocationId: string;
    startedAt?: string | null;
    changed: boolean;
    cause?: ActionChangeCause | null;
    execution?: { totalMs: number } | null;
  }[],
): HistoryPoint[] =>
  entries
    .filter((entry) => entry.startedAt)
    .map((entry) => ({
      time: new Date(entry.startedAt as string).getTime(),
      invocationId: entry.invocationId,
      durationMs: entry.execution?.totalMs,
      changed: entry.changed,
      cause: entry.cause ?? undefined,
    }))
    .sort((a, b) => a.time - b.time);

export const MAP_VIEWS = ["table", "map"] as const;
export type MapView = (typeof MAP_VIEWS)[number];

// The fields of a row that place it in the map.
export interface MapRow {
  keyId: string;
  targetLabel: string;
  mnemonic: string;
  primaryOutput: string;
}

export interface HeatTreeNode<
  R extends MapRow = MapRow,
  K extends string = string,
> {
  id: string;
  name: string;
  // A package ("//cpp/foo"), a target label, or the output of an action.
  path: string;
  kind: "package" | "target" | "action";
  children: HeatTreeNode<R, K>[];
  row?: R;
  actions: number;
  // The fields of the rows below, summed.
  sums: Record<K, number>;
}

// "@repo//a/b:t" -> repo "@repo", packages ["a", "b"], target "t".
export const splitLabel = (
  label: string,
): { repo: string; packages: string[]; target: string } => {
  const slashes = label.indexOf("//");
  const repo = slashes > 0 ? label.slice(0, slashes) : "";
  const rest = slashes >= 0 ? label.slice(slashes + 2) : label;
  const colon = rest.lastIndexOf(":");
  const pkg = colon >= 0 ? rest.slice(0, colon) : rest;
  const packages = pkg.split("/").filter((part) => part !== "");
  const target = colon >= 0 ? rest.slice(colon + 1) : (packages.at(-1) ?? "");
  return { repo, packages, target };
};

const joinNames = (parent: string, child: string): string =>
  parent.endsWith("/") || child.startsWith(":")
    ? `${parent}${child}`
    : `${parent}/${child}`;

// Folds a node with a single child into it: "cpp" > "foo" > ":bar" becomes
// "cpp/foo:bar", and a target with one action becomes that action.
const collapse = <R extends MapRow, K extends string>(
  node: HeatTreeNode<R, K>,
): HeatTreeNode<R, K> => {
  const children = node.children.map(collapse);
  const only = children.length === 1 ? children[0] : undefined;
  if (only && node.kind === "package") {
    return { ...only, name: joinNames(node.name, only.name) };
  }
  if (only?.row && node.kind === "target") {
    return { ...only, name: `${node.name} ${only.row.mnemonic}` };
  }
  return { ...node, children };
};

const actionName = (mnemonic: string, primaryOutput: string): string =>
  primaryOutput === ""
    ? `${mnemonic} (no outputs)`
    : `${mnemonic} ${primaryOutput.split("/").at(-1)}`;

// Packages, then targets, then their actions, with fields summed up the
// tree.
export const heatTree = <
  K extends string,
  R extends MapRow & Record<K, number>,
>(
  rows: R[],
  fields: readonly K[],
): HeatTreeNode<R, K> => {
  const make = (
    id: string,
    name: string,
    path: string,
    kind: HeatTreeNode["kind"],
  ): HeatTreeNode<R, K> => ({
    id,
    name,
    path,
    kind,
    children: [],
    actions: 0,
    sums: Object.fromEntries(fields.map((f) => [f, 0])) as Record<K, number>,
  });
  const add = (node: HeatTreeNode<R, K>, row: R) => {
    node.actions += 1;
    for (const f of fields) node.sums[f] += row[f];
  };
  const root = make("", "All", "", "package");
  const index = new Map<string, HeatTreeNode<R, K>>();
  const child = (
    parent: HeatTreeNode<R, K>,
    id: string,
    name: string,
    path: string,
    kind: HeatTreeNode["kind"],
  ) => {
    let node = index.get(id);
    if (!node) {
      node = make(id, name, path, kind);
      index.set(id, node);
      parent.children.push(node);
    }
    return node;
  };
  for (const row of rows) {
    const { repo, packages, target } = splitLabel(row.targetLabel);
    add(root, row);
    let node = root;
    let path = `${repo}//`;
    if (repo !== "") {
      node = child(node, `pkg:${path}`, path, path, "package");
      add(node, row);
    }
    for (const [i, part] of packages.entries()) {
      path = i === 0 ? `${path}${part}` : `${path}/${part}`;
      node = child(node, `pkg:${path}`, part, path, "package");
      add(node, row);
    }
    node = child(
      node,
      `target:${row.targetLabel}`,
      target === "" ? "(no target)" : `:${target}`,
      row.targetLabel,
      "target",
    );
    add(node, row);
    const leaf = make(
      row.keyId,
      actionName(row.mnemonic, row.primaryOutput),
      row.primaryOutput,
      "action",
    );
    leaf.row = row;
    add(leaf, row);
    node.children.push(leaf);
  }
  return { ...root, children: root.children.map(collapse) };
};

export const heatLeaves = <R extends MapRow, K extends string>(
  node: HeatTreeNode<R, K>,
): HeatTreeNode<R, K>[] =>
  node.children.length === 0 ? [node] : node.children.flatMap(heatLeaves);

// The nodes from the root down to the zoomed one, then on through nodes
// that hold a single node. The walk stops at ids it cannot find.
export const zoomTrail = <R extends MapRow, K extends string>(
  root: HeatTreeNode<R, K>,
  ids: string[],
): HeatTreeNode<R, K>[] => {
  const trail = [root];
  for (const id of ids) {
    const next = trail[trail.length - 1].children.find((c) => c.id === id);
    if (!next || next.children.length === 0) break;
    trail.push(next);
  }
  for (;;) {
    const last = trail[trail.length - 1];
    if (last.children.length !== 1 || last.children[0].children.length === 0) {
      return trail;
    }
    trail.push(last.children[0]);
  }
};

// A type, not an interface, to fit the index signature recharts wants.
export type MapCell = {
  id: string;
  name: string;
  value: number;
};

// The cells of the zoomed node, biggest first, sized by the first of size
// and then the other sizes that is not zero everywhere, else by action
// count (size null). Cells of size zero are left out.
export const mapCells = <R extends MapRow, K extends string, S extends string>(
  node: HeatTreeNode<R, K>,
  sizes: Record<S, (node: HeatTreeNode<R, K>) => number>,
  size: S,
): { cells: MapCell[]; size: S | null; hidden: number } => {
  const order = [
    size,
    ...(Object.keys(sizes) as S[]).filter((s) => s !== size),
  ];
  const shown =
    order.find((s) => node.children.some((c) => sizes[s](c) > 0)) ?? null;
  const of =
    shown === null ? (c: HeatTreeNode<R, K>) => c.actions : sizes[shown];
  const cells = node.children
    .filter((c) => of(c) > 0)
    .map((c) => ({ id: c.id, name: c.name, value: of(c) }))
    .sort((a, b) => b.value - a.value || a.name.localeCompare(b.name));
  return { cells, size: shown, hidden: node.children.length - cells.length };
};

export const CHURN_FIELDS = [
  "changes",
  "newVersions",
  "direct",
  "dependency",
  "both",
  "unknown",
  "executedAfterChange",
  "timeMs",
] as const;
export type ChurnField = (typeof CHURN_FIELDS)[number];

// Invocations and percentiles do not add up, so only actions show them.
export const IMPACT_FIELDS = [
  "executions",
  "totalMs",
  "execWallMs",
  "queueMs",
  "share",
] as const;
export type ImpactField = (typeof IMPACT_FIELDS)[number];

const ratio = (a: number, b: number) => (b > 0 ? a / b : 0);

export const changesPerAction = (n: HeatTreeNode<MapRow, ChurnField>) =>
  ratio(n.sums.changes, n.actions);
export const avgDuration = (n: HeatTreeNode<MapRow, ImpactField>) =>
  ratio(n.sums.totalMs, n.sums.executions);
export const executionsPerAction = (n: HeatTreeNode<MapRow, ImpactField>) =>
  ratio(n.sums.executions, n.actions);
export const queueShare = (n: HeatTreeNode<MapRow, ImpactField>) =>
  ratio(n.sums.queueMs, n.sums.totalMs);

// YlOrRd from ColorBrewer, cold to hot.
export const HEAT_COLORS = [
  "#ffffcc",
  "#ffeda0",
  "#fed976",
  "#feb24c",
  "#fd8d3c",
  "#fc4e2a",
  "#e31a1c",
  "#bd0026",
  "#800026",
];

const hexRgb = (hex: string): number[] => [
  Number.parseInt(hex.slice(1, 3), 16),
  Number.parseInt(hex.slice(3, 5), 16),
  Number.parseInt(hex.slice(5, 7), 16),
];

const unit = (t: number) =>
  Number.isFinite(t) ? Math.min(1, Math.max(0, t)) : 0;

// The color at t in [0, 1] along HEAT_COLORS.
export const heatColor = (t: number): string => {
  const scaled = unit(t) * (HEAT_COLORS.length - 1);
  const i = Math.min(Math.floor(scaled), HEAT_COLORS.length - 2);
  const from = hexRgb(HEAT_COLORS[i]);
  const to = hexRgb(HEAT_COLORS[i + 1]);
  const mix = from.map((c, k) => Math.round(c + (to[k] - c) * (scaled - i)));
  return `#${mix.map((c) => c.toString(16).padStart(2, "0")).join("")}`;
};

export interface HeatScale {
  min: number;
  max: number;
  log: boolean;
  at: (value: number) => number;
}

// Maps values onto [0, 1] from their min to their max. Log when they are
// positive and span 10x or more, so a few huge values do not leave the
// rest the same color; linear otherwise. A single value is the mid color.
export const heatScale = (values: number[]): HeatScale => {
  const finite = values.filter(Number.isFinite);
  if (finite.length === 0) {
    return { min: 0, max: 0, log: false, at: () => 0.5 };
  }
  const min = Math.min(...finite);
  const max = Math.max(...finite);
  const log = min > 0 && max / min >= 10;
  const f = log ? Math.log : (v: number) => v;
  const lo = f(min);
  const span = f(max) - lo;
  return {
    min,
    max,
    log,
    at: (value) => (span > 0 ? unit((f(value) - lo) / span) : 0.5),
  };
};

// Black or white text, whichever contrasts more with the fill.
export const textColorOn = (hex: string): string => {
  const [r, g, b] = hexRgb(hex).map((c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  const luminance = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  return luminance > 0.179 ? "rgba(0, 0, 0, 0.88)" : "#ffffff";
};

// Text cut to fit width pixels, or "" when not even a few characters fit.
export const fitText = (text: string, width: number, charWidth = 7): string => {
  const max = Math.floor(width / charWidth);
  if (max < 3) return "";
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
};

export const formatRate = (value: number): string =>
  value >= 10 ? `${Math.round(value)}` : `${Number(value.toFixed(1))}`;

export const formatPercent = (fraction: number): string =>
  `${Number((fraction * 100).toFixed(fraction < 0.1 ? 1 : 0))}%`;
