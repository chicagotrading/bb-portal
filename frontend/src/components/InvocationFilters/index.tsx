import { useQuery } from "@apollo/client/react";
import { Flex, Segmented, Select, Tooltip, Typography } from "antd";
import type React from "react";
import { useMemo, useState } from "react";
import type { BazelInvocationWhereInput } from "@/graphql/__generated__/graphql";
import { BAZEL_COMMANDS } from "@/utils/bazelCommands";
import {
  ACTION_INSIGHTS_FILTER_OPTIONS,
  INVOCATION_FILTER_OPTIONS,
} from "./index.graphql";
import {
  type FilterOption,
  type InvocationFilterSearch,
  type InvocationFilters,
  refOptions,
} from "./utils";

const NO_COMMANDS: string[] = [];

// The commands and filters of the URL, stable while other params change.
export const useInvocationFilterSearch = (
  search: InvocationFilterSearch,
): { commands: string[]; filters: InvocationFilters } => {
  const commands = search.command ?? NO_COMMANDS;
  const { repo, ref, user, ci } = search;
  const filters = useMemo<InvocationFilters>(
    () => ({ repo, ref, users: user, ci }),
    [repo, ref, user, ci],
  );
  return { commands, filters };
};

const NO_OPTIONS: FilterOption[] = [];

const optionRender = (option: {
  data: { label?: React.ReactNode; invocations?: number };
}) => (
  <Flex justify="space-between" gap="small">
    <Typography.Text ellipsis>{option.data.label}</Typography.Text>
    {option.data.invocations !== undefined && (
      <Typography.Text type="secondary">
        {option.data.invocations}
      </Typography.Text>
    )}
  </Flex>
);

// A single value Select that also takes a typed value, for a branch
// without counted invocations or when the options failed to load.
const FreeSelect: React.FC<{
  label: string;
  placeholder: string;
  value?: string;
  options: FilterOption[];
  loading: boolean;
  width: number;
  onChange: (value?: string) => void;
}> = ({ label, placeholder, value, options, loading, width, onChange }) => {
  const [search, setSearch] = useState("");
  const typed = search.trim();
  const items = useMemo(() => {
    const known = (v: string) => options.some((o) => o.value === v);
    return [
      ...(typed && !known(typed)
        ? [{ value: typed, label: `Use "${typed}"` }]
        : []),
      ...(value && value !== typed && !known(value)
        ? [{ value, label: value }]
        : []),
      ...options.map((o) => ({ ...o, label: o.value })),
    ];
  }, [options, typed, value]);
  return (
    <Select
      aria-label={label}
      allowClear
      showSearch={{
        searchValue: search,
        onSearch: setSearch,
        optionFilterProp: "value",
      }}
      placeholder={placeholder}
      loading={loading}
      style={{ width }}
      popupMatchSelectWidth={false}
      value={value}
      options={items}
      optionRender={optionRender}
      notFoundContent="Type a value"
      onChange={(v?: string) => {
        setSearch("");
        onChange(v);
      }}
    />
  );
};

type CiValue = "all" | "ci" | "other";
const CI_VALUES: Record<CiValue, boolean | undefined> = {
  all: undefined,
  ci: true,
  other: false,
};

export const InvocationFilterBar: React.FC<{
  // The time window and commands, which scope the options.
  optionsWhere: BazelInvocationWhereInput;
  // Offer only values of invocations with an ingested execution log.
  analyzedOnly?: boolean;
  commands: string[];
  filters: InvocationFilters;
  onCommandsChange: (commands: string[]) => void;
  onFiltersChange: (filters: InvocationFilters) => void;
}> = ({
  optionsWhere,
  analyzedOnly = false,
  commands,
  filters,
  onCommandsChange,
  onFiltersChange,
}) => {
  const analyzed = useQuery(ACTION_INSIGHTS_FILTER_OPTIONS, {
    variables: { where: optionsWhere },
    fetchPolicy: "cache-and-network",
    skip: !analyzedOnly,
  });
  const all = useQuery(INVOCATION_FILTER_OPTIONS, {
    variables: { where: optionsWhere },
    fetchPolicy: "cache-and-network",
    skip: analyzedOnly,
  });
  const { loading, error } = analyzedOnly ? analyzed : all;
  const options = analyzedOnly
    ? analyzed.data?.actionInsightsFilterOptions
    : all.data?.invocationFilterOptions;
  const repos = options?.repos ?? NO_OPTIONS;
  const refs = useMemo(
    () => refOptions(options?.refs ?? [], filters.repo),
    [options, filters.repo],
  );
  const users = options?.users ?? NO_OPTIONS;
  const ci: CiValue =
    filters.ci === true ? "ci" : filters.ci === false ? "other" : "all";
  // Picking a repo or ref implies CI; not CI clears them.
  const setSourceControl = (patch: Pick<InvocationFilters, "repo" | "ref">) =>
    onFiltersChange({
      ...filters,
      ...patch,
      ci: filters.ci === false ? undefined : filters.ci,
    });

  return (
    <Flex wrap gap="small" align="center">
      <Select
        aria-label="Commands"
        mode="multiple"
        allowClear
        placeholder="All commands"
        style={{ minWidth: 180 }}
        value={commands}
        onChange={onCommandsChange}
        options={BAZEL_COMMANDS.map((command) => ({
          label: command,
          value: command,
        }))}
      />
      <Segmented<CiValue>
        value={ci}
        onChange={(value) =>
          onFiltersChange({
            ...filters,
            ci: CI_VALUES[value],
            ...(value === "other" && { repo: undefined, ref: undefined }),
          })
        }
        options={[
          { value: "all", label: "All" },
          { value: "ci", label: "CI" },
          {
            value: "other",
            label: (
              <Tooltip title="Invocations without a source control repo, which the invocation metadata extractor sets from CI variables">
                Not CI
              </Tooltip>
            ),
          },
        ]}
      />
      <FreeSelect
        label="Repository"
        placeholder={error ? "Type a repo" : "All repos"}
        value={filters.repo}
        options={repos}
        loading={loading && !options}
        width={200}
        onChange={(repo) => setSourceControl({ repo })}
      />
      <FreeSelect
        label="Branch"
        placeholder={error ? "Type a branch" : "All branches"}
        value={filters.ref}
        options={refs}
        loading={loading && !options}
        width={240}
        onChange={(ref) => setSourceControl({ ref })}
      />
      <Select
        aria-label="Users"
        mode="tags"
        allowClear
        placeholder={error ? "Type a user" : "All users"}
        style={{ minWidth: 200 }}
        popupMatchSelectWidth={false}
        loading={loading && !options}
        value={filters.users ?? []}
        options={users.map((u) => ({ ...u, label: u.value }))}
        optionRender={optionRender}
        onChange={(value: string[]) =>
          onFiltersChange({
            ...filters,
            users: value.length > 0 ? value : undefined,
          })
        }
      />
    </Flex>
  );
};
