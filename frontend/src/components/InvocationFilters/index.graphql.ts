import { gql } from "@/graphql/__generated__";

export const INVOCATION_FILTER_OPTIONS = gql(/* GraphQL */ `
  query InvocationFilterOptions($where: BazelInvocationWhereInput) {
    invocationFilterOptions(where: $where) {
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
