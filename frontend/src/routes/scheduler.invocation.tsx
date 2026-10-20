import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { SchedulerInvocationPage } from "@/components/pages/SchedulerInvocation";
import { QueuedOperationsCursorSchema } from "@/components/QueuedOperationsTable/utils";
import { generatePageTitle } from "@/utils/generatePageTitle";
import {
  InvocationNameSchema,
  type InvocationNameSearch,
} from "@/utils/invocationName";

export enum InvocationChildrenFilter {
  QUEUED = "queued",
  ACTIVE = "active",
  ALL = "all",
}

const InvocationSearchSchema = z.object({
  invocationName: InvocationNameSchema.optional().catch(undefined),
  childrenFilter: z
    .enum(InvocationChildrenFilter)
    .catch(InvocationChildrenFilter.ACTIVE),
  cursor: QueuedOperationsCursorSchema.optional(),
});

export type InvocationSearchParams = Omit<
  z.infer<typeof InvocationSearchSchema>,
  "invocationName"
> & { invocationName: InvocationNameSearch };

export const Route = createFileRoute("/scheduler/invocation")({
  component: RouteComponent,
  validateSearch: (search) => InvocationSearchSchema.parse(search),
  beforeLoad: ({ search }) => {
    if (!search.invocationName) {
      throw redirect({ to: "/scheduler" });
    }
  },
  head: (_ctx) => ({
    meta: [
      {
        title: generatePageTitle([
          "Invocation",
          _ctx.match.search.invocationName?.sizeClassQueueName.platformQueueName
            .instanceNamePrefix,
        ]),
      },
    ],
  }),
});

function RouteComponent() {
  const { invocationName, childrenFilter, cursor } = Route.useSearch();

  // beforeLoad redirects without a queue.
  if (!invocationName) {
    return null;
  }

  return (
    <SchedulerInvocationPage
      invocationName={invocationName}
      childrenFilter={childrenFilter}
      cursor={cursor}
    />
  );
}
