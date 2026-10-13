import { theme } from "antd";
import type React from "react";
import {
  CartesianGrid,
  Cell,
  ReferenceLine,
  ResponsiveContainer,
  Scatter,
  ScatterChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import dayjs from "@/lib/dayjs";
import {
  readableDurationFromMilliseconds,
  spelledOutDurationFromMilliseconds,
} from "@/utils/time";
import type { HistoryEntry } from "./types";
import { CAUSES, type HistoryPoint, historyPoints } from "./utils";

export const HistoryChart: React.FC<{ entries: HistoryEntry[] }> = ({
  entries,
}) => {
  const { token } = theme.useToken();
  const points = historyPoints(entries);
  if (points.length === 0) return null;
  const executed = points.filter((point) => point.durationMs !== undefined);
  const first = points[0].time;
  const last = points[points.length - 1].time;
  const pad = Math.max((last - first) * 0.02, 3_600_000);
  return (
    <ResponsiveContainer width="100%" height={240}>
      <ScatterChart margin={{ top: 10, right: 20, left: 10, bottom: 0 }}>
        <CartesianGrid vertical={false} strokeDasharray="3 3" />
        <XAxis
          dataKey="time"
          type="number"
          scale="time"
          domain={[first - pad, last + pad]}
          tickFormatter={(v: number) => dayjs(v).format("MMM D HH:mm")}
        />
        <YAxis
          dataKey="durationMs"
          type="number"
          width={70}
          tickFormatter={(v: number) =>
            readableDurationFromMilliseconds(v, { smallestUnit: "ms" })
          }
        />
        <Tooltip
          cursor={false}
          content={({ active, payload }) => {
            const point = payload?.[0]?.payload as HistoryPoint | undefined;
            if (!active || !point) return null;
            return (
              <div
                style={{
                  padding: "8px 12px",
                  background: token.colorBgElevated,
                  border: `1px solid ${token.colorBorderSecondary}`,
                  color: token.colorText,
                }}
              >
                <div>{dayjs(point.time).format("YYYY-MM-DD HH:mm:ss")}</div>
                <div>
                  {spelledOutDurationFromMilliseconds(point.durationMs ?? 0)}
                </div>
                <div>
                  {point.cause ? `Changed: ${CAUSES[point.cause].label}` : ""}
                </div>
              </div>
            );
          }}
        />
        {points.map(
          (point) =>
            point.cause && (
              <ReferenceLine
                key={`change-${point.invocationId}`}
                x={point.time}
                stroke={CAUSES[point.cause].color}
                strokeDasharray="3 3"
                strokeOpacity={0.6}
              />
            ),
        )}
        <Scatter name="Executions" data={executed} isAnimationActive={false}>
          {executed.map((point) => (
            <Cell
              key={point.invocationId}
              fill={
                point.cause
                  ? CAUSES[point.cause].color
                  : token.colorTextQuaternary
              }
            />
          ))}
        </Scatter>
      </ScatterChart>
    </ResponsiveContainer>
  );
};
