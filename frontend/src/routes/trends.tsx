import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { INVOCATION_FILTER_SEARCH } from "@/components/InvocationFilters/utils";
import { TrendsPage } from "@/components/pages/Trends";
import { env } from "@/utils/env";
import { requireFeature } from "@/utils/featureGuard";
import { generatePageTitle } from "@/utils/generatePageTitle";
import { INVOCATION_WINDOWS } from "@/utils/invocationWindows";

const TrendsSearchSchema = z.object({
  limit: z.number().int().min(1).max(10000).optional().catch(undefined),
  window: z.enum(Object.keys(INVOCATION_WINDOWS)).optional().catch(undefined),
  ...INVOCATION_FILTER_SEARCH,
});

export const Route = createFileRoute("/trends")({
  component: TrendsPage,
  validateSearch: (search) => TrendsSearchSchema.parse(search),
  beforeLoad: requireFeature(env.featureFlags?.bes?.pageTrends),
  head: (_ctx) => ({ meta: [{ title: generatePageTitle(["Trends"]) }] }),
});
