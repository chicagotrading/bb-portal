import {
  Button,
  Flex,
  Progress,
  Table,
  type TableColumnsType,
  Typography,
} from "antd";
import type React from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { OutputPath, TargetLink } from "./cells";
import { ViewSwitch } from "./HeatMap";
import { ImpactMap } from "./ImpactMap";
import type { ImpactRow, SelectedAction } from "./types";
import { type MapView, truncatedNote } from "./utils";

const duration = (milliseconds: number) =>
  readableDurationFromMilliseconds(milliseconds, { smallestUnit: "ms" });

const columns = (
  onSelect: (action: SelectedAction) => void,
): TableColumnsType<ImpactRow> => [
  {
    title: "Target",
    key: "target",
    render: (_, row) => <TargetLink label={row.targetLabel} maxWidth={340} />,
    sorter: (a, b) => a.targetLabel.localeCompare(b.targetLabel),
  },
  {
    title: "Mnemonic",
    key: "mnemonic",
    dataIndex: "mnemonic",
    sorter: (a, b) => a.mnemonic.localeCompare(b.mnemonic),
  },
  {
    title: "Output",
    key: "output",
    render: (_, row) => <OutputPath path={row.primaryOutput} maxWidth={240} />,
  },
  {
    title: "Executions",
    key: "executions",
    align: "right",
    dataIndex: "executions",
    sorter: (a, b) => a.executions - b.executions,
  },
  {
    title: "Invocations",
    key: "invocations",
    align: "right",
    dataIndex: "invocations",
    sorter: (a, b) => a.invocations - b.invocations,
  },
  {
    title: "Total time",
    key: "totalMs",
    align: "right",
    render: (_, row) => duration(row.totalMs),
    sorter: (a, b) => a.totalMs - b.totalMs,
    defaultSortOrder: "descend",
  },
  {
    title: "p50",
    key: "p50Ms",
    align: "right",
    render: (_, row) => duration(row.p50Ms),
    sorter: (a, b) => a.p50Ms - b.p50Ms,
  },
  {
    title: "p95",
    key: "p95Ms",
    align: "right",
    render: (_, row) => duration(row.p95Ms),
    sorter: (a, b) => a.p95Ms - b.p95Ms,
  },
  {
    title: "Max",
    key: "maxMs",
    align: "right",
    render: (_, row) => duration(row.maxMs),
    sorter: (a, b) => a.maxMs - b.maxMs,
  },
  {
    title: "Queue",
    key: "queueMs",
    align: "right",
    render: (_, row) => duration(row.queueMs),
    sorter: (a, b) => a.queueMs - b.queueMs,
  },
  {
    title: "Share of executed time",
    key: "share",
    render: (_, row) => (
      <Progress
        percent={row.share * 100}
        size="small"
        format={(percent) => `${(percent ?? 0).toFixed(1)}%`}
        style={{ minWidth: 140 }}
      />
    ),
    sorter: (a, b) => a.share - b.share,
  },
  {
    title: "History",
    key: "history",
    render: (_, row) => (
      <Button
        type="link"
        size="small"
        aria-label={`History of ${row.mnemonic} ${row.targetLabel}`}
        onClick={(event) => {
          event.stopPropagation();
          onSelect(row);
        }}
      >
        History
      </Button>
    ),
  },
];

export const ImpactTable: React.FC<{
  rows: ImpactRow[];
  rowLimit: number;
  view: MapView;
  onViewChange: (view: MapView) => void;
  onSelect: (action: SelectedAction) => void;
}> = ({ rows, rowLimit, view, onViewChange, onSelect }) => {
  const note = truncatedNote(
    rows.length,
    rowLimit,
    "total time",
    view === "map" ? "the map covers only these" : undefined,
  );
  return (
    <>
      <Flex justify="space-between" wrap gap={8}>
        <Typography.Paragraph type="secondary" style={{ flex: "1 1 600px" }}>
          {view === "map" &&
            "Hotter means a higher value of the metric picked as the color. Click a package or target to zoom in, an action to open its history."}
          {note && `${view === "map" ? " " : ""}${note}`}
        </Typography.Paragraph>
        <ViewSwitch view={view} onChange={onViewChange} />
      </Flex>
      {view === "map" ? (
        <ImpactMap rows={rows} onSelect={onSelect} />
      ) : (
        <Table
          columns={columns(onSelect)}
          dataSource={rows}
          rowKey="keyId"
          size="small"
          scroll={{ x: true }}
          pagination={{ defaultPageSize: 25, hideOnSinglePage: true }}
          // A shortcut for the History button.
          onRow={(row) => ({
            onClick: () => onSelect(row),
            style: { cursor: "pointer" },
          })}
          locale={{ emptyText: "No action was executed in these invocations" }}
        />
      )}
    </>
  );
};
