import { useQuery } from "@apollo/client/react";
import { Link } from "@tanstack/react-router";
import {
  Space,
  Table,
  type TableColumnsType,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type React from "react";
import dayjs from "@/lib/dayjs";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { CauseTag, GroupTags } from "./cells";
import { compareActionLink } from "./compare";
import { ACTION_HISTORY } from "./index.graphql";
import type { HistoryEntry, InvocationScope } from "./types";

export const useActionHistory = (keyId: string, scope: InvocationScope) =>
  useQuery(ACTION_HISTORY, {
    variables: {
      keyId,
      where: scope.where,
      invocationLimit: scope.invocationLimit,
    },
  });

const columns = (maxGroups?: number): TableColumnsType<HistoryEntry> => [
  {
    title: "Invocation",
    key: "invocation",
    render: (_, entry) => (
      <Link
        to="/bazel-invocations/$invocationID"
        params={{ invocationID: entry.invocationId }}
      >
        {entry.startedAt
          ? dayjs(entry.startedAt).format("YYYY-MM-DD HH:mm")
          : entry.invocationId}
      </Link>
    ),
  },
  {
    title: "Change",
    key: "cause",
    render: (_, entry) => (
      <Space size={4} wrap>
        <CauseTag cause={entry.changed ? entry.cause : null} />
        {entry.changed && !entry.newVersion && (
          <Tooltip title="The action returned to a digest it had in an earlier analyzed invocation, as when builds of different branches alternate">
            <Tag>earlier version</Tag>
          </Tooltip>
        )}
      </Space>
    ),
  },
  {
    title: "Changed inputs",
    key: "changedGroups",
    render: (_, entry) =>
      entry.changed ? (
        <GroupTags names={entry.changedGroups} max={maxGroups} />
      ) : null,
  },
  {
    title: "Duration",
    key: "duration",
    align: "right",
    render: (_, entry) => {
      if (!entry.execution) {
        return <Typography.Text type="secondary">cache hit</Typography.Text>;
      }
      const longest = readableDurationFromMilliseconds(
        entry.execution.totalMs,
        { smallestUnit: "ms" },
      );
      if (entry.executions <= 1) return longest;
      return (
        <Tooltip
          title={`${entry.executions} executions, ${readableDurationFromMilliseconds(entry.executedMs, { smallestUnit: "ms" })} in total; the longest is shown`}
        >
          {longest} (×{entry.executions})
        </Tooltip>
      );
    },
  },
  {
    title: "Queue",
    key: "queue",
    align: "right",
    render: (_, entry) =>
      entry.execution
        ? readableDurationFromMilliseconds(entry.execution.queueMs, {
            smallestUnit: "ms",
          })
        : null,
  },
  {
    title: "Runner",
    key: "runner",
    render: (_, entry) => entry.execution?.runner,
  },
  {
    title: "",
    key: "compare",
    render: (_, entry) => {
      const link = entry.changed ? compareActionLink(entry) : undefined;
      return link ? (
        <Link to="/browser/$" params={link.params} search={link.search}>
          Compare
        </Link>
      ) : null;
    },
  },
];

export const HistoryTable: React.FC<{
  entries: HistoryEntry[];
  loading: boolean;
  maxGroups?: number;
  emptyText: string;
}> = ({ entries, loading, maxGroups, emptyText }) => (
  <Table
    columns={columns(maxGroups)}
    dataSource={entries}
    loading={loading}
    rowKey="invocationId"
    size="small"
    scroll={{ x: true }}
    pagination={{ defaultPageSize: 20, hideOnSinglePage: true }}
    locale={{ emptyText }}
  />
);
