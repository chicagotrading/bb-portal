import {
  Alert,
  Descriptions,
  Drawer,
  Space,
  Statistic,
  Tooltip,
  Typography,
} from "antd";
import type React from "react";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { OutputPath, TargetLink } from "./cells";
import { HistoryChart } from "./HistoryChart";
import { HistoryTable, useActionHistory } from "./HistoryTable";
import type { InvocationScope, SelectedAction } from "./types";

const HistoryContent: React.FC<{
  action: SelectedAction;
  scope: InvocationScope;
}> = ({ action, scope }) => {
  const { data, loading, error } = useActionHistory(action.keyId, scope);
  const entries = data?.actionHistory ?? [];
  // Counts every execution, like the Impact table.
  const executions = entries.reduce((sum, entry) => sum + entry.executions, 0);
  const executedMs = entries.reduce((sum, entry) => sum + entry.executedMs, 0);
  const stat = (value: number | string) => (data ? value : "-");
  return (
    <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
      <Descriptions
        size="small"
        column={1}
        items={[
          {
            key: "target",
            label: "Target",
            children: <TargetLink label={action.targetLabel} />,
          },
          { key: "mnemonic", label: "Mnemonic", children: action.mnemonic },
          {
            key: "output",
            label: "Output",
            children: <OutputPath path={action.primaryOutput} maxWidth={600} />,
          },
        ]}
      />
      {error && <Alert type="error" showIcon title={error.message} />}
      <Space size="large">
        <Statistic
          title={
            <Tooltip title="Analyzed invocations that ran the action, cache hits included">
              Invocations seen
            </Tooltip>
          }
          value={stat(entries.length)}
        />
        <Statistic
          title="Changes"
          value={stat(entries.filter((entry) => entry.changed).length)}
        />
        <Statistic title="Executions" value={stat(executions)} />
        <Statistic
          title="Executed time"
          value={stat(
            readableDurationFromMilliseconds(executedMs, {
              smallestUnit: "ms",
            }),
          )}
        />
      </Space>
      <HistoryChart entries={entries} />
      <Typography.Text type="secondary">
        Dots are the longest execution in each invocation; dashed lines mark
        changes, colored by cause.
      </Typography.Text>
      <HistoryTable
        entries={[...entries].reverse()}
        loading={loading}
        maxGroups={3}
        emptyText="The action ran in none of the analyzed invocations"
      />
    </Space>
  );
};

export const HistoryDrawer: React.FC<{
  action: SelectedAction | null;
  scope: InvocationScope;
  onClose: () => void;
}> = ({ action, scope, onClose }) => (
  <Drawer
    open={action !== null}
    onClose={onClose}
    size="80%"
    destroyOnHidden
    title={action ? `${action.mnemonic} ${action.targetLabel}` : undefined}
  >
    {action && <HistoryContent action={action} scope={scope} />}
  </Drawer>
);
