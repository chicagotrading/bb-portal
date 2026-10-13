import {
  Tooltip as AntTooltip,
  Breadcrumb,
  Empty,
  Flex,
  Segmented,
  Space,
  Typography,
  theme,
} from "antd";
import type React from "react";
import { useMemo, useState } from "react";
import {
  ResponsiveContainer,
  Tooltip,
  Treemap,
  type TreemapNode,
} from "recharts";
import {
  fitText,
  HEAT_COLORS,
  type HeatTreeNode,
  heatColor,
  heatLeaves,
  heatScale,
  type MapRow,
  type MapView,
  mapCells,
  textColorOn,
  zoomTrail,
} from "./utils";

export const ViewSwitch: React.FC<{
  view: MapView;
  onChange: (view: MapView) => void;
}> = ({ view, onChange }) => (
  <Segmented<MapView>
    value={view}
    onChange={onChange}
    options={[
      { value: "table", label: "Table" },
      { value: "map", label: "Map" },
    ]}
  />
);

export interface SizeMetric<N> {
  label: string;
  of: (node: N) => number;
}

export interface HeatMetric<N> extends SizeMetric<N> {
  // The legend title, e.g. "Changes per action".
  legend: string;
  format: (value: number) => string;
}

type ScaleMode = "all" | "level";

const Choice = <T extends string>({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: T;
  onChange: (value: T) => void;
  options: { value: T; label: string }[];
}) => (
  <Space size={6}>
    <Typography.Text type="secondary">{label}</Typography.Text>
    <Segmented<T>
      size="small"
      value={value}
      onChange={onChange}
      options={options}
    />
  </Space>
);

const HeatLegend: React.FC<{
  title: string;
  min: string;
  max: string;
  log: boolean;
  mode: ScaleMode;
}> = ({ title, min, max, log, mode }) => (
  <AntTooltip
    title={`${log ? "Log" : "Linear"} scale from the lowest to the highest value ${mode === "all" ? "of all actions" : "of the cells shown"}.`}
  >
    <Space size={6}>
      <Typography.Text type="secondary">{title}</Typography.Text>
      <Typography.Text type="secondary">{min}</Typography.Text>
      <div
        role="img"
        aria-label={`${title} from ${min} to ${max}`}
        style={{
          width: 160,
          height: 10,
          background: `linear-gradient(to right, ${HEAT_COLORS.join(", ")})`,
        }}
      />
      <Typography.Text type="secondary">{max}</Typography.Text>
    </Space>
  </AntTooltip>
);

const NodeTooltip = <R extends MapRow, K extends string>({
  active,
  payload,
  byId,
  rows,
}: {
  active?: boolean;
  payload?: Array<{ payload?: { id?: string } }>;
  byId: Map<string, HeatTreeNode<R, K>>;
  rows: (node: HeatTreeNode<R, K>) => [string, React.ReactNode][];
}) => {
  const { token } = theme.useToken();
  const id = payload?.[0]?.payload?.id;
  const node = id === undefined ? undefined : byId.get(id);
  if (!active || !node) return null;
  const lines: [string, React.ReactNode][] = [
    ...(node.row
      ? ([
          ["Target", node.row.targetLabel],
          ["Mnemonic", node.row.mnemonic],
          ["Output", node.row.primaryOutput || "no outputs"],
        ] as [string, string][])
      : ([["Actions", node.actions]] as [string, number][])),
    ...rows(node),
  ];
  return (
    <div
      style={{
        maxWidth: 520,
        padding: "8px 12px",
        background: token.colorBgElevated,
        border: `1px solid ${token.colorBorderSecondary}`,
        borderRadius: token.borderRadius,
        boxShadow: token.boxShadowSecondary,
        color: token.colorText,
        fontSize: 13,
      }}
    >
      <div style={{ fontWeight: 600, wordBreak: "break-all" }}>
        {node.row ? `${node.row.mnemonic} ${node.row.targetLabel}` : node.path}
      </div>
      <table style={{ marginTop: 4, borderSpacing: "12px 0", marginLeft: -12 }}>
        <tbody>
          {lines.map(([label, value]) => (
            <tr key={label}>
              <td style={{ color: token.colorTextSecondary }}>{label}</td>
              <td style={{ wordBreak: "break-all" }}>{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div style={{ marginTop: 4, color: token.colorTextSecondary }}>
        {node.row ? "Click to open its history" : "Click to zoom in"}
      </div>
    </div>
  );
};

// A treemap of packages, targets and actions, one level at a time, sized
// by one metric and colored by another.
export const HeatMap = <
  R extends MapRow,
  K extends string,
  S extends string,
  H extends string,
>({
  root,
  sizes,
  heats,
  detail,
  tooltip,
  emptyText,
  onSelect,
}: {
  root: HeatTreeNode<R, K>;
  sizes: Record<S, SizeMetric<HeatTreeNode<R, K>>>;
  heats: Record<H, HeatMetric<HeatTreeNode<R, K>>>;
  // The second line of a cell; size null means sized by action count.
  detail: (node: HeatTreeNode<R, K>, size: S | null) => string;
  tooltip: (node: HeatTreeNode<R, K>) => [string, React.ReactNode][];
  emptyText: string;
  onSelect: (row: R) => void;
}) => {
  const { token } = theme.useToken();
  const sizeKeys = Object.keys(sizes) as S[];
  const heatKeys = Object.keys(heats) as H[];
  const [size, setSize] = useState<S>(sizeKeys[0]);
  const [heat, setHeat] = useState<H>(heatKeys[0]);
  const [mode, setMode] = useState<ScaleMode>("all");
  const [zoom, setZoom] = useState<string[]>([]);
  const trail = useMemo(() => zoomTrail(root, zoom), [root, zoom]);
  const current = trail[trail.length - 1];
  const byId = useMemo(
    () => new Map(current.children.map((child) => [child.id, child])),
    [current],
  );
  const heatOf = heats[heat].of;
  const leafHeats = useMemo(
    () => heatLeaves(root).map((leaf) => heatOf(leaf)),
    [root, heatOf],
  );
  const sizeOf = Object.fromEntries(
    sizeKeys.map((key) => [key, sizes[key].of]),
  ) as Record<S, (node: HeatTreeNode<R, K>) => number>;
  const { cells, size: shownSize, hidden } = mapCells(current, sizeOf, size);
  const scale = heatScale(
    mode === "all"
      ? leafHeats
      : cells.flatMap((c) => {
          const node = byId.get(c.id);
          return node ? [heatOf(node)] : [];
        }),
  );

  if (root.children.length === 0) return <Empty description={emptyText} />;

  const open = (id: string) => {
    const node = byId.get(id);
    if (node?.row) onSelect(node.row);
    else if (node) setZoom([...trail.slice(1).map((n) => n.id), node.id]);
  };

  const cell = (props: TreemapNode) => {
    const { x, y, width, height, depth } = props;
    const node = byId.get(props.id as string);
    if (depth === 0 || !node) return <g />;
    const fill = heatColor(scale.at(heatOf(node)));
    const color = textColorOn(fill);
    // Bigger text on big cells, none on cells too small to read.
    const big = width >= 240 && height >= 90;
    const nameSize = big ? 15 : 12;
    const detailSize = big ? 13 : 11;
    const name =
      height >= nameSize + 6
        ? fitText(node.name, width - 10, nameSize * 0.6)
        : "";
    const second =
      name && height >= nameSize + detailSize + 14
        ? fitText(detail(node, shownSize), width - 10, detailSize * 0.58)
        : "";
    return (
      <g style={{ cursor: "pointer" }}>
        <rect
          x={x}
          y={y}
          width={width}
          height={height}
          fill={fill}
          stroke={token.colorBgContainer}
          strokeWidth={2}
        />
        {name && (
          <text
            x={x + 6}
            y={y + nameSize + 3}
            fill={color}
            fontSize={nameSize}
            fontWeight={600}
            pointerEvents="none"
          >
            {name}
          </text>
        )}
        {second && (
          <text
            x={x + 6}
            y={y + nameSize + detailSize + 7}
            fill={color}
            fillOpacity={0.85}
            fontSize={detailSize}
            pointerEvents="none"
          >
            {second}
          </text>
        )}
      </g>
    );
  };

  const sizeLabel = (key: S | null) =>
    (key === null ? "Actions" : sizes[key].label).toLowerCase();
  const notes = [
    shownSize !== size &&
      `Nothing here has ${sizeLabel(size)}, so areas are ${sizeLabel(shownSize)}.`,
    hidden > 0 &&
      `${hidden} of ${current.children.length} without ${sizeLabel(shownSize)} are not shown.`,
  ].filter((note): note is string => typeof note === "string");

  return (
    <Space orientation="vertical" size="small" style={{ width: "100%" }}>
      <Flex justify="end" wrap gap={16}>
        <Choice
          label="Area"
          value={size}
          onChange={setSize}
          options={sizeKeys.map((key) => ({
            value: key,
            label: sizes[key].label,
          }))}
        />
        {heatKeys.length > 1 && (
          <Choice
            label="Color"
            value={heat}
            onChange={setHeat}
            options={heatKeys.map((key) => ({
              value: key,
              label: heats[key].label,
            }))}
          />
        )}
        <Choice<ScaleMode>
          label="Color scale"
          value={mode}
          onChange={setMode}
          options={[
            { value: "all", label: "All actions" },
            { value: "level", label: "This level" },
          ]}
        />
      </Flex>
      <Flex justify="space-between" align="center" wrap gap={8}>
        <Breadcrumb
          items={trail.map((node, i) => ({
            key: node.id,
            // Nodes with one child show the same cells as that child.
            title:
              i === trail.length - 1 || node.children.length === 1 ? (
                node.name
              ) : (
                <Typography.Link
                  onClick={() =>
                    setZoom(trail.slice(1, i + 1).map((n) => n.id))
                  }
                >
                  {node.name}
                </Typography.Link>
              ),
          }))}
        />
        <HeatLegend
          title={heats[heat].legend}
          min={heats[heat].format(scale.min)}
          max={heats[heat].format(scale.max)}
          log={scale.log}
          mode={mode}
        />
      </Flex>
      {notes.length > 0 && (
        <Typography.Text type="secondary">{notes.join(" ")}</Typography.Text>
      )}
      <ResponsiveContainer width="100%" height={560}>
        <Treemap
          key={current.id}
          data={cells}
          dataKey="value"
          nameKey="name"
          aspectRatio={4 / 3}
          isAnimationActive={false}
          content={cell}
          onClick={(node) => open(node.id as string)}
        >
          <Tooltip
            isAnimationActive={false}
            content={<NodeTooltip byId={byId} rows={tooltip} />}
          />
        </Treemap>
      </ResponsiveContainer>
    </Space>
  );
};
