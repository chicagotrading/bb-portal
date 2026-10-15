package graphql

import (
	"context"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-portal/ent/gen/ent"
	"github.com/buildbarn/bb-portal/ent/gen/ent/actionexecution"
	"github.com/buildbarn/bb-portal/ent/gen/ent/actionkey"
	"github.com/buildbarn/bb-portal/ent/gen/ent/actionversion"
	"github.com/buildbarn/bb-portal/ent/gen/ent/bazelinvocation"
	"github.com/buildbarn/bb-portal/ent/gen/ent/execlogsummary"
	"github.com/buildbarn/bb-portal/ent/gen/ent/predicate"
	"github.com/buildbarn/bb-portal/ent/gen/ent/sourcecontrol"
	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/internal/database/sqlc"
	"github.com/buildbarn/bb-portal/internal/graphql/helpers"
	"github.com/buildbarn/bb-portal/internal/graphql/model"
	"github.com/buildbarn/bb-portal/pkg/execlog"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultInsightsInvocations = 100
	maxInsightsInvocations     = 200
	defaultInsightsRows        = 100
	maxInsightsRows            = 1000
	// Extra changes classified for the causes list.
	causeChangeBudget = 20000
	// Changes whose two versions' groups are decoded together.
	explainBatchChanges = 2000
	// IDs per IN (...) query.
	idChunk = 5000
	// Summaries whose spawn index is loaded per query.
	indexChunk     = 100
	actionKeyIDTag = "ActionKey"
	// SpawnIndexCacheBytes bounds the spawn indexes kept across requests,
	// about 140 kB per 39k spawn invocation.
	SpawnIndexCacheBytes = 256 << 20
)

func clampLimit(v *int, def, maxValue int) int {
	if v == nil || *v <= 0 {
		return def
	}
	return min(*v, maxValue)
}

type analyzedInvocation struct {
	id        int64
	uuid      uuid.UUID
	startedAt time.Time
	// summary lacks the spawn index, which is in index.
	summary *ent.ExecLogSummary
	index   actioninsights.SpawnIndex
}

// analyzedInvocations returns the newest matches with an ingested log,
// oldest first, and the query of all matches.
func (r *Resolver) analyzedInvocations(ctx context.Context, where *ent.BazelInvocationWhereInput, limit int) (*ent.BazelInvocationQuery, []analyzedInvocation, error) {
	q, err := where.Filter(r.db.Ent().BazelInvocation.Query())
	if err != nil {
		return nil, nil, util.StatusWrap(err, "Invalid invocation filter")
	}
	invocations, err := q.Clone().
		Where(bazelinvocation.HasExecLogSummaryWith(execlogsummary.StatusEQ(execlogsummary.StatusDONE))).
		Order(bazelinvocation.ByStartedAt(sql.OrderDesc()), bazelinvocation.ByID(sql.OrderDesc())).
		Limit(limit).
		Select(bazelinvocation.FieldID, bazelinvocation.FieldInvocationID, bazelinvocation.FieldStartedAt).
		All(ctx)
	if err != nil {
		return nil, nil, util.StatusWrap(err, "Failed to select invocations")
	}
	ids := make([]int64, len(invocations))
	for i, inv := range invocations {
		ids[i] = inv.ID
	}
	summaries, err := r.db.Ent().ExecLogSummary.Query().
		Where(
			execlogsummary.BazelInvocationIDIn(ids...),
			execlogsummary.StatusEQ(execlogsummary.StatusDONE),
		).
		Select(
			execlogsummary.FieldBazelInvocationID,
			execlogsummary.FieldRev2InstanceName,
			execlogsummary.FieldDigestFunction,
			execlogsummary.FieldSpawnCount,
			execlogsummary.FieldExecutedCount,
		).
		All(ctx)
	if err != nil {
		return nil, nil, util.StatusWrap(err, "Failed to load execution log summaries")
	}
	byInvocation := make(map[int64]*ent.ExecLogSummary, len(summaries))
	summaryIDs := make([]int64, len(summaries))
	for i, s := range summaries {
		byInvocation[s.BazelInvocationID] = s
		summaryIDs[i] = s.ID
	}
	indexes, err := r.loadSpawnIndexes(ctx, summaryIDs)
	if err != nil {
		return nil, nil, err
	}
	var out []analyzedInvocation
	for _, inv := range invocations {
		s, ok := byInvocation[inv.ID]
		if !ok {
			continue
		}
		out = append(out, analyzedInvocation{id: inv.ID, uuid: inv.InvocationID, startedAt: inv.StartedAt, summary: s, index: indexes[s.ID]})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].startedAt.Equal(out[j].startedAt) {
			return out[i].startedAt.Before(out[j].startedAt)
		}
		return out[i].id < out[j].id
	})
	return q, out, nil
}

// loadSpawnIndexes returns spawn indexes by summary ID, from the cache
// where possible.
func (r *Resolver) loadSpawnIndexes(ctx context.Context, summaryIDs []int64) (map[int64]actioninsights.SpawnIndex, error) {
	out := make(map[int64]actioninsights.SpawnIndex, len(summaryIDs))
	var missing []int64
	for _, id := range summaryIDs {
		if r.spawnIndexes != nil {
			if index, ok := r.spawnIndexes.Get(id); ok {
				out[id] = index
				continue
			}
		}
		missing = append(missing, id)
	}
	for i := 0; i < len(missing); i += indexChunk {
		rows, err := r.db.Ent().ExecLogSummary.Query().
			Where(execlogsummary.IDIn(missing[i:min(i+indexChunk, len(missing))]...)).
			Select(execlogsummary.FieldSpawnIndex).
			All(ctx)
		if err != nil {
			return nil, util.StatusWrap(err, "Failed to load spawn indexes")
		}
		for _, row := range rows {
			out[row.ID] = row.SpawnIndex
			if r.spawnIndexes != nil {
				r.spawnIndexes.Put(row.ID, row.SpawnIndex)
			}
		}
	}
	return out, nil
}

// coverage counts the matched invocations by the state of their log, since
// the oldest analyzed one.
func (r *Resolver) coverage(ctx context.Context, matched *ent.BazelInvocationQuery, invs []analyzedInvocation, out *model.ActionInsights) error {
	span := matched.Clone()
	if len(invs) > 0 && !invs[0].startedAt.IsZero() {
		span = span.Where(bazelinvocation.StartedAtGTE(invs[0].startedAt))
	}
	for _, c := range []struct {
		count *int
		preds []predicate.BazelInvocation
	}{
		{&out.InvocationsMatched, nil},
		{&out.InvocationsFailed, []predicate.BazelInvocation{
			bazelinvocation.HasExecLogSummaryWith(execlogsummary.StatusEQ(execlogsummary.StatusFAILED)),
		}},
		{&out.InvocationsPending, []predicate.BazelInvocation{
			bazelinvocation.ExecLogIDNotNil(),
			bazelinvocation.Not(bazelinvocation.HasExecLogSummaryWith(execlogsummary.StatusIn(execlogsummary.StatusDONE, execlogsummary.StatusFAILED))),
		}},
		{&out.InvocationsWithoutLog, []predicate.BazelInvocation{
			bazelinvocation.ExecLogIDIsNil(),
			bazelinvocation.Not(bazelinvocation.HasExecLogSummary()),
		}},
	} {
		n, err := span.Clone().Where(c.preds...).Count(ctx)
		if err != nil {
			return util.StatusWrap(err, "Failed to count invocations")
		}
		*c.count = n
	}

	ids := make([]int64, len(invs))
	for i, inv := range invs {
		ids[i] = inv.id
	}
	refs, err := r.db.Ent().SourceControl.Query().
		Where(sourcecontrol.HasBazelInvocationWith(bazelinvocation.IDIn(ids...)), sourcecontrol.RefNEQ("")).
		Select(sourcecontrol.FieldRepo, sourcecontrol.FieldRef).
		All(ctx)
	if err != nil {
		return util.StatusWrap(err, "Failed to load source control refs")
	}
	// The same ref in two repos is two branches.
	distinct := map[[2]string]struct{}{}
	for _, sc := range refs {
		distinct[[2]string{sc.Repo, sc.Ref}] = struct{}{}
	}
	out.RefsAnalyzed = len(distinct)
	return nil
}

func inputsOf(invs []analyzedInvocation) []actioninsights.Invocation {
	out := make([]actioninsights.Invocation, len(invs))
	for i, inv := range invs {
		out[i] = actioninsights.Invocation{ID: inv.id, Index: inv.index}
	}
	return out
}

func chunks(ids []int64, f func([]int64) error) error {
	for i := 0; i < len(ids); i += idChunk {
		if err := f(ids[i:min(i+idChunk, len(ids))]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) loadKeys(ctx context.Context, ids []int64) (map[int64]*ent.ActionKey, error) {
	out := make(map[int64]*ent.ActionKey, len(ids))
	err := chunks(ids, func(c []int64) error {
		keys, err := r.db.Ent().ActionKey.Query().Where(actionkey.IDIn(c...)).All(ctx)
		for _, k := range keys {
			out[k.ID] = k
		}
		return err
	})
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to load action keys")
	}
	return out, nil
}

func (r *Resolver) loadVersions(ctx context.Context, ids []int64) (map[int64]*ent.ActionVersion, error) {
	out := make(map[int64]*ent.ActionVersion, len(ids))
	err := chunks(ids, func(c []int64) error {
		versions, err := r.db.Ent().ActionVersion.Query().Where(actionversion.IDIn(c...)).All(ctx)
		for _, v := range versions {
			out[v.ID] = v
		}
		return err
	})
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to load action versions")
	}
	return out, nil
}

func newExplainer(versions map[int64]*ent.ActionVersion) *actioninsights.Explainer {
	return actioninsights.NewExplainer(func(id int64) (map[string]string, bool) {
		v, ok := versions[id]
		if !ok || v.Groups == nil {
			return nil, false
		}
		return v.Groups, true
	})
}

func groupKind(k execlog.GroupKind) model.ActionInputGroupKind {
	switch k {
	case execlog.GroupCommand:
		return model.ActionInputGroupKindCommand
	case execlog.GroupSource:
		return model.ActionInputGroupKindSource
	case execlog.GroupExternal:
		return model.ActionInputGroupKindExternal
	}
	return model.ActionInputGroupKindGenerated
}

func changeCause(c actioninsights.Cause) model.ActionChangeCause {
	switch c {
	case actioninsights.CauseDirect:
		return model.ActionChangeCauseDirect
	case actioninsights.CauseDependency:
		return model.ActionChangeCauseDependency
	case actioninsights.CauseBoth:
		return model.ActionChangeCauseBoth
	}
	return model.ActionChangeCauseUnknown
}

func actionKeyGraphQLID(id int64) string {
	return helpers.GraphQLIDFromTypeAndID(actionKeyIDTag, id)
}

// insightsSlot runs one insights computation at a time: each can take
// hundreds of MB, and concurrent page loads OOM-killed the portal.
var insightsSlot = make(chan struct{}, 1)

func acquireInsightsSlot(ctx context.Context) (func(), error) {
	select {
	case insightsSlot <- struct{}{}:
		return func() { <-insightsSlot }, nil
	case <-ctx.Done():
		return nil, util.StatusWrap(ctx.Err(), "Waiting for another action insights request")
	}
}

func (r *Resolver) actionInsights(ctx context.Context, where *ent.BazelInvocationWhereInput, invocationLimit, rowLimit *int) (*model.ActionInsights, error) {
	release, err := acquireInsightsSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	rows := clampLimit(rowLimit, defaultInsightsRows, maxInsightsRows)
	matched, invs, err := r.analyzedInvocations(ctx, where, clampLimit(invocationLimit, defaultInsightsInvocations, maxInsightsInvocations))
	if err != nil {
		return nil, err
	}
	out := &model.ActionInsights{
		InvocationsAnalyzed: len(invs),
		Impact:              []*model.ActionImpact{},
		Churn:               []*model.ActionChurn{},
		Causes:              []*model.ActionChurnCause{},
	}
	if err := r.coverage(ctx, matched, invs, out); err != nil {
		return nil, err
	}
	if len(invs) == 0 {
		return out, nil
	}
	ids := make([]int64, len(invs))
	for i, inv := range invs {
		ids[i] = inv.id
		out.SpawnsAnalyzed += int(inv.summary.SpawnCount)
		out.ExecutedSpawns += int(inv.summary.ExecutedCount)
	}

	executedMs, err := r.db.Sqlc().SumActionExecutionTime(ctx, ids)
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to sum executed time")
	}
	out.ExecutedTimeMs = int(executedMs)
	impact, err := r.db.Sqlc().AggregateActionExecutions(ctx, sqlc.AggregateActionExecutionsParams{
		BazelInvocationIds: ids,
		RowLimit:           int64(rows),
	})
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to aggregate executions")
	}

	changes, err := actioninsights.FindChanges(inputsOf(invs))
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to read spawn indexes")
	}
	out.ChangesTotal = len(changes)
	byKey := map[int64][]actioninsights.Change{}
	newVersions := map[int64]int{}
	for _, c := range changes {
		byKey[c.KeyID] = append(byKey[c.KeyID], c)
		if c.NewVersion {
			newVersions[c.KeyID]++
		}
	}
	changedKeys := make([]int64, 0, len(byKey))
	for k := range byKey {
		changedKeys = append(changedKeys, k)
	}
	execMs := map[actioninsights.ExecKey]int64{}
	if err := chunks(changedKeys, func(c []int64) error {
		times, err := r.db.Sqlc().GetActionExecutionTimes(ctx, sqlc.GetActionExecutionTimesParams{
			BazelInvocationIds: ids,
			ActionKeyIds:       c,
		})
		for _, t := range times {
			execMs[actioninsights.ExecKey{Invocation: t.BazelInvocationID, KeyID: t.ActionKeyID}] = t.TotalMs
		}
		return err
	}); err != nil {
		return nil, util.StatusWrap(err, "Failed to load execution times")
	}

	// Classify the top churn rows, then further keys within the budget.
	keyTime := make(map[int64]int64, len(byKey))
	for k, cs := range byKey {
		for _, c := range cs {
			keyTime[k] += execMs[actioninsights.ExecKey{Invocation: c.Invocation, KeyID: k}]
		}
	}
	// The same order as actioninsights.Aggregate.
	sort.Slice(changedKeys, func(i, j int) bool {
		a, b := changedKeys[i], changedKeys[j]
		if newVersions[a] != newVersions[b] {
			return newVersions[a] > newVersions[b]
		}
		if len(byKey[a]) != len(byKey[b]) {
			return len(byKey[a]) > len(byKey[b])
		}
		if keyTime[a] != keyTime[b] {
			return keyTime[a] > keyTime[b]
		}
		return a < b
	})
	var selected []int64
	explainedCount := 0
	for i, k := range changedKeys {
		if i >= rows && explainedCount+len(byKey[k]) > causeChangeBudget {
			break
		}
		selected = append(selected, k)
		explainedCount += len(byKey[k])
	}

	keyIDs := append([]int64(nil), selected...)
	for _, row := range impact {
		keyIDs = append(keyIDs, row.ActionKeyID)
	}
	keys, err := r.loadKeys(ctx, keyIDs)
	if err != nil {
		return nil, err
	}

	// Decoded groups take ~10 KB per version, so explain a batch of keys
	// at a time instead of holding every version at once.
	explained := make([]actioninsights.Explained, 0, explainedCount)
	for start := 0; start < len(selected); {
		end, batchChanges := start, 0
		for end < len(selected) && (end == start || batchChanges+len(byKey[selected[end]]) <= explainBatchChanges) {
			batchChanges += len(byKey[selected[end]])
			end++
		}
		versionIDs := make([]int64, 0, 2*batchChanges)
		for _, k := range selected[start:end] {
			for _, c := range byKey[k] {
				versionIDs = append(versionIDs, c.PrevVersion, c.VersionID)
			}
		}
		versions, err := r.loadVersions(ctx, versionIDs)
		if err != nil {
			return nil, err
		}
		explainer := newExplainer(versions)
		for _, k := range selected[start:end] {
			key, ok := keys[k]
			if !ok {
				continue
			}
			for _, c := range byKey[k] {
				cause, changed := explainer.Explain(c.PrevVersion, c.VersionID, key.TargetLabel)
				explained = append(explained, actioninsights.Explained{Change: c, Cause: cause, Changed: changed})
			}
		}
		start = end
	}
	out.ChangesExplained = len(explained)
	churn, causes := actioninsights.Aggregate(explained, execMs)

	for _, row := range impact {
		key, ok := keys[row.ActionKeyID]
		if !ok {
			continue
		}
		share := 0.0
		if executedMs > 0 {
			share = float64(row.TotalMs) / float64(executedMs)
		}
		out.Impact = append(out.Impact, &model.ActionImpact{
			KeyID:         actionKeyGraphQLID(key.ID),
			TargetLabel:   key.TargetLabel,
			Mnemonic:      key.Mnemonic,
			PrimaryOutput: key.PrimaryOutput,
			Executions:    int(row.Executions),
			Invocations:   int(row.Invocations),
			TotalMs:       int(row.TotalMs),
			ExecWallMs:    int(row.ExecWallMs),
			QueueMs:       int(row.QueueMs),
			P50Ms:         int(row.P50Ms),
			P95Ms:         int(row.P95Ms),
			MaxMs:         int(row.MaxMs),
			Share:         share,
		})
	}
	for _, row := range churn[:min(rows, len(churn))] {
		key := keys[row.KeyID]
		top := make([]*model.ActionInputGroupCount, 0, len(row.TopCauses))
		for _, c := range row.TopCauses {
			top = append(top, &model.ActionInputGroupCount{Name: c.Name, Kind: groupKind(execlog.KindOfGroup(c.Name)), Count: c.Count})
		}
		out.Churn = append(out.Churn, &model.ActionChurn{
			KeyID:               actionKeyGraphQLID(key.ID),
			TargetLabel:         key.TargetLabel,
			Mnemonic:            key.Mnemonic,
			PrimaryOutput:       key.PrimaryOutput,
			Changes:             row.Changes,
			NewVersions:         row.NewVersions,
			Direct:              row.Direct,
			Dependency:          row.Dependency,
			Both:                row.Both,
			Unknown:             row.Unknown,
			ExecutedAfterChange: row.ExecutedAfterChange,
			TimeMs:              int(row.TimeMs),
			TopCauses:           top,
		})
	}
	for _, c := range causes[:min(rows, len(causes))] {
		out.Causes = append(out.Causes, &model.ActionChurnCause{
			Name:            c.Name,
			Kind:            groupKind(c.Kind),
			Changes:         c.Changes,
			NewVersions:     c.NewVersions,
			ActionsAffected: c.ActionsAffected,
			TimeMs:          int(c.TimeMs),
		})
	}
	return out, nil
}

func digestFunctionName(value int16) string {
	name, ok := remoteexecution.DigestFunction_Value_name[int32(value)]
	if !ok {
		name = remoteexecution.DigestFunction_UNKNOWN.String()
	}
	return strings.ToLower(name)
}

func actionDigest(v *ent.ActionVersion) *model.ActionDigest {
	if v == nil {
		return &model.ActionDigest{}
	}
	return &model.ActionDigest{Hash: hex.EncodeToString(v.DigestHash), SizeBytes: int(v.DigestSizeBytes)}
}

func (r *Resolver) actionHistory(ctx context.Context, keyID string, where *ent.BazelInvocationWhereInput, invocationLimit *int) ([]*model.ActionHistoryEntry, error) {
	typ, id, err := helpers.GraphQLTypeAndIntIDFromID(keyID)
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to parse keyId")
	}
	if typ != actionKeyIDTag {
		return nil, status.Errorf(codes.InvalidArgument, "keyId is a %s ID, not an action key", typ)
	}
	key, err := r.db.Ent().ActionKey.Get(ctx, id)
	if ent.IsNotFound(err) {
		return []*model.ActionHistoryEntry{}, nil
	}
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to load action key")
	}
	_, invs, err := r.analyzedInvocations(ctx, where, clampLimit(invocationLimit, defaultInsightsInvocations, maxInsightsInvocations))
	if err != nil {
		return nil, err
	}
	history, err := actioninsights.History(inputsOf(invs), id)
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to read spawn indexes")
	}
	byID := make(map[int64]*analyzedInvocation, len(invs))
	for i := range invs {
		byID[invs[i].id] = &invs[i]
	}
	versionIDs := make([]int64, 0, len(history))
	invocationIDs := make([]int64, 0, len(history))
	for _, h := range history {
		versionIDs = append(versionIDs, h.VersionID)
		invocationIDs = append(invocationIDs, h.Invocation)
	}
	versions, err := r.loadVersions(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	// The longest execution per invocation, and the count and sum of all.
	executions := map[int64]*ent.ActionExecution{}
	executionCount := map[int64]int{}
	executedMs := map[int64]int64{}
	if err := chunks(invocationIDs, func(c []int64) error {
		rows, err := r.db.Ent().ActionExecution.Query().
			Where(actionexecution.ActionKeyID(id), actionexecution.BazelInvocationIDIn(c...)).
			All(ctx)
		for _, e := range rows {
			executionCount[e.BazelInvocationID]++
			executedMs[e.BazelInvocationID] += e.TotalMs
			if prev, ok := executions[e.BazelInvocationID]; !ok || e.TotalMs > prev.TotalMs {
				executions[e.BazelInvocationID] = e
			}
		}
		return err
	}); err != nil {
		return nil, util.StatusWrap(err, "Failed to load executions")
	}

	explainer := newExplainer(versions)
	out := make([]*model.ActionHistoryEntry, 0, len(history))
	for _, h := range history {
		inv := byID[h.Invocation]
		e := &model.ActionHistoryEntry{
			InvocationID:   inv.uuid,
			InstanceName:   inv.summary.Rev2InstanceName,
			DigestFunction: digestFunctionName(inv.summary.DigestFunction),
			Digest:         actionDigest(versions[h.VersionID]),
			Changed:        h.Changed,
			NewVersion:     h.NewVersion,
			ChangedGroups:  []string{},
			Executions:     executionCount[h.Invocation],
			ExecutedMs:     int(executedMs[h.Invocation]),
		}
		if !inv.startedAt.IsZero() {
			startedAt := inv.startedAt
			e.StartedAt = &startedAt
		}
		if h.PrevVersion != 0 {
			e.PreviousDigest = actionDigest(versions[h.PrevVersion])
		}
		if h.Changed {
			cause, changed := explainer.Explain(h.PrevVersion, h.VersionID, key.TargetLabel)
			c := changeCause(cause)
			e.Cause = &c
			if changed != nil {
				e.ChangedGroups = changed
			}
		}
		if x, ok := executions[h.Invocation]; ok {
			m := &model.ActionExecutionMetrics{
				Runner:          x.Runner,
				ExitCode:        int(x.ExitCode),
				TotalMs:         int(x.TotalMs),
				QueueMs:         int(x.QueueMs),
				ExecWallMs:      int(x.ExecWallMs),
				FetchMs:         int(x.FetchMs),
				UploadMs:        int(x.UploadMs),
				InputBytes:      int(x.InputBytes),
				InputFiles:      int(x.InputFiles),
				MemoryPeakBytes: int(x.MemoryPeakBytes),
			}
			if x.StartOffsetMs != nil {
				offset := int(*x.StartOffsetMs)
				m.StartOffsetMs = &offset
			}
			e.Execution = m
		}
		out = append(out, e)
	}
	return out, nil
}
