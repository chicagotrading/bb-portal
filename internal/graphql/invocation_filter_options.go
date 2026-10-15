package graphql

import (
	"context"
	"sort"

	"github.com/buildbarn/bb-portal/ent/gen/ent"
	"github.com/buildbarn/bb-portal/ent/gen/ent/bazelinvocation"
	"github.com/buildbarn/bb-portal/ent/gen/ent/execlogsummary"
	"github.com/buildbarn/bb-portal/ent/gen/ent/sourcecontrol"
	"github.com/buildbarn/bb-portal/internal/graphql/model"
	"github.com/buildbarn/bb-storage/pkg/util"
)

// Values per filter option list, and refs per repo.
const filterOptionLimit = 200

type filterOptionRow struct {
	Repo     *string `json:"repo"`
	Ref      string  `json:"ref"`
	Username string  `json:"username"`
	Count    int     `json:"count"`
}

func repoOf(row filterOptionRow) string {
	if row.Repo == nil {
		return ""
	}
	return *row.Repo
}

func sortByCount(rows []filterOptionRow, name func(filterOptionRow) string) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return name(rows[i]) < name(rows[j])
	})
}

func (r *Resolver) invocationFilterOptions(ctx context.Context, where *ent.BazelInvocationWhereInput) (*model.InvocationFilterOptions, error) {
	q, err := where.Filter(r.db.Ent().BazelInvocation.Query())
	if err != nil {
		return nil, util.StatusWrap(err, "Invalid invocation filter")
	}
	return filterOptions(ctx, q)
}

func (r *Resolver) actionInsightsFilterOptions(ctx context.Context, where *ent.BazelInvocationWhereInput) (*model.InvocationFilterOptions, error) {
	q, err := where.Filter(r.db.Ent().BazelInvocation.Query())
	if err != nil {
		return nil, util.StatusWrap(err, "Invalid invocation filter")
	}
	return filterOptions(ctx, q.Where(bazelinvocation.HasExecLogSummaryWith(execlogsummary.StatusEQ(execlogsummary.StatusDONE))))
}

// filterOptions groups the invocations of q by source control repo and
// ref, and by username, in the database.
func filterOptions(ctx context.Context, q *ent.BazelInvocationQuery) (*model.InvocationFilterOptions, error) {
	out := &model.InvocationFilterOptions{
		Repos: []*model.InvocationFilterOption{},
		Refs:  []*model.InvocationRefOption{},
		Users: []*model.InvocationFilterOption{},
	}

	// SourceControl has no policy; traversing from the invocations applies theirs.
	var refs []filterOptionRow
	if err := q.Clone().QuerySourceControl().
		Where(sourcecontrol.RefNotNil(), sourcecontrol.RefNEQ("")).
		GroupBy(sourcecontrol.FieldRepo, sourcecontrol.FieldRef).
		Aggregate(ent.Count()).
		Scan(ctx, &refs); err != nil {
		return nil, util.StatusWrap(err, "Failed to group source control refs")
	}
	sortByCount(refs, func(row filterOptionRow) string { return row.Ref })
	perRepo := map[string]int{}
	for _, row := range refs {
		if perRepo[repoOf(row)] >= filterOptionLimit {
			continue
		}
		perRepo[repoOf(row)]++
		out.Refs = append(out.Refs, &model.InvocationRefOption{Repo: row.Repo, Ref: row.Ref, Invocations: row.Count})
	}

	var repos []filterOptionRow
	if err := q.Clone().QuerySourceControl().
		Where(sourcecontrol.RepoNotNil(), sourcecontrol.RepoNEQ("")).
		GroupBy(sourcecontrol.FieldRepo).
		Aggregate(ent.Count()).
		Scan(ctx, &repos); err != nil {
		return nil, util.StatusWrap(err, "Failed to group source control repos")
	}
	sortByCount(repos, repoOf)
	for _, row := range repos[:min(filterOptionLimit, len(repos))] {
		out.Repos = append(out.Repos, &model.InvocationFilterOption{Value: repoOf(row), Invocations: row.Count})
	}

	var users []filterOptionRow
	if err := q.Clone().
		Where(bazelinvocation.UsernameNotNil(), bazelinvocation.UsernameNEQ("")).
		GroupBy(bazelinvocation.FieldUsername).
		Aggregate(ent.Count()).
		Scan(ctx, &users); err != nil {
		return nil, util.StatusWrap(err, "Failed to group usernames")
	}
	sortByCount(users, func(row filterOptionRow) string { return row.Username })
	for _, row := range users[:min(filterOptionLimit, len(users))] {
		out.Users = append(out.Users, &model.InvocationFilterOption{Value: row.Username, Invocations: row.Count})
	}
	return out, nil
}
