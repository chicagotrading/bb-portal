import {
  BarChartOutlined,
  ClockCircleFilled,
  LineChartOutlined,
} from "@ant-design/icons";
import { useQuery } from "@apollo/client/react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Button, Row, Select, Space, Statistic, Typography, theme } from "antd";
import type React from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Area,
  AreaChart,
  Bar,
  Brush,
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceDot,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PortalCard } from "@/components/PortalCard";
import {
  type BazelInvocationNodeFragment,
  BazelInvocationOrderField,
  type FindBuildTimesQueryVariables,
  OrderDirection,
} from "@/graphql/__generated__/graphql";
import dayjs from "@/lib/dayjs";
import { BAZEL_COMMANDS } from "@/utils/bazelCommands";
import {
  INVOCATION_WINDOWS,
  invocationWindowWhere,
} from "@/utils/invocationWindows";
import {
  readableDurationFromMilliseconds,
  spelledOutDurationFromMilliseconds,
} from "@/utils/time";
import FIND_BUILD_DURATIONS from "./index.graphql";

const TRENDS_DEFAULT_LIMIT = 1000;
const TRENDS_LIMITS = [100, 500, 1000, 5000];

// Width of a distribution bin, in decades of milliseconds (about 26% per bin).
const LOG_BIN_WIDTH = 0.1;
const LOG_TICKS = [
  10, 100, 1_000, 10_000, 60_000, 600_000, 3_600_000, 36_000_000,
].map(Math.log10);

const shortDuration = (milliseconds: number): string => {
  if (milliseconds < 1_000) return `${Math.round(milliseconds)} ms`;
  if (milliseconds < 60_000) return `${Math.round(milliseconds / 1_000)} s`;
  if (milliseconds < 3_600_000)
    return `${Math.round(milliseconds / 60_000)} min`;
  return `${Math.round(milliseconds / 3_600_000)} h`;
};

interface DistributionBin {
  x: number;
  lower: number;
  upper: number;
  count: number;
  density: number;
  cumulative: number;
}

export const TrendsPage: React.FC = () => {
  const { token } = theme.useToken();
  const [commands, setCommands] = useState<string[]>([]);
  const [brushRange, setBrushRange] = useState<{
    startIndex: number;
    endIndex: number;
  } | null>(null);
  // Invocation whose tooltip was pinned by a click; it stays put and links to the invocation.
  const [pinned, setPinned] = useState<{
    invocationId: string;
    from: number;
    duration: number;
    x: number;
  } | null>(null);
  const chartBox = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const unpin = (event: KeyboardEvent) => {
      if (event.key === "Escape") setPinned(null);
    };
    window.addEventListener("keydown", unpin);
    return () => window.removeEventListener("keydown", unpin);
  }, []);
  const search = useSearch({ from: "/trends" });
  const limit = search.limit ?? TRENDS_DEFAULT_LIMIT;
  const timeWindow = search.window ?? "all";
  const navigate = useNavigate({ from: "/trends" });
  const variables = useMemo<FindBuildTimesQueryVariables>(() => {
    return {
      first: limit,
      where: {
        ...invocationWindowWhere(timeWindow, commands, new Date()),
        endedAtNotNil: true,
      },
      // Newest first; without an order the API returns the oldest invocations still kept.
      orderBy: {
        direction: OrderDirection.Desc,
        field: BazelInvocationOrderField.StartedAt,
      },
    };
  }, [limit, timeWindow, commands]);

  const { loading, data, previousData, error } = useQuery(
    FIND_BUILD_DURATIONS,
    {
      variables,
      pollInterval: 120000,
      fetchPolicy: "cache-and-network",
    },
  );

  const activeData = loading ? previousData : data;

  // Stable across renders, so dragging the brush doesn't hand recharts new data and reset the drag.
  const dataPoints = useMemo(() => {
    const nodes = error
      ? []
      : (activeData?.findBazelInvocations.edges ?? [])
          .map((edge) => edge?.node)
          .filter((x): x is BazelInvocationNodeFragment => !!x);
    return nodes
      .map((x) => {
        const from = new Date(x.startedAt).getTime();
        const to = new Date(x.endedAt).getTime();
        return { invocationId: x.invocationID, from, to, duration: to - from };
      })
      .filter((point) => point.duration > 0)
      .sort((a, b) => a.from - b.from);
  }, [activeData, error]);

  const range =
    brushRange && brushRange.endIndex < dataPoints.length ? brushRange : null;
  const visiblePoints = range
    ? dataPoints.slice(range.startIndex, range.endIndex + 1)
    : dataPoints;

  // A categorical axis labels every invocation; a time axis gets evenly spaced ticks.
  const firstFrom = visiblePoints.length > 0 ? visiblePoints[0].from : 0;
  const timeSpan =
    visiblePoints.length > 0
      ? visiblePoints[visiblePoints.length - 1].from - firstFrom
      : 0;
  const timeTicks = Array.from(
    { length: 8 },
    (_, i) => firstFrom + (timeSpan * i) / 7,
  );
  const timeTickFormat =
    timeSpan <= 3_600_000
      ? "HH:mm:ss"
      : timeSpan <= 86_400_000
        ? "HH:mm"
        : timeSpan <= 7 * 86_400_000
          ? "MMM D HH:mm"
          : "MMM D";

  var avg: number =
    visiblePoints.reduce((sum, item) => sum + item.duration, 0) /
    visiblePoints.length;
  var medianVals = visiblePoints.map((x) => x.duration).sort((a, b) => a - b);
  var medianMid = Math.floor(visiblePoints.length / 2);
  var median: number;

  if (medianVals.length % 2 === 0) {
    median = (medianVals[medianMid - 1] + medianVals[medianMid]) / 2;
  } else {
    median = medianVals[medianMid];
  }

  var max: number = Math.max(...visiblePoints.map((x) => x.duration));
  var min: number = Math.min(...visiblePoints.map((x) => x.duration));

  // Linear interpolation between ranks, so p50 equals the median above.
  const percentile = (q: number) => {
    const rank = q * (medianVals.length - 1);
    const lower = medianVals[Math.floor(rank)];
    const upper = medianVals[Math.ceil(rank)];
    return lower + (upper - lower) * (rank - Math.floor(rank));
  };
  const percentileMarkers = [
    { name: "p50", value: percentile(0.5) },
    { name: "p90", value: percentile(0.9) },
    { name: "p95", value: percentile(0.95) },
    { name: "p99", value: percentile(0.99) },
  ];

  // Durations span orders of magnitude, so bin and smooth them in log10 space.
  const logs = medianVals.map((duration) => Math.log10(duration));
  const logMin =
    logs.length > 0 ? Math.floor(logs[0] / LOG_BIN_WIDTH) * LOG_BIN_WIDTH : 0;
  const logMax =
    logs.length > 0
      ? Math.max(
          logMin + LOG_BIN_WIDTH,
          Math.ceil(logs[logs.length - 1] / LOG_BIN_WIDTH) * LOG_BIN_WIDTH,
        )
      : 1;
  const logMean = logs.reduce((sum, x) => sum + x, 0) / logs.length;
  const logDeviation = Math.sqrt(
    logs.reduce((sum, x) => sum + (x - logMean) ** 2, 0) / logs.length,
  );
  // Durations have several peaks, which Silverman's rule smooths away; use half its robust form.
  const logQuartile = (q: number) =>
    logs[Math.min(logs.length - 1, Math.floor(q * logs.length))];
  const logSpread = Math.min(
    logDeviation,
    (logQuartile(0.75) - logQuartile(0.25)) / 1.34,
  );
  const bandwidth = Math.max(
    LOG_BIN_WIDTH,
    (0.9 * logSpread * logs.length ** -0.2) / 2,
  );
  const binCount = Math.round((logMax - logMin) / LOG_BIN_WIDTH);
  const distribution: DistributionBin[] =
    logs.length === 0
      ? []
      : Array.from({ length: binCount }, (_, i) => {
          const lower = logMin + i * LOG_BIN_WIDTH;
          const upper = lower + LOG_BIN_WIDTH;
          const x = lower + LOG_BIN_WIDTH / 2;
          const last = i === binCount - 1;
          const count = logs.filter(
            (l) => l >= lower && (l < upper || last),
          ).length;
          const density =
            (logs.reduce(
              (sum, l) => sum + Math.exp(-0.5 * ((x - l) / bandwidth) ** 2),
              0,
            ) /
              (bandwidth * Math.sqrt(2 * Math.PI))) *
            LOG_BIN_WIDTH;
          const cumulative =
            (100 * logs.filter((l) => l <= x).length) / logs.length;
          return { x, lower, upper, count, density, cumulative };
        });
  const distributionTicks = LOG_TICKS.filter((t) => t >= logMin && t <= logMax);

  return (
    <PortalCard
      icon={<LineChartOutlined />}
      titleBits={[<span key="title">Trends</span>]}
    >
      <PortalCard
        type="inner"
        icon={<ClockCircleFilled />}
        titleBits={[
          <span key="invocation-durations">Invocation Durations</span>,
        ]}
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
            style={{ minWidth: 190 }}
            value={limit}
            onChange={(value) =>
              navigate({ search: (prev) => ({ ...prev, limit: value }) })
            }
            options={TRENDS_LIMITS.map((value) => ({
              value,
              label: `Latest ${value} invocations`,
            }))}
          />,
          <Select
            key="command-filter"
            mode="multiple"
            allowClear
            placeholder="All commands"
            style={{ minWidth: 220 }}
            value={commands}
            onChange={(value) => {
              setCommands(value);
              setPinned(null);
              setBrushRange(null);
            }}
            options={BAZEL_COMMANDS.map((command) => ({
              label: command,
              value: command,
            }))}
          />,
        ]}
      >
        <Row>
          <Space size="large">
            <Statistic
              title={range ? "In window" : "Total"}
              value={
                range
                  ? visiblePoints.length
                  : data?.findBazelInvocations.totalCount
              }
              styles={{
                content: {
                  color: "#82ca9d",
                },
              }}
            />
            <Statistic
              title="Average"
              value={
                visiblePoints.length === 0
                  ? "-"
                  : readableDurationFromMilliseconds(avg, {
                      smallestUnit: "ms",
                    })
              }
              styles={{
                content: {
                  color: "#82ca9d",
                },
              }}
            />
            <Statistic
              title="Median"
              value={
                visiblePoints.length === 0
                  ? "-"
                  : readableDurationFromMilliseconds(median, {
                      smallestUnit: "ms",
                    })
              }
              styles={{
                content: {
                  color: "#8884d8",
                },
              }}
            />
            <Statistic
              title="Max"
              value={
                visiblePoints.length === 0
                  ? "-"
                  : readableDurationFromMilliseconds(max, {
                      smallestUnit: "ms",
                    })
              }
            />
            <Statistic
              title="Min"
              value={
                visiblePoints.length === 0
                  ? "-"
                  : readableDurationFromMilliseconds(min, {
                      smallestUnit: "ms",
                    })
              }
            />
          </Space>
        </Row>
        <Row>
          <div ref={chartBox} style={{ position: "relative", width: "100%" }}>
            <ResponsiveContainer width="100%" height={300}>
              <AreaChart
                data={dataPoints}
                margin={{ top: 10, right: 90, left: 10, bottom: 0 }}
                style={{ cursor: "pointer" }}
                onClick={(state, event) => {
                  // Releasing a brush drag also fires a chart click.
                  if ((event.target as Element).closest(".recharts-brush")) {
                    return;
                  }
                  const point = dataPoints.find(
                    (p) => p.from === Number(state.activeLabel),
                  );
                  if (point) {
                    setPinned({
                      ...point,
                      x: Number(state.activeCoordinate?.x ?? 0),
                    });
                  }
                }}
              >
                <defs>
                  <linearGradient id="colorUv" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#8884d8" stopOpacity={0.8} />
                    <stop offset="95%" stopColor="#8884d8" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis
                  dataKey="from"
                  type="number"
                  scale="time"
                  domain={["dataMin", "dataMax"]}
                  ticks={timeTicks}
                  tickFormatter={(v: number) => dayjs(v).format(timeTickFormat)}
                />
                <YAxis
                  width={70}
                  tickFormatter={(v: number) =>
                    readableDurationFromMilliseconds(v, { smallestUnit: "s" })
                  }
                />
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <Tooltip
                  active={pinned ? false : undefined}
                  position={{ y: 0 }}
                  separator=": "
                  labelFormatter={(label) => (
                    <>
                      {dayjs(Number(label)).format("YYYY-MM-DD HH:mm:ss")}
                      <br />
                      <small>Click to pin, then open the invocation</small>
                    </>
                  )}
                  formatter={(value) => [
                    spelledOutDurationFromMilliseconds(Number(value)),
                    "Duration",
                  ]}
                />
                <Area
                  type="monotone"
                  dataKey="duration"
                  stroke="#8884d8"
                  fillOpacity={1}
                  fill="url(#colorUv)"
                  activeDot={{
                    r: 6,
                    stroke: token.colorBgContainer,
                    strokeWidth: 2,
                  }}
                />
                {pinned && (
                  <ReferenceDot
                    x={pinned.from}
                    y={pinned.duration}
                    r={7}
                    fill="#8884d8"
                    stroke={token.colorText}
                    strokeWidth={2}
                  />
                )}
                <Brush
                  dataKey="from"
                  height={40}
                  stroke="#8884d8"
                  fill={token.colorBgContainer}
                  startIndex={range?.startIndex}
                  endIndex={range?.endIndex}
                  tickFormatter={(v: number) => dayjs(v).format("MMM D HH:mm")}
                  onChange={({ startIndex, endIndex }) => {
                    setPinned(null);
                    setBrushRange(
                      startIndex === 0 && endIndex === dataPoints.length - 1
                        ? null
                        : { startIndex, endIndex },
                    );
                  }}
                >
                  <AreaChart data={dataPoints}>
                    <Area
                      type="monotone"
                      dataKey="duration"
                      stroke="#8884d8"
                      fill="#8884d8"
                      fillOpacity={0.3}
                      isAnimationActive={false}
                    />
                  </AreaChart>
                </Brush>
              </AreaChart>
            </ResponsiveContainer>
            {pinned && (
              <div
                style={{
                  position: "absolute",
                  top: 0,
                  left: Math.min(
                    pinned.x + 12,
                    (chartBox.current?.clientWidth ?? 1500) - 300,
                  ),
                  padding: "8px 12px",
                  background: token.colorBgElevated,
                  border: `1px solid ${token.colorBorderSecondary}`,
                  color: token.colorText,
                }}
              >
                <Button
                  type="text"
                  size="small"
                  aria-label="Unpin"
                  onClick={() => setPinned(null)}
                  style={{ position: "absolute", top: 2, right: 2 }}
                >
                  ×
                </Button>
                <div style={{ paddingRight: 24 }}>
                  {dayjs(pinned.from).format("YYYY-MM-DD HH:mm:ss")}
                </div>
                <div style={{ color: "#8884d8" }}>
                  Duration:{" "}
                  {spelledOutDurationFromMilliseconds(pinned.duration)}
                </div>
                <div>
                  Open the{" "}
                  <Link
                    to="/bazel-invocations/$invocationID"
                    params={{ invocationID: pinned.invocationId }}
                  >
                    invocation
                  </Link>
                </div>
              </div>
            )}
          </div>
        </Row>
      </PortalCard>
      <PortalCard
        type="inner"
        icon={<BarChartOutlined />}
        titleBits={[
          <span key="duration-distribution">Duration Distribution</span>,
        ]}
      >
        <Row>
          <Space size="large">
            {percentileMarkers.map((marker) => (
              <Statistic
                key={marker.name}
                title={marker.name}
                value={
                  visiblePoints.length === 0
                    ? "-"
                    : readableDurationFromMilliseconds(marker.value, {
                        smallestUnit: "ms",
                      })
                }
              />
            ))}
          </Space>
        </Row>
        <Typography.Text type="secondary">
          Invocations per duration, with a smoothed density curve
        </Typography.Text>
        <Row>
          <ResponsiveContainer width="100%" height={220}>
            <ComposedChart
              data={distribution}
              margin={{ top: 20, right: 90, left: 10, bottom: 0 }}
            >
              <XAxis
                dataKey="x"
                type="number"
                domain={[logMin, logMax]}
                ticks={distributionTicks}
                tickFormatter={(v: number) => shortDuration(10 ** v)}
              />
              <YAxis yAxisId="count" width={70} allowDecimals={false} />
              <YAxis
                yAxisId="spacer"
                orientation="right"
                width={60}
                tick={false}
                axisLine={false}
              />
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <Tooltip
                separator=": "
                labelFormatter={(_, payload) => {
                  const bin = payload?.[0]?.payload as
                    | DistributionBin
                    | undefined;
                  return bin
                    ? `${readableDurationFromMilliseconds(10 ** bin.lower)} – ${readableDurationFromMilliseconds(10 ** bin.upper)}`
                    : "";
                }}
                formatter={(value, name) => {
                  const v = Number(value);
                  if (name === "Invocations") {
                    return [
                      `${v} (${((v / logs.length) * 100).toFixed(1)}%)`,
                      name,
                    ];
                  }
                  if (name === "Finished within") {
                    return [`${v.toFixed(1)}%`, name];
                  }
                  return [v.toFixed(1), name];
                }}
              />
              <Bar
                yAxisId="count"
                dataKey="count"
                name="Invocations"
                fill="#82ca9d"
                fillOpacity={0.35}
                isAnimationActive={false}
              />
              <Line
                yAxisId="count"
                dataKey="density"
                name="Smoothed"
                type="monotone"
                stroke="#2e9e5f"
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
              {logs.length > 0 &&
                percentileMarkers.map((marker) => (
                  <ReferenceLine
                    key={marker.name}
                    yAxisId="count"
                    x={Math.log10(marker.value)}
                    stroke="#8884d8"
                    strokeDasharray="4 2"
                    label={{
                      value: marker.name,
                      position: "top",
                      fill: token.colorText,
                    }}
                  />
                ))}
            </ComposedChart>
          </ResponsiveContainer>
        </Row>
        <Typography.Text type="secondary">
          Invocations per duration, with the share finished within each duration
        </Typography.Text>
        <Row>
          <ResponsiveContainer width="100%" height={220}>
            <ComposedChart
              data={distribution}
              margin={{ top: 20, right: 90, left: 10, bottom: 0 }}
            >
              <XAxis
                dataKey="x"
                type="number"
                domain={[logMin, logMax]}
                ticks={distributionTicks}
                tickFormatter={(v: number) => shortDuration(10 ** v)}
              />
              <YAxis yAxisId="count" width={70} allowDecimals={false} />
              <YAxis
                yAxisId="share"
                orientation="right"
                width={60}
                domain={[0, 100]}
                ticks={[0, 25, 50, 75, 90, 100]}
                tickFormatter={(v: number) => `${v}%`}
              />
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <Tooltip
                separator=": "
                labelFormatter={(_, payload) => {
                  const bin = payload?.[0]?.payload as
                    | DistributionBin
                    | undefined;
                  return bin
                    ? `${readableDurationFromMilliseconds(10 ** bin.lower)} – ${readableDurationFromMilliseconds(10 ** bin.upper)}`
                    : "";
                }}
                formatter={(value, name) => {
                  const v = Number(value);
                  if (name === "Invocations") {
                    return [
                      `${v} (${((v / logs.length) * 100).toFixed(1)}%)`,
                      name,
                    ];
                  }
                  if (name === "Finished within") {
                    return [`${v.toFixed(1)}%`, name];
                  }
                  return [v.toFixed(1), name];
                }}
              />
              <Bar
                yAxisId="count"
                dataKey="count"
                name="Invocations"
                fill="#82ca9d"
                fillOpacity={0.35}
                isAnimationActive={false}
              />
              <Line
                yAxisId="share"
                dataKey="cumulative"
                name="Finished within"
                type="monotone"
                stroke="#8884d8"
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
              {logs.length > 0 &&
                percentileMarkers.map((marker) => (
                  <ReferenceLine
                    key={marker.name}
                    yAxisId="count"
                    x={Math.log10(marker.value)}
                    stroke="#8884d8"
                    strokeDasharray="4 2"
                    label={{
                      value: marker.name,
                      position: "top",
                      fill: token.colorText,
                    }}
                  />
                ))}
            </ComposedChart>
          </ResponsiveContainer>
        </Row>
      </PortalCard>
    </PortalCard>
  );
};
