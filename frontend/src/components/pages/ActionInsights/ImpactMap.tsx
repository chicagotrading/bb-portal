import type React from "react";
import { useMemo } from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { HeatMap, type HeatMetric, type SizeMetric } from "./HeatMap";
import type { ImpactRow, SelectedAction } from "./types";
import {
  avgDuration,
  executionsPerAction,
  formatPercent,
  formatRate,
  type HeatTreeNode,
  heatTree,
  IMPACT_FIELDS,
  type ImpactField,
  queueShare,
} from "./utils";

type Node = HeatTreeNode<ImpactRow, ImpactField>;
type Size = "time" | "executions";
type Heat = "avg" | "executions" | "queue";

const duration = (ms: number) =>
  readableDurationFromMilliseconds(ms, { smallestUnit: "ms" });

const plural = (count: number, noun: string) =>
  `${count} ${noun}${count === 1 ? "" : "s"}`;

const SIZES: Record<Size, SizeMetric<Node>> = {
  time: { label: "Executed time", of: (n) => n.sums.totalMs },
  executions: { label: "Executions", of: (n) => n.sums.executions },
};

const HEATS: Record<Heat, HeatMetric<Node>> = {
  avg: {
    label: "Avg duration",
    legend: "Avg duration",
    of: avgDuration,
    format: duration,
  },
  executions: {
    label: "Executions",
    legend: "Executions per action",
    of: executionsPerAction,
    format: formatRate,
  },
  queue: {
    label: "Queue share",
    legend: "Queue share",
    of: queueShare,
    format: formatPercent,
  },
};

const detail = (n: Node, size: Size | null): string => {
  const executions = plural(n.sums.executions, "execution");
  switch (size) {
    case "executions":
      return `${executions} · avg ${duration(avgDuration(n))}`;
    case "time":
      return `${duration(n.sums.totalMs)} · ${executions}`;
    default:
      return `${plural(n.actions, "action")} · ${duration(n.sums.totalMs)}`;
  }
};

const tooltip = (n: Node): [string, React.ReactNode][] => [
  ...(n.row
    ? ([["Invocations", n.row.invocations]] as [string, number][])
    : []),
  ["Executions", n.sums.executions],
  ["Total time", duration(n.sums.totalMs)],
  ["Avg duration", duration(avgDuration(n))],
  ...(n.row
    ? ([
        ["p50", duration(n.row.p50Ms)],
        ["p95", duration(n.row.p95Ms)],
        ["Max", duration(n.row.maxMs)],
      ] as [string, string][])
    : []),
  ["Queue", `${duration(n.sums.queueMs)} (${formatPercent(queueShare(n))})`],
  ["Share of executed time", formatPercent(n.sums.share)],
];

export const ImpactMap: React.FC<{
  rows: ImpactRow[];
  onSelect: (action: SelectedAction) => void;
}> = ({ rows, onSelect }) => {
  const root = useMemo(() => heatTree(rows, IMPACT_FIELDS), [rows]);
  return (
    <HeatMap
      root={root}
      sizes={SIZES}
      heats={HEATS}
      detail={detail}
      tooltip={tooltip}
      emptyText="No action was executed in these invocations"
      onSelect={onSelect}
    />
  );
};
