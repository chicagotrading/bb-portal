import { z } from "zod";
import type { InvocationName } from "@/lib/grpc-client/buildbarn/buildqueuestate/buildqueuestate";
import type { Any } from "@/lib/grpc-client/google/protobuf/any";
import { protobufToObjectWithTypeField } from "@/utils/protobufToObject";
import {
  SizeClassQueueNameSchema,
  type SizeClassQueueNameSearch,
} from "@/utils/sizeClassQueueName";

export const InvocationIdSchema = z.object({
  typeUrl: z.string(),
  value: z.string(),
});

export const InvocationNameSchema = z.object({
  sizeClassQueueName: SizeClassQueueNameSchema,
  ids: z.array(InvocationIdSchema).default([]),
});

export type InvocationIdSearch = z.infer<typeof InvocationIdSchema>;
export type InvocationNameSearch = z.infer<typeof InvocationNameSchema>;

export const anyToInvocationIdSearch = (id: Any): InvocationIdSearch => ({
  typeUrl: id.typeUrl,
  value: btoa(Array.from(id.value, (b) => String.fromCharCode(b)).join("")),
});

export const invocationIdSearchToAny = (id: InvocationIdSearch): Any => ({
  typeUrl: id.typeUrl,
  value: Uint8Array.from(atob(id.value), (c) => c.charCodeAt(0)),
});

export const invocationNameFromSearch = (
  search: InvocationNameSearch,
): InvocationName => ({
  sizeClassQueueName: search.sizeClassQueueName,
  ids: search.ids.map(invocationIdSearchToAny),
});

export const rootInvocationNameSearch = (
  sizeClassQueueName: SizeClassQueueNameSearch,
): InvocationNameSearch => ({ sizeClassQueueName, ids: [] });

export const childInvocationNameSearch = (
  parent: InvocationNameSearch,
  childId: Any,
): InvocationNameSearch => ({
  sizeClassQueueName: parent.sizeClassQueueName,
  ids: [...parent.ids, anyToInvocationIdSearch(childId)],
});

export const invocationIdToString = (id: Any): string => {
  const object = protobufToObjectWithTypeField(id, false);
  if (
    typeof object !== "object" ||
    object === null ||
    Object.keys(object).length === 0
  ) {
    return JSON.stringify({ "@type": id.typeUrl });
  }
  return JSON.stringify(object);
};
