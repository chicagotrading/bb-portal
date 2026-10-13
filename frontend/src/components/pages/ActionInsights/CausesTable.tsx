import { Table, type TableColumnsType, Tag, Typography } from "antd";
import type React from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { TooFewInvocations } from "./ChurnTable";
import { GroupTag } from "./cells";
import type { CauseRow } from "./types";
import { GROUP_KINDS, truncatedNote } from "./utils";

const columns: TableColumnsType<CauseRow> = [
  {
    title: "Input",
    key: "name",
    render: (_, row) => <GroupTag name={row.name} kind={row.kind} />,
    sorter: (a, b) => a.name.localeCompare(b.name),
  },
  {
    title: "Kind",
    key: "kind",
    render: (_, row) => (
      <Tag color={GROUP_KINDS[row.kind].tagColor}>
        {GROUP_KINDS[row.kind].label}
      </Tag>
    ),
    filters: Object.entries(GROUP_KINDS).map(([value, kind]) => ({
      text: kind.label,
      value,
    })),
    onFilter: (value, row) => row.kind === value,
  },
  {
    title: "New versions caused",
    key: "newVersions",
    align: "right",
    dataIndex: "newVersions",
    sorter: (a, b) => a.newVersions - b.newVersions || a.changes - b.changes,
    defaultSortOrder: "descend",
  },
  {
    title: "Times changed",
    key: "changes",
    align: "right",
    dataIndex: "changes",
    sorter: (a, b) => a.changes - b.changes,
  },
  {
    title: "Actions affected",
    key: "actionsAffected",
    align: "right",
    dataIndex: "actionsAffected",
    sorter: (a, b) => a.actionsAffected - b.actionsAffected,
  },
  {
    title: "Executed time downstream",
    key: "timeMs",
    align: "right",
    render: (_, row) =>
      readableDurationFromMilliseconds(row.timeMs, { smallestUnit: "ms" }),
    sorter: (a, b) => a.timeMs - b.timeMs,
  },
];

export const CausesTable: React.FC<{
  rows: CauseRow[];
  rowLimit: number;
  invocationsAnalyzed: number;
  changesExplained: number;
  changesTotal: number;
}> = ({
  rows,
  rowLimit,
  invocationsAnalyzed,
  changesExplained,
  changesTotal,
}) => {
  if (invocationsAnalyzed < 2) return <TooFewInvocations />;
  const note = truncatedNote(rows.length, rowLimit, "new versions caused");
  return (
    <>
      <Typography.Paragraph type="secondary">
        Inputs that changed under some action, counted once per action change.
        {changesExplained < changesTotal &&
          ` Based on ${changesExplained} of ${changesTotal} changes.`}
        {note && ` ${note}`}
      </Typography.Paragraph>
      <Table
        columns={columns}
        dataSource={rows}
        rowKey="name"
        size="small"
        scroll={{ x: true }}
        pagination={{ defaultPageSize: 25, hideOnSinglePage: true }}
        locale={{ emptyText: "No input changed between these invocations" }}
      />
    </>
  );
};
