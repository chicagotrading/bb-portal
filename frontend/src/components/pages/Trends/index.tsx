import { ClockCircleFilled, LineChartOutlined } from "@ant-design/icons";
import { useQuery } from "@apollo/client/react";
import { Link } from "@tanstack/react-router";
import { Button, Row, Select, Space, Statistic, theme } from "antd";
import type React from "react";
import { useEffect, useMemo, useState } from "react";
import {
  Area,
  AreaChart,
  Brush,
  CartesianGrid,
  ReferenceDot,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PortalCard } from "@/components/PortalCard";
import type {
  BazelInvocationNodeFragment,
  FindBuildTimesQueryVariables,
} from "@/graphql/__generated__/graphql";
import dayjs from "@/lib/dayjs";
import {
  readableDurationFromMilliseconds,
  spelledOutDurationFromMilliseconds,
} from "@/utils/time";
import FIND_BUILD_DURATIONS from "./index.graphql";

const BAZEL_COMMANDS = [
  "build",
  "test",
  "run",
  "coverage",
  "query",
  "cquery",
  "aquery",
  "fetch",
];

export const TrendsPage: React.FC = () => {
  const { token } = theme.useToken();
  const [commands, setCommands] = useState<string[]>([]);
  // Invocation whose tooltip was pinned by a click; it stays put and links to the invocation.
  const [pinned, setPinned] = useState<{
    invocationId: string;
    from: number;
    duration: number;
    x: number;
  } | null>(null);
  useEffect(() => {
    const unpin = (event: KeyboardEvent) => {
      if (event.key === "Escape") setPinned(null);
    };
    window.addEventListener("keydown", unpin);
    return () => window.removeEventListener("keydown", unpin);
  }, []);
  const [brushRange, setBrushRange] = useState<{
    startIndex: number;
    endIndex: number;
  } | null>(null);
  const variables: FindBuildTimesQueryVariables = {
    first: 1000,
    where: {
      startedAtNotNil: true,
      endedAtNotNil: true,
      ...(commands.length > 0 && { commandIn: commands }),
    },
  };

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
    timeSpan <= 86_400_000
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
          <div style={{ position: "relative" }}>
            <AreaChart
              width={1500}
              height={300}
              data={dataPoints}
              margin={{ top: 10, right: 90, left: 10, bottom: 0 }}
              style={{ cursor: "pointer" }}
              onClick={(state) => {
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
                contentStyle={{
                  background: token.colorBgElevated,
                  borderColor: token.colorBorderSecondary,
                }}
                labelStyle={{ color: token.colorText }}
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
            </AreaChart>
            {pinned && (
              <div
                style={{
                  position: "absolute",
                  top: 0,
                  left: Math.min(pinned.x + 12, 1500 - 300),
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
    </PortalCard>
  );
};
