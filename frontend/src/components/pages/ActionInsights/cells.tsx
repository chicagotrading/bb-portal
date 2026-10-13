import { Link } from "@tanstack/react-router";
import { Flex, Space, Tag, Tooltip, Typography } from "antd";
import type React from "react";
import {
  type ActionChangeCause,
  ActionInputGroupKind,
} from "@/graphql/__generated__/graphql";
import {
  CAUSES,
  causeSegments,
  causeSummary,
  GROUP_KINDS,
  groupKind,
  groupTargetLabel,
  groupText,
  splitOutputPath,
} from "./utils";

const stopPropagation = (event: React.MouseEvent) => event.stopPropagation();

export const TargetLink: React.FC<{ label: string; maxWidth?: number }> = ({
  label,
  maxWidth,
}) => {
  const link = (
    <Link
      to="/targets"
      search={{ targetsTable: { where: [{ labelContainsFold: label }] } }}
      onClick={stopPropagation}
    >
      {label}
    </Link>
  );
  if (maxWidth === undefined) return link;
  return (
    <Typography.Text ellipsis={{ tooltip: label }} style={{ maxWidth }}>
      {link}
    </Typography.Text>
  );
};

export const OutputPath: React.FC<{ path: string; maxWidth?: number }> = ({
  path,
  maxWidth = 360,
}) => {
  const split = splitOutputPath(path);
  if (path === "") {
    return <Typography.Text type="secondary">no outputs</Typography.Text>;
  }
  return (
    <Space size={4}>
      {split.config && <Tag>{split.config}</Tag>}
      <Typography.Text ellipsis={{ tooltip: path }} style={{ maxWidth }}>
        {split.path}
      </Typography.Text>
    </Space>
  );
};

export const CauseTag: React.FC<{ cause?: ActionChangeCause | null }> = ({
  cause,
}) => {
  if (!cause) {
    return <Typography.Text type="secondary">unchanged</Typography.Text>;
  }
  const info = CAUSES[cause];
  return (
    <Tooltip title={info.description}>
      <Tag color={info.color}>{info.label}</Tag>
    </Tooltip>
  );
};

const Swatch: React.FC<{ color: string }> = ({ color }) => (
  <span
    aria-hidden
    style={{
      display: "inline-block",
      width: 10,
      height: 10,
      marginRight: 4,
      background: color,
    }}
  />
);

// The key to the colors of CauseBar.
export const CauseLegend: React.FC = () => (
  <Space size="middle" wrap>
    {Object.values(CAUSES).map((info) => (
      <Tooltip key={info.label} title={info.description}>
        <Typography.Text type="secondary">
          <Swatch color={info.color} />
          {info.label}
        </Typography.Text>
      </Tooltip>
    ))}
  </Space>
);

export const CauseBar: React.FC<{
  counts: { direct: number; both: number; dependency: number; unknown: number };
}> = ({ counts }) => {
  const segments = causeSegments(counts);
  const summary = causeSummary(segments);
  return (
    <Flex vertical gap={2} style={{ minWidth: 120 }}>
      <Flex
        role="img"
        aria-label={summary}
        style={{ width: "100%", height: 10 }}
      >
        {segments.map((segment) => (
          <div
            key={segment.cause}
            style={{
              width: `${segment.percent}%`,
              background: CAUSES[segment.cause].color,
            }}
          />
        ))}
      </Flex>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {summary}
      </Typography.Text>
    </Flex>
  );
};

export const GroupTag: React.FC<{
  name: string;
  kind?: ActionInputGroupKind;
  count?: number;
  maxWidth?: number;
}> = ({ name, kind, count, maxWidth = 420 }) => {
  const actualKind = kind ?? groupKind(name);
  const label =
    actualKind === ActionInputGroupKind.Generated
      ? groupTargetLabel(name)
      : undefined;
  const text = `${groupText(name)}${count === undefined ? "" : ` ×${count}`}`;
  return (
    <Tooltip title={`${GROUP_KINDS[actualKind].label} input: ${name}`}>
      <Tag
        color={GROUP_KINDS[actualKind].tagColor}
        style={{ maxWidth, overflow: "hidden", textOverflow: "ellipsis" }}
      >
        {label ? (
          <Link
            to="/targets"
            search={{ targetsTable: { where: [{ labelContainsFold: label }] } }}
            onClick={stopPropagation}
          >
            {text}
          </Link>
        ) : (
          text
        )}
      </Tag>
    </Tooltip>
  );
};

export const GroupTags: React.FC<{ names: string[]; max?: number }> = ({
  names,
  max,
}) => {
  if (names.length === 0) {
    return <Typography.Text type="secondary">-</Typography.Text>;
  }
  const shown = max === undefined ? names : names.slice(0, max);
  const hidden = names.length - shown.length;
  return (
    <Flex wrap gap={4}>
      {shown.map((name) => (
        <GroupTag key={name} name={name} />
      ))}
      {hidden > 0 && (
        <Tooltip
          title={names
            .slice(shown.length)
            .map((name) => <div key={name}>{name}</div>)}
        >
          <Tag>+{hidden} more</Tag>
        </Tooltip>
      )}
    </Flex>
  );
};
