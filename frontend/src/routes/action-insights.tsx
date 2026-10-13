import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { ActionInsightsPage } from "@/components/pages/ActionInsights";
import {
  ACTION_INSIGHTS_TABS,
  MAP_VIEWS,
} from "@/components/pages/ActionInsights/utils";
import { env } from "@/utils/env";
import { requireFeature } from "@/utils/featureGuard";
import { generatePageTitle } from "@/utils/generatePageTitle";
import { INVOCATION_WINDOWS } from "@/utils/invocationWindows";

const ActionInsightsSearchSchema = z.object({
  limit: z.number().int().min(1).max(500).optional().catch(undefined),
  window: z.enum(Object.keys(INVOCATION_WINDOWS)).optional().catch(undefined),
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
  tab: z.enum(ACTION_INSIGHTS_TABS).optional().catch(undefined),
  churnView: z.enum(MAP_VIEWS).optional().catch(undefined),
  impactView: z.enum(MAP_VIEWS).optional().catch(undefined),
});

export const Route = createFileRoute("/action-insights")({
  component: ActionInsightsPage,
  validateSearch: (search) => ActionInsightsSearchSchema.parse(search),
  beforeLoad: requireFeature(env.featureFlags?.bes?.pageTrends),
  head: (_ctx) => ({
    meta: [{ title: generatePageTitle(["Action Insights"]) }],
  }),
});
