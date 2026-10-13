package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gqlgen "github.com/99designs/gqlgen/graphql"
	"github.com/buildbarn/bb-portal/ent/gen/ent"
	"github.com/buildbarn/bb-portal/ent/gen/ent/execlogsummary"
	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/internal/database/buildeventrecorder"
	"github.com/buildbarn/bb-portal/internal/database/dbauthservice"
	"github.com/buildbarn/bb-portal/internal/database/embedded"
	"github.com/buildbarn/bb-portal/internal/graphql"
	"github.com/buildbarn/bb-portal/internal/graphql/helpers"
	"github.com/buildbarn/bb-portal/pkg/invocation/files"
	"github.com/buildbarn/bb-portal/test/testutils"
	"github.com/buildbarn/bb-storage/pkg/auth"
	"github.com/buildbarn/bb-storage/pkg/clock"
	"github.com/buildbarn/bb-storage/pkg/jmespath"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"

	_ "github.com/buildbarn/bb-portal/ent/gen/ent/runtime"
)

var dbProvider *embedded.DatabaseProvider

func TestMain(m *testing.M) {
	var err error
	dbProvider, err = embedded.NewDatabaseProvider(os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not start embedded DB: %v\n", err)
		os.Exit(1)
	}
	defer dbProvider.Cleanup()
	m.Run()
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func query(t *testing.T, url, q string, vars map[string]any) json.RawMessage {
	body, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	var r gqlResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&r))
	require.Empty(t, r.Errors)
	return r.Data
}

const insightsQuery = `query ($where: BazelInvocationWhereInput, $limit: Int) {
  actionInsights(where: $where, invocationLimit: $limit) {
    invocationsMatched invocationsAnalyzed invocationsPending invocationsFailed invocationsWithoutLog refsAnalyzed
    spawnsAnalyzed executedSpawns executedTimeMs changesTotal changesExplained
    impact { keyId targetLabel mnemonic primaryOutput executions invocations totalMs execWallMs queueMs p50Ms p95Ms maxMs share }
    churn { keyId targetLabel changes newVersions direct dependency both unknown executedAfterChange timeMs topCauses { name kind count } }
    causes { name kind changes newVersions actionsAffected timeMs }
  }
}`

const historyQuery = `query ($key: ID!) {
  actionHistory(keyId: $key) {
    invocationId instanceName digestFunction changed newVersion cause changedGroups
    digest { hash sizeBytes }
    previousDigest { hash }
    execution { runner totalMs startOffsetMs }
    executions executedMs
  }
}`

type insights struct {
	InvocationsMatched    int
	InvocationsAnalyzed   int
	InvocationsPending    int
	InvocationsFailed     int
	InvocationsWithoutLog int
	RefsAnalyzed          int
	SpawnsAnalyzed        int
	ExecutedSpawns        int
	ExecutedTimeMs        int
	ChangesTotal          int
	ChangesExplained      int
	Impact                []map[string]any
	Churn                 []map[string]any
	Causes                []map[string]any
}

func TestActionInsights(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	srv := graphql.NewGraphqlHandler(db, noop.NewTracerProvider())
	srv.AroundOperations(func(ctx context.Context, next gqlgen.OperationHandler) gqlgen.ResponseHandler {
		return next(dbauthservice.NewContextWithDbAuthServiceBypass(ctx))
	})
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	instance := testutils.CreateInstanceName(ctx, t, client, "main")
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	invocation := func(hour int) *ent.BazelInvocation {
		return testutils.StartCreateInvocation(client, instance).SetStartedAt(base.Add(time.Duration(hour) * time.Hour)).SaveX(ctx)
	}
	key := func(label, mnemonic, output string) *ent.ActionKey {
		return client.ActionKey.Create().SetInstanceName(instance).SetTargetLabel(label).SetMnemonic(mnemonic).SetPrimaryOutput(output).SaveX(ctx)
	}
	version := func(k *ent.ActionKey, hash byte, groups map[string]string) *ent.ActionVersion {
		return client.ActionVersion.Create().SetActionKey(k).SetDigestHash([]byte{hash}).SetDigestSizeBytes(int64(hash)).
			SetGroups(groups).SetFirstSeenAt(base).SetLastSeenAt(base).SaveX(ctx)
	}
	summary := func(inv *ent.BazelInvocation, spawns, executed int64, pairs ...*ent.ActionVersion) {
		var p []actioninsights.Pair
		for _, v := range pairs {
			p = append(p, actioninsights.Pair{KeyID: v.ActionKeyID, VersionID: v.ID})
		}
		client.ExecLogSummary.Create().SetBazelInvocation(inv).SetStatus(execlogsummary.StatusDONE).SetStartedAt(base).
			SetRev2InstanceName("main").SetDigestFunction(1).SetSpawnCount(spawns).SetExecutedCount(executed).
			SetSpawnIndex(actioninsights.EncodeSpawnIndex(p)).SaveX(ctx)
	}
	execution := func(inv *ent.BazelInvocation, v *ent.ActionVersion, ms int64) {
		client.ActionExecution.Create().SetBazelInvocation(inv).SetActionKeyID(v.ActionKeyID).SetActionVersion(v).
			SetRunner("remote").SetTotalMs(ms).SetExecWallMs(ms / 2).SetQueueMs(10).SetStartOffsetMs(5).SaveX(ctx)
	}
	ref := func(inv *ent.BazelInvocation, ref string) {
		client.SourceControl.Create().SetBazelInvocation(inv).SetRef(ref).SaveX(ctx)
	}

	compile := key("//app:main", "CppCompile", "bazel-out/k8-opt/bin/app/main.o")
	gen := key("//lib:gen", "Genrule", "bazel-out/k8-opt/bin/lib/gen.h")
	test := key("//app:test", "TestRunner", "bazel-out/k8-opt/testlogs/app/test/test.xml")
	c1 := version(compile, 11, map[string]string{"cmd": "a", "src:app/main.cc": "x", "//lib (k8-opt)": "g1"})
	c2 := version(compile, 12, map[string]string{"cmd": "a", "src:app/main.cc": "x", "//lib (k8-opt)": "g2"})
	c3 := version(compile, 13, map[string]string{"cmd": "b", "src:app/main.cc": "y", "//lib (k8-opt)": "g2"})
	g1 := version(gen, 21, map[string]string{"cmd": "c", "//other (sources)": "s1"})
	g2 := version(gen, 22, map[string]string{"cmd": "c", "//other (sources)": "s2"})
	t1 := version(test, 31, map[string]string{"cmd": "d"})

	a, b, c := invocation(1), invocation(2), invocation(3)
	summary(a, 3, 2, c1, g1, t1)
	summary(b, 3, 3, c2, g2, t1)
	summary(c, 2, 1, c3, g2)
	execution(a, c1, 1000)
	execution(a, g1, 500)
	// The compile ran twice in b.
	execution(b, c2, 2000)
	execution(b, c2, 100)
	execution(b, g2, 300)
	execution(c, c3, 1500)
	ref(a, "main")
	ref(b, "feature")
	ref(c, "main")
	// Newer invocations without an analyzed log: none published, failed,
	// and not ingested yet.
	invocation(4)
	failed := invocation(5)
	client.ExecLogSummary.Create().SetBazelInvocation(failed).SetStatus(execlogsummary.StatusFAILED).SetStartedAt(base).SaveX(ctx)
	logFile, err := buildeventrecorder.SaveSingleFile(ctx, db, instance.ID, files.ParsedBepFile{
		Path: "execution_log.binpb.zst", InstanceName: "main", DigestFunction: 1, Hash: make([]byte, 32), SizeBytes: 5,
	})
	require.NoError(t, err)
	testutils.StartCreateInvocation(client, instance).SetStartedAt(base.Add(6 * time.Hour)).SetExecLogID(logFile).SaveX(ctx)

	run := func(vars map[string]any) insights {
		var got struct{ ActionInsights insights }
		require.NoError(t, json.Unmarshal(query(t, server.URL, insightsQuery, vars), &got))
		return got.ActionInsights
	}
	ai := run(nil)
	require.Equal(t, 6, ai.InvocationsMatched)
	require.Equal(t, 3, ai.InvocationsAnalyzed)
	require.Equal(t, 1, ai.InvocationsPending)
	require.Equal(t, 1, ai.InvocationsFailed)
	require.Equal(t, 1, ai.InvocationsWithoutLog)
	require.Equal(t, 2, ai.RefsAnalyzed)
	require.Equal(t, 8, ai.SpawnsAnalyzed)
	require.Equal(t, 6, ai.ExecutedSpawns)
	require.Equal(t, 5400, ai.ExecutedTimeMs)
	require.Equal(t, 3, ai.ChangesTotal)
	require.Equal(t, 3, ai.ChangesExplained)

	compileID := ai.Impact[0]["keyId"].(string)
	require.Equal(t, []map[string]any{
		{
			"keyId": compileID, "targetLabel": "//app:main", "mnemonic": "CppCompile", "primaryOutput": "bazel-out/k8-opt/bin/app/main.o",
			"executions": 4.0, "invocations": 3.0, "totalMs": 4600.0, "execWallMs": 2300.0, "queueMs": 40.0,
			"p50Ms": 1000.0, "p95Ms": 2000.0, "maxMs": 2000.0, "share": 4600.0 / 5400.0,
		},
		{
			"keyId": ai.Impact[1]["keyId"], "targetLabel": "//lib:gen", "mnemonic": "Genrule", "primaryOutput": "bazel-out/k8-opt/bin/lib/gen.h",
			"executions": 2.0, "invocations": 2.0, "totalMs": 800.0, "execWallMs": 400.0, "queueMs": 20.0,
			"p50Ms": 300.0, "p95Ms": 500.0, "maxMs": 500.0, "share": 800.0 / 5400.0,
		},
	}, ai.Impact)

	require.Equal(t, []map[string]any{
		{
			"keyId": compileID, "targetLabel": "//app:main", "changes": 2.0, "newVersions": 2.0, "direct": 1.0, "dependency": 1.0, "both": 0.0, "unknown": 0.0,
			"executedAfterChange": 2.0, "timeMs": 3600.0,
			"topCauses": []any{
				map[string]any{"name": "//lib (k8-opt)", "kind": "GENERATED", "count": 1.0},
				map[string]any{"name": "cmd", "kind": "COMMAND", "count": 1.0},
				map[string]any{"name": "src:app/main.cc", "kind": "SOURCE", "count": 1.0},
			},
		},
		{
			"keyId": ai.Impact[1]["keyId"], "targetLabel": "//lib:gen", "changes": 1.0, "newVersions": 1.0, "direct": 0.0, "dependency": 1.0, "both": 0.0, "unknown": 0.0,
			"executedAfterChange": 1.0, "timeMs": 300.0,
			"topCauses": []any{map[string]any{"name": "//other (sources)", "kind": "SOURCE", "count": 1.0}},
		},
	}, ai.Churn)

	require.Equal(t, []map[string]any{
		{"name": "//lib (k8-opt)", "kind": "GENERATED", "changes": 1.0, "newVersions": 1.0, "actionsAffected": 1.0, "timeMs": 2100.0},
		{"name": "cmd", "kind": "COMMAND", "changes": 1.0, "newVersions": 1.0, "actionsAffected": 1.0, "timeMs": 1500.0},
		{"name": "src:app/main.cc", "kind": "SOURCE", "changes": 1.0, "newVersions": 1.0, "actionsAffected": 1.0, "timeMs": 1500.0},
		{"name": "//other (sources)", "kind": "SOURCE", "changes": 1.0, "newVersions": 1.0, "actionsAffected": 1.0, "timeMs": 300.0},
	}, ai.Causes)

	// The limit counts analyzed invocations only; the coverage counts
	// start at the oldest of them.
	ai = run(map[string]any{"limit": 2})
	require.Equal(t, 2, ai.InvocationsAnalyzed)
	require.Equal(t, 5, ai.InvocationsMatched)
	require.Equal(t, 1, ai.ChangesTotal)
	require.Len(t, ai.Churn, 1)

	// Filters apply before the limit.
	where := map[string]any{"startedAtGT": base.Add(150 * time.Minute).Format(time.RFC3339)}
	ai = run(map[string]any{"where": where})
	require.Equal(t, 1, ai.InvocationsAnalyzed)
	require.Equal(t, 4, ai.InvocationsMatched)
	require.Equal(t, 1, ai.RefsAnalyzed)
	require.Zero(t, ai.ChangesTotal)

	// Without analyzed invocations, all matches are counted.
	where = map[string]any{"startedAtGT": base.Add(210 * time.Minute).Format(time.RFC3339)}
	ai = run(map[string]any{"where": where})
	require.Zero(t, ai.InvocationsAnalyzed)
	require.Equal(t, 3, ai.InvocationsMatched)
	require.Equal(t, []int{1, 1, 1}, []int{ai.InvocationsPending, ai.InvocationsFailed, ai.InvocationsWithoutLog})
	require.Empty(t, ai.Churn)

	// Filtering by ref scopes churn to one branch.
	where = map[string]any{"hasSourceControlWith": []any{map[string]any{"ref": "main"}}}
	ai = run(map[string]any{"where": where})
	require.Equal(t, 2, ai.InvocationsAnalyzed)
	require.Equal(t, 1, ai.RefsAnalyzed)
	require.Equal(t, 2, ai.ChangesTotal)

	var history struct {
		ActionHistory []map[string]any
	}
	require.NoError(t, json.Unmarshal(query(t, server.URL, historyQuery, map[string]any{"key": compileID}), &history))
	exec := func(ms float64) map[string]any {
		return map[string]any{"runner": "remote", "totalMs": ms, "startOffsetMs": 5.0}
	}
	require.Equal(t, []map[string]any{
		{
			"invocationId": a.InvocationID.String(), "instanceName": "main", "digestFunction": "sha256", "changed": false, "newVersion": false,
			"cause": nil, "changedGroups": []any{}, "digest": map[string]any{"hash": "0b", "sizeBytes": 11.0},
			"previousDigest": nil, "execution": exec(1000), "executions": 1.0, "executedMs": 1000.0,
		},
		{
			"invocationId": b.InvocationID.String(), "instanceName": "main", "digestFunction": "sha256", "changed": true, "newVersion": true,
			"cause": "DEPENDENCY", "changedGroups": []any{"//lib (k8-opt)"}, "digest": map[string]any{"hash": "0c", "sizeBytes": 12.0},
			"previousDigest": map[string]any{"hash": "0b"}, "execution": exec(2000), "executions": 2.0, "executedMs": 2100.0,
		},
		{
			"invocationId": c.InvocationID.String(), "instanceName": "main", "digestFunction": "sha256", "changed": true, "newVersion": true,
			"cause": "DIRECT", "changedGroups": []any{"cmd", "src:app/main.cc"}, "digest": map[string]any{"hash": "0d", "sizeBytes": 13.0},
			"previousDigest": map[string]any{"hash": "0c"}, "execution": exec(1500), "executions": 1.0, "executedMs": 1500.0,
		},
	}, history.ActionHistory)

	// The test action was a cache hit in both invocations that ran it.
	testID := helpers.GraphQLIDFromTypeAndID("ActionKey", test.ID)
	require.NoError(t, json.Unmarshal(query(t, server.URL, historyQuery, map[string]any{"key": testID}), &history))
	require.Len(t, history.ActionHistory, 2)
	for _, h := range history.ActionHistory {
		require.Equal(t, false, h["changed"])
		require.Nil(t, h["execution"])
		require.Equal(t, 0.0, h["executions"])
	}

	// Unknown keys have no history.
	require.NoError(t, json.Unmarshal(query(t, server.URL, historyQuery, map[string]any{"key": helpers.GraphQLIDFromTypeAndID("ActionKey", 1<<40)}), &history))
	require.Empty(t, history.ActionHistory)
}

const filterOptionsQuery = `query ($where: BazelInvocationWhereInput) {
  actionInsightsFilterOptions(where: $where) {
    repos { value invocations }
    refs { repo ref invocations }
    users { value invocations }
  }
}`

func TestActionInsightsFilterOptions(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	authService := dbauthservice.NewDbAuthService(client, clock.SystemClock, auth.NewJMESPathExpressionAuthorizer(
		jmespath.MustCompile("instanceName == 'main'"),
	), 0)
	srv := graphql.NewGraphqlHandler(db, noop.NewTracerProvider())
	srv.AroundOperations(func(ctx context.Context, next gqlgen.OperationHandler) gqlgen.ResponseHandler {
		return next(dbauthservice.NewContextWithDbAuthService(ctx, authService))
	})
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	main := testutils.CreateInstanceName(ctx, t, client, "main")
	denied := testutils.CreateInstanceName(ctx, t, client, "denied")
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	hour := 0
	invocation := func(instance *ent.InstanceName, user string, status execlogsummary.Status, repo, ref string) {
		hour++
		create := testutils.StartCreateInvocation(client, instance).SetStartedAt(base.Add(time.Duration(hour) * time.Hour))
		if user != "" {
			create.SetUsername(user)
		}
		inv := create.SaveX(ctx)
		if status != "" {
			client.ExecLogSummary.Create().SetBazelInvocation(inv).SetStatus(status).SetStartedAt(base).
				SetSpawnIndex(actioninsights.EncodeSpawnIndex(nil)).SaveX(ctx)
		}
		if repo != "" || ref != "" {
			sc := client.SourceControl.Create().SetBazelInvocation(inv)
			if repo != "" {
				sc.SetRepo(repo)
			}
			if ref != "" {
				sc.SetRef(ref)
			}
			sc.SaveX(ctx)
		}
	}
	done := execlogsummary.StatusDONE
	invocation(main, "alice", done, "tsg/nebula", "features/pilot")
	invocation(main, "bob", done, "tsg/nebula", "features/pilot")
	invocation(main, "alice", done, "tsg/nebula", "mr-branch")
	invocation(main, "alice", done, "ctc/monorepo", "features/pilot")
	invocation(main, "carol", done, "", "")
	invocation(main, "", done, "", "local-ref")
	// Not analyzed: no ingested log, a failed one, or not authorized.
	invocation(main, "dave", "", "tsg/nebula", "other")
	invocation(main, "erin", execlogsummary.StatusFAILED, "tsg/nebula", "other")
	invocation(denied, "mallory", done, "secret/repo", "main")

	type option struct {
		Value       string
		Ref         string
		Repo        *string
		Invocations int
	}
	run := func(where map[string]any) (repos, refs, users []option) {
		var got struct {
			ActionInsightsFilterOptions struct{ Repos, Refs, Users []option }
		}
		require.NoError(t, json.Unmarshal(query(t, server.URL, filterOptionsQuery, map[string]any{"where": where}), &got))
		o := got.ActionInsightsFilterOptions
		return o.Repos, o.Refs, o.Users
	}
	str := func(s string) *string { return &s }

	repos, refs, users := run(nil)
	require.Equal(t, []option{{Value: "tsg/nebula", Invocations: 3}, {Value: "ctc/monorepo", Invocations: 1}}, repos)
	require.Equal(t, []option{
		{Repo: str("tsg/nebula"), Ref: "features/pilot", Invocations: 2},
		{Repo: str("ctc/monorepo"), Ref: "features/pilot", Invocations: 1},
		{Ref: "local-ref", Invocations: 1},
		{Repo: str("tsg/nebula"), Ref: "mr-branch", Invocations: 1},
	}, refs)
	require.Equal(t, []option{{Value: "alice", Invocations: 3}, {Value: "bob", Invocations: 1}, {Value: "carol", Invocations: 1}}, users)

	// The filter applies.
	repos, refs, users = run(map[string]any{"startedAtGT": base.Add(150 * time.Minute).Format(time.RFC3339)})
	require.Equal(t, []option{{Value: "ctc/monorepo", Invocations: 1}, {Value: "tsg/nebula", Invocations: 1}}, repos)
	require.Len(t, refs, 3)
	require.Equal(t, []option{{Value: "alice", Invocations: 2}, {Value: "carol", Invocations: 1}}, users)

	// The filters the page builds: a CI branch, users, and CI or not, where
	// CI means a source control with a repo.
	var analyzed struct {
		ActionInsights struct{ InvocationsAnalyzed, RefsAnalyzed int }
	}
	count := func(where map[string]any) int {
		require.NoError(t, json.Unmarshal(query(t, server.URL, insightsQuery, map[string]any{"where": where}), &analyzed))
		return analyzed.ActionInsights.InvocationsAnalyzed
	}
	// A ref in two repos counts as two.
	require.Equal(t, 3, count(map[string]any{"hasSourceControlWith": []any{map[string]any{"ref": "features/pilot"}}}))
	require.Equal(t, 2, analyzed.ActionInsights.RefsAnalyzed)
	ci := map[string]any{"repoNotNil": true}
	require.Equal(t, 2, count(map[string]any{"hasSourceControlWith": []any{map[string]any{"repoNotNil": true, "repo": "tsg/nebula", "ref": "features/pilot"}}}))
	require.Equal(t, 1, count(map[string]any{"hasSourceControlWith": []any{map[string]any{"ref": "features/pilot", "repo": "ctc/monorepo"}}}))
	require.Equal(t, 4, count(map[string]any{"usernameIn": []string{"alice", "bob"}}))
	require.Equal(t, 4, count(map[string]any{"hasSourceControlWith": []any{ci}}))
	require.Equal(t, 2, count(map[string]any{"not": map[string]any{"hasSourceControlWith": []any{ci}}}))
}
