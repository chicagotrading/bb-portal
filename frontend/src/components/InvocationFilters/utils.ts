import { z } from "zod";
import type {
  BazelInvocationWhereInput,
  SourceControlWhereInput,
} from "@/graphql/__generated__/graphql";
import { invocationWindowWhere } from "@/utils/invocationWindows";

export interface InvocationFilters {
  repo?: string;
  ref?: string;
  users?: string[];
  // true for CI only, false for everything else.
  ci?: boolean;
}

// The URL search params of the filters and commands, to spread into a
// route's search schema.
export const INVOCATION_FILTER_SEARCH = {
  command: z.array(z.string()).optional().catch(undefined),
  repo: z.string().min(1).optional().catch(undefined),
  ref: z.string().min(1).optional().catch(undefined),
  // ?user=alice arrives as a string.
  user: z
    .preprocess(
      (value) => (typeof value === "string" ? [value] : value),
      z.array(z.string().min(1)).min(1),
    )
    .optional()
    .catch(undefined),
  ci: z.boolean().optional().catch(undefined),
};

const searchSchema = z.object(INVOCATION_FILTER_SEARCH);
export type InvocationFilterSearch = z.infer<typeof searchSchema>;

export const parseInvocationFilterSearch = (
  search: unknown,
): InvocationFilterSearch => searchSchema.parse(search);

// The search params for commands and filters; empty lists drop out.
export const invocationFilterSearch = (
  commands: string[],
  filters: InvocationFilters,
): InvocationFilterSearch => ({
  command: commands.length > 0 ? commands : undefined,
  repo: filters.repo,
  ref: filters.ref,
  user: filters.users && filters.users.length > 0 ? filters.users : undefined,
  ci: filters.ci,
});

// CI invocations are those with a source control repo: the metadata
// extractor only sets one from GitLab CI variables, and unlike a Build it
// does not depend on the portal's buildKey.
const CI_SOURCE_CONTROL: SourceControlWhereInput = { repoNotNil: true };

// The invocations of a time window and commands, matching a repo and ref
// in one source control, users, and CI or not.
export const invocationFiltersWhere = (
  window: string,
  commands: string[],
  filters: InvocationFilters,
  now: Date,
): BazelInvocationWhereInput => {
  const sourceControl: SourceControlWhereInput = {
    ...(filters.ci === true && CI_SOURCE_CONTROL),
    ...(filters.repo && { repo: filters.repo }),
    ...(filters.ref && { ref: filters.ref }),
  };
  return {
    ...invocationWindowWhere(window, commands, now),
    ...(Object.keys(sourceControl).length > 0 && {
      hasSourceControlWith: [sourceControl],
    }),
    ...(filters.users &&
      filters.users.length > 0 && { usernameIn: filters.users }),
    ...(filters.ci === false && {
      not: { hasSourceControlWith: [CI_SOURCE_CONTROL] },
    }),
  };
};

// Describes the filtered invocations, e.g.
// "CI invocations of tsg/nebula on features/pilot by alice".
export const describeInvocations = (filters: InvocationFilters): string =>
  [
    filters.ci === true && "CI",
    filters.ci === false && "non-CI",
    "invocations",
    filters.repo && `of ${filters.repo}`,
    filters.ref && `on ${filters.ref}`,
    filters.users &&
      filters.users.length > 0 &&
      `by ${filters.users.join(", ")}`,
  ]
    .filter((part): part is string => typeof part === "string")
    .join(" ");

export interface FilterOption {
  value: string;
  invocations: number;
}

// The refs to offer: those of the repo, or of every repo with the counts
// of a ref in several repos added up, most invocations first.
export const refOptions = (
  refs: { repo?: string | null; ref: string; invocations: number }[],
  repo: string | undefined,
): FilterOption[] => {
  const counts = new Map<string, number>();
  for (const r of refs) {
    if (repo && r.repo !== repo) continue;
    counts.set(r.ref, (counts.get(r.ref) ?? 0) + r.invocations);
  }
  return [...counts]
    .map(([value, invocations]) => ({ value, invocations }))
    .sort(
      (a, b) => b.invocations - a.invocations || a.value.localeCompare(b.value),
    );
};
