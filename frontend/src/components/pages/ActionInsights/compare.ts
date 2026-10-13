import { DigestFunction_Value } from "@/lib/grpc-client/build/bazel/remote/execution/v2/remote_execution";
import { BrowserPageType } from "@/types/BrowserPageType";
import { digestFunctionValueFromString } from "@/utils/digestFunctionUtils";
import { generateBrowserSplat } from "@/utils/urlGenerator";

interface Digest {
  hash: string;
  sizeBytes: number;
}

// Browser route params comparing the previous action (left) with the new one.
// Digests of size 0 are computed by the portal for spawns without an action
// and cannot be opened.
export const compareActionLink = (entry: {
  instanceName: string;
  digestFunction: string;
  digest: Digest;
  previousDigest?: Digest | null;
}) => {
  const digestFunction = digestFunctionValueFromString(entry.digestFunction);
  if (
    !entry.previousDigest ||
    entry.previousDigest.hash === entry.digest.hash ||
    entry.previousDigest.sizeBytes === 0 ||
    entry.digest.sizeBytes === 0 ||
    digestFunction === DigestFunction_Value.UNKNOWN ||
    digestFunction === DigestFunction_Value.UNRECOGNIZED
  ) {
    return undefined;
  }
  const toDigest = (digest: Digest) => ({
    hash: digest.hash,
    sizeBytes: digest.sizeBytes.toString(),
  });
  return {
    params: {
      _splat: generateBrowserSplat(
        entry.instanceName,
        digestFunction,
        toDigest(entry.previousDigest),
        BrowserPageType.Action,
      ),
    },
    search: {
      comparedAction: {
        instanceName: entry.instanceName,
        digestFunction,
        digest: toDigest(entry.digest),
      },
    },
  };
};
