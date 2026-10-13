import type { BazelInvocationWhereInput } from "@/graphql/__generated__/graphql";

// Hours to look back, or undefined for every invocation still kept.
export const INVOCATION_WINDOWS: Record<
  string,
  { label: string; hours?: number }
> = {
  all: { label: "All kept" },
  "24h": { label: "Last 24 hours", hours: 24 },
  "7d": { label: "Last 7 days", hours: 7 * 24 },
  "30d": { label: "Last 30 days", hours: 30 * 24 },
};

export const invocationWindowWhere = (
  window: string,
  commands: string[],
  now: Date,
): BazelInvocationWhereInput => {
  const hours = INVOCATION_WINDOWS[window]?.hours;
  return {
    startedAtNotNil: true,
    ...(hours && {
      startedAtGT: new Date(now.getTime() - hours * 3_600_000).toISOString(),
    }),
    ...(commands.length > 0 && { commandIn: commands }),
  };
};
