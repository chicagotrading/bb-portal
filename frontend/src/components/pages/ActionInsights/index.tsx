import { NodeIndexOutlined } from "@ant-design/icons";
import { useQuery } from "@apollo/client/react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  Alert,
  Empty,
  Select,
  Space,
  Spin,
  Statistic,
  Tabs,
  Typography,
} from "antd";
import type React from "react";
import { useEffect, useMemo, useState } from "react";
import {
  InvocationFilterBar,
  useInvocationFilterSearch,
} from "@/components/InvocationFilters";
import {
  describeInvocations,
  type InvocationFilters,
  invocationFilterSearch,
  invocationFiltersWhere,
} from "@/components/InvocationFilters/utils";
import { PortalCard } from "@/components/PortalCard";
import {
  INVOCATION_WINDOWS,
  invocationWindowWhere,
} from "@/utils/invocationWindows";
import { readableDurationFromMilliseconds } from "@/utils/time";
import { CausesTable } from "./CausesTable";
import { ChurnTable } from "./ChurnTable";
import { HistoryDrawer } from "./HistoryDrawer";
import { ImpactTable } from "./ImpactTable";
import { ACTION_INSIGHTS } from "./index.graphql";
import type {
  ActionInsightsData,
  InvocationScope,
  SelectedAction,
} from "./types";
import {
  type ActionInsightsTab,
  coverageParts,
  emptyReason,
  type MapView,
} from "./utils";

const DEFAULT_LIMIT = 100;
const LIMITS = [25, 50, 100, 200];
const ROW_LIMIT = 500;

const NoExecutionLogs: React.FC = () => (
  <Empty
    description={
      <Space orientation="vertical">
        <span>None of these invocations published an execution log.</span>
        <Typography.Text type="secondary">
          Only invocations run with{" "}
          <Typography.Text code>
            --execution_log_compact_file=...
          </Typography.Text>{" "}
          are analyzed. Bazel uploads the log with the build tool logs, and the
          portal ingests it shortly after the invocation ends.
        </Typography.Text>
      </Space>
    }
  />
);

const NotIngested: React.FC<{ insights: ActionInsightsData }> = ({
  insights,
}) => (
  <Empty
    description={
      <Space orientation="vertical">
        <span>
          None of these invocations has an ingested execution log yet:{" "}
          {coverageParts(insights).slice(1).join(", ")}.
        </span>
        <Typography.Text type="secondary">
          The portal ingests logs one at a time shortly after an invocation
          ends. Logs that keep waiting mean the ingestion service is busy or not
          configured (
          <Typography.Text code>
            execLogIngestServiceConfiguration
          </Typography.Text>
          ). A failed log has its error in the portal's database and is not
          retried.
        </Typography.Text>
      </Space>
    }
  />
);

const Coverage: React.FC<{
  insights: ActionInsightsData;
  filters: InvocationFilters;
}> = ({ insights, filters }) => (
  <Space orientation="vertical" size="small" style={{ width: "100%" }}>
    <Typography.Text type="secondary">
      {insights.invocationsMatched} {describeInvocations(filters)} match since
      the oldest analyzed one: {coverageParts(insights).join(", ")}. Analyzed
      invocations are compared oldest first.
    </Typography.Text>
    {insights.refsAnalyzed > 1 && (
      <Typography.Text type="secondary">
        They come from {insights.refsAnalyzed} source control branches, so
        switching between them counts as changes. New versions leave out returns
        to a version seen before; filter by repo and branch to look at one.
      </Typography.Text>
    )}
    <Space size="large" wrap>
      <Statistic title="Spawns" value={insights.spawnsAnalyzed} />
      <Statistic title="Executed" value={insights.executedSpawns} />
      <Statistic
        title="Executed time"
        value={readableDurationFromMilliseconds(insights.executedTimeMs, {
          smallestUnit: "ms",
        })}
      />
      <Statistic title="Action changes" value={insights.changesTotal} />
    </Space>
  </Space>
);

export const ActionInsightsPage: React.FC = () => {
  const search = useSearch({ from: "/action-insights" });
  const navigate = useNavigate({ from: "/action-insights" });
  const limit = search.limit ?? DEFAULT_LIMIT;
  const timeWindow = search.window ?? "all";
  const { commands, filters } = useInvocationFilterSearch(search);
  const tab: ActionInsightsTab = search.tab ?? "impact";
  const churnView: MapView = search.churnView ?? "table";
  const impactView: MapView = search.impactView ?? "table";
  // Table is the default and stays out of the URL.
  const setView = (param: "churnView" | "impactView") => (view: MapView) =>
    navigate({
      search: (prev) => ({
        ...prev,
        [param]: view === "table" ? undefined : view,
      }),
    });
  const [selected, setSelected] = useState<SelectedAction | null>(null);

  const scope = useMemo<InvocationScope>(
    () => ({
      where: invocationFiltersWhere(timeWindow, commands, filters, new Date()),
      invocationLimit: limit,
    }),
    [timeWindow, commands, filters, limit],
  );
  const optionsWhere = useMemo(
    () => invocationWindowWhere(timeWindow, commands, new Date()),
    [timeWindow, commands],
  );
  const { data, previousData, loading, error } = useQuery(ACTION_INSIGHTS, {
    variables: { ...scope, rowLimit: ROW_LIMIT },
    fetchPolicy: "cache-and-network",
  });
  const insights = (loading ? (data ?? previousData) : data)?.actionInsights;
  // The filters of the shown results, which lag while new ones load.
  const [shownFilters, setShownFilters] = useState(filters);
  useEffect(() => {
    if (data) setShownFilters(filters);
  }, [data, filters]);

  const content = (insights: ActionInsightsData) => {
    switch (emptyReason(insights)) {
      case "noMatches":
        return <Empty description="No invocations match these filters." />;
      case "notIngested":
        return <NotIngested insights={insights} />;
      case "noLogs":
        return <NoExecutionLogs />;
    }
    return (
      <>
        <Coverage insights={insights} filters={shownFilters} />
        <Tabs
          activeKey={tab}
          onChange={(key) =>
            navigate({
              search: (prev) => ({ ...prev, tab: key as ActionInsightsTab }),
            })
          }
          items={[
            {
              key: "impact",
              label: "Impact",
              children: (
                <ImpactTable
                  rows={insights.impact}
                  rowLimit={ROW_LIMIT}
                  view={impactView}
                  onViewChange={setView("impactView")}
                  onSelect={setSelected}
                />
              ),
            },
            {
              key: "churn",
              label: "Churn",
              children: (
                <ChurnTable
                  rows={insights.churn}
                  rowLimit={ROW_LIMIT}
                  invocationsAnalyzed={insights.invocationsAnalyzed}
                  scope={scope}
                  view={churnView}
                  onViewChange={setView("churnView")}
                  onSelect={setSelected}
                />
              ),
            },
            {
              key: "causes",
              label: "Causes",
              children: (
                <CausesTable
                  rows={insights.causes}
                  rowLimit={ROW_LIMIT}
                  invocationsAnalyzed={insights.invocationsAnalyzed}
                  changesExplained={insights.changesExplained}
                  changesTotal={insights.changesTotal}
                />
              ),
            },
          ]}
        />
      </>
    );
  };

  const body = () => {
    if (error) return <Alert type="error" showIcon title={error.message} />;
    if (!insights) return <Spin />;
    // Results of the previous filters stay visible, dimmed, while loading.
    return <Spin spinning={loading}>{content(insights)}</Spin>;
  };

  return (
    <PortalCard
      icon={<NodeIndexOutlined />}
      titleBits={[<span key="title">Action Insights</span>]}
      extraBits={[
        <Select
          key="window"
          style={{ minWidth: 150 }}
          value={timeWindow}
          onChange={(value) =>
            navigate({ search: (prev) => ({ ...prev, window: value }) })
          }
          options={Object.entries(INVOCATION_WINDOWS).map(([value, w]) => ({
            value,
            label: w.label,
          }))}
        />,
        <Select
          key="limit"
          style={{ minWidth: 250 }}
          value={limit}
          onChange={(value) =>
            navigate({ search: (prev) => ({ ...prev, limit: value }) })
          }
          options={LIMITS.map((value) => ({
            value,
            label: `Latest ${value} analyzed invocations`,
          }))}
        />,
      ]}
    >
      <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
        <InvocationFilterBar
          optionsWhere={optionsWhere}
          analyzedOnly
          commands={commands}
          filters={filters}
          onCommandsChange={(value) =>
            navigate({
              search: (prev) => ({
                ...prev,
                ...invocationFilterSearch(value, filters),
              }),
            })
          }
          onFiltersChange={(next) =>
            navigate({
              search: (prev) => ({
                ...prev,
                ...invocationFilterSearch(commands, next),
              }),
            })
          }
        />
        {body()}
      </Space>
      <HistoryDrawer
        action={selected}
        scope={scope}
        onClose={() => setSelected(null)}
      />
    </PortalCard>
  );
};
