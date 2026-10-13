import type React from "react";
import { useMemo } from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { HeatMap, type HeatMetric, type SizeMetric } from "./HeatMap";
import type { ChurnRow, SelectedAction } from "./types";
import {
  CHURN_FIELDS,
  type ChurnField,
  causeSegments,
  causeSummary,
  changesPerAction,
  formatRate,
  type HeatTreeNode,
  heatTree,
} from "./utils";

type Node = HeatTreeNode<ChurnRow, ChurnField>;
type Size = "time" | "changes" | "actions";

const duration = (ms: number) =>
  readableDurationFromMilliseconds(ms, { smallestUnit: "ms" });

const plural = (count: number, noun: string) =>
  `${count} ${noun}${count === 1 ? "" : "s"}`;

const SIZES: Record<Size, SizeMetric<Node>> = {
  time: { label: "Executed time", of: (n) => n.sums.timeMs },
  changes: { label: "Changes", of: (n) => n.sums.changes },
  actions: { label: "Actions", of: (n) => n.actions },
};

const HEATS: Record<"changes", HeatMetric<Node>> = {
  changes: {
    label: "Changes per action",
    legend: "Changes per action",
    of: changesPerAction,
    format: formatRate,
  },
};

const detail = (n: Node, size: Size | null): string => {
  const changes = plural(n.sums.changes, "change");
  switch (size) {
    case "time":
      return `${duration(n.sums.timeMs)} · ${changes}`;
    case "changes":
      return `${changes} · ${plural(n.actions, "action")}`;
    default:
      return `${plural(n.actions, "action")} · ${changes}`;
  }
};

const tooltip = (n: Node): [string, React.ReactNode][] => [
  ["Changes", n.sums.changes],
  ["Changes per action", formatRate(changesPerAction(n))],
  ["New versions", n.sums.newVersions],
  ["Causes", causeSummary(causeSegments(n.sums)) || "-"],
  ["Executed after change", n.sums.executedAfterChange],
  ["Time after changes", duration(n.sums.timeMs)],
];

export const ChurnMap: React.FC<{
  rows: ChurnRow[];
  onSelect: (action: SelectedAction) => void;
}> = ({ rows, onSelect }) => {
  const root = useMemo(() => heatTree(rows, CHURN_FIELDS), [rows]);
  return (
    <HeatMap
      root={root}
      sizes={SIZES}
      heats={HEATS}
      detail={detail}
      tooltip={tooltip}
      emptyText="No action changed between these invocations"
      onSelect={onSelect}
    />
  );
};
