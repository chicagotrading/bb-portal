import {
  Alert,
  Button,
  Empty,
  Flex,
  Space,
  Table,
  type TableColumnsType,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type React from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { ChurnMap } from "./ChurnMap";
import {
  CauseBar,
  CauseLegend,
  GroupTag,
  OutputPath,
  TargetLink,
} from "./cells";
import { ViewSwitch } from "./HeatMap";
import { HistoryTable, useActionHistory } from "./HistoryTable";
import type { ChurnRow, InvocationScope, SelectedAction } from "./types";
import { type MapView, truncatedNote } from "./utils";

export const TooFewInvocations: React.FC = () => (
  <Empty description="At least two analyzed invocations are needed to detect changes." />
);

const ChangeEvents: React.FC<{ keyId: string; scope: InvocationScope }> = ({
  keyId,
  scope,
}) => {
  const { data, loading, error } = useActionHistory(keyId, scope);
  if (error) return <Alert type="error" showIcon title={error.message} />;
  const changes = (data?.actionHistory ?? [])
    .filter((entry) => entry.changed)
    .reverse();
  return (
    <HistoryTable entries={changes} loading={loading} emptyText="No changes" />
  );
};

const TOP_CAUSES_SHOWN = 1;

const columns = (
  onSelect: (action: SelectedAction) => void,
): TableColumnsType<ChurnRow> => [
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
    render: (_, row) => <OutputPath path={row.primaryOutput} maxWidth={200} />,
  },
  {
    title: "New versions",
    key: "newVersions",
    align: "right",
    dataIndex: "newVersions",
    sorter: (a, b) => a.newVersions - b.newVersions || a.changes - b.changes,
    defaultSortOrder: "descend",
  },
  {
    title: "Changes",
    key: "changes",
    align: "right",
    dataIndex: "changes",
    sorter: (a, b) => a.changes - b.changes,
  },
  {
    title: "Direct vs via dependencies",
    key: "causes",
    render: (_, row) => <CauseBar counts={row} />,
    sorter: (a, b) =>
      a.direct + a.both - (b.direct + b.both) || a.changes - b.changes,
  },
  {
    title: "Executed after change",
    key: "executedAfterChange",
    align: "right",
    dataIndex: "executedAfterChange",
    sorter: (a, b) => a.executedAfterChange - b.executedAfterChange,
  },
  {
    title: "Time after changes",
    key: "timeMs",
    align: "right",
    render: (_, row) =>
      readableDurationFromMilliseconds(row.timeMs, { smallestUnit: "ms" }),
    sorter: (a, b) => a.timeMs - b.timeMs,
  },
  {
    title: "Top causes",
    key: "topCauses",
    width: 380,
    render: (_, row) => (
      <Flex gap={4}>
        {row.topCauses.slice(0, TOP_CAUSES_SHOWN).map((cause) => (
          <GroupTag
            key={cause.name}
            name={cause.name}
            kind={cause.kind}
            count={cause.count}
            maxWidth={300}
          />
        ))}
        {row.topCauses.length > TOP_CAUSES_SHOWN && (
          <Tooltip
            title={row.topCauses.slice(TOP_CAUSES_SHOWN).map((cause) => (
              <div key={cause.name}>
                {cause.name} ×{cause.count}
              </div>
            ))}
          >
            <Tag>+{row.topCauses.length - TOP_CAUSES_SHOWN} more</Tag>
          </Tooltip>
        )}
      </Flex>
    ),
  },
  {
    title: "History",
    key: "history",
    render: (_, row) => (
      <Button
        type="link"
        size="small"
        aria-label={`History of ${row.mnemonic} ${row.targetLabel}`}
        onClick={() => onSelect(row)}
      >
        History
      </Button>
    ),
  },
];

export const ChurnTable: React.FC<{
  rows: ChurnRow[];
  rowLimit: number;
  invocationsAnalyzed: number;
  scope: InvocationScope;
  view: MapView;
  onViewChange: (view: MapView) => void;
  onSelect: (action: SelectedAction) => void;
}> = ({
  rows,
  rowLimit,
  invocationsAnalyzed,
  scope,
  view,
  onViewChange,
  onSelect,
}) => {
  if (invocationsAnalyzed < 2) return <TooFewInvocations />;
  const note = truncatedNote(
    rows.length,
    rowLimit,
    "new versions",
    view === "map" ? "the map covers only these" : undefined,
  );
  return (
    <>
      <Flex justify="space-between" wrap gap={8}>
        <Typography.Paragraph type="secondary" style={{ flex: "1 1 600px" }}>
          A change is a new digest since the previous analyzed invocation that
          ran the action. New versions leave out returns to an earlier digest.
          {view === "map" &&
            " Hotter means more changes per action. Click a package or target to zoom in, an action to open its history."}
          {note && ` ${note}`}
        </Typography.Paragraph>
        <Space size="large" wrap align="start">
          {view === "table" && <CauseLegend />}
          <ViewSwitch view={view} onChange={onViewChange} />
        </Space>
      </Flex>
      {view === "map" ? (
        <ChurnMap rows={rows} onSelect={onSelect} />
      ) : (
        <Table
          columns={columns(onSelect)}
          dataSource={rows}
          rowKey="keyId"
          size="small"
          scroll={{ x: true }}
          pagination={{ defaultPageSize: 25, hideOnSinglePage: true }}
          expandable={{
            expandedRowRender: (row) => (
              <ChangeEvents keyId={row.keyId} scope={scope} />
            ),
          }}
          locale={{ emptyText: "No action changed between these invocations" }}
        />
      )}
    </>
  );
};
