import type {
  ActionHistoryQuery,
  ActionInsightsQuery,
  BazelInvocationWhereInput,
} from "@/graphql/__generated__/graphql";

export type ActionInsightsData = ActionInsightsQuery["actionInsights"];
export type ImpactRow = ActionInsightsData["impact"][number];
export type ChurnRow = ActionInsightsData["churn"][number];
export type CauseRow = ActionInsightsData["causes"][number];
export type HistoryEntry = ActionHistoryQuery["actionHistory"][number];

export interface SelectedAction {
  keyId: string;
  targetLabel: string;
  mnemonic: string;
  primaryOutput: string;
}

// The invocation set the history must be read over, same as the insights.
export interface InvocationScope {
  where: BazelInvocationWhereInput;
  invocationLimit: number;
}
