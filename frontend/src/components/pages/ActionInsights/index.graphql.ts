import { gql } from "@/graphql/__generated__";

export const ACTION_INSIGHTS = gql(/* GraphQL */ `
  query ActionInsights(
    $where: BazelInvocationWhereInput
    $invocationLimit: Int
    $rowLimit: Int
  ) {
    actionInsights(
      where: $where
      invocationLimit: $invocationLimit
      rowLimit: $rowLimit
    ) {
      invocationsMatched
      invocationsAnalyzed
      invocationsPending
      invocationsFailed
      invocationsWithoutLog
      refsAnalyzed
      spawnsAnalyzed
      executedSpawns
      executedTimeMs
      changesTotal
      changesExplained
      impact {
        keyId
        targetLabel
        mnemonic
        primaryOutput
        executions
        invocations
        totalMs
        execWallMs
        queueMs
        p50Ms
        p95Ms
        maxMs
        share
      }
      churn {
        keyId
        targetLabel
        mnemonic
        primaryOutput
        changes
        newVersions
        direct
        dependency
        both
        unknown
        executedAfterChange
        timeMs
        topCauses {
          name
          kind
          count
        }
      }
      causes {
        name
        kind
        changes
        newVersions
        actionsAffected
        timeMs
      }
    }
  }
`);

export const ACTION_HISTORY = gql(/* GraphQL */ `
  query ActionHistory(
    $keyId: ID!
    $where: BazelInvocationWhereInput
    $invocationLimit: Int
  ) {
    actionHistory(
      keyId: $keyId
      where: $where
      invocationLimit: $invocationLimit
    ) {
      invocationId
      startedAt
      instanceName
      digestFunction
      digest {
        hash
        sizeBytes
      }
      previousDigest {
        hash
        sizeBytes
      }
      changed
      newVersion
      cause
      changedGroups
      executions
      executedMs
      execution {
        runner
        exitCode
        totalMs
        queueMs
        execWallMs
        fetchMs
        uploadMs
        inputBytes
        inputFiles
        memoryPeakBytes
      }
    }
  }
`);

export const ACTION_INSIGHTS_FILTER_OPTIONS = gql(/* GraphQL */ `
  query ActionInsightsFilterOptions($where: BazelInvocationWhereInput) {
    actionInsightsFilterOptions(where: $where) {
      repos {
        value
        invocations
      }
      refs {
        repo
        ref
        invocations
      }
      users {
        value
        invocations
      }
    }
  }
`);
