package actioninsights_test

import (
	"testing"

	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/pkg/execlog"
	"github.com/stretchr/testify/require"
)

func inv(id int64, pairs ...actioninsights.Pair) actioninsights.Invocation {
	return actioninsights.Invocation{ID: id, Index: actioninsights.EncodeSpawnIndex(pairs)}
}

func TestFindChanges(t *testing.T) {
	invocations := []actioninsights.Invocation{
		inv(1, actioninsights.Pair{KeyID: 1, VersionID: 10}, actioninsights.Pair{KeyID: 2, VersionID: 20}),
		inv(2, actioninsights.Pair{KeyID: 1, VersionID: 11}),
		// Key 2 skipped invocation 2 and changed since invocation 1.
		inv(3, actioninsights.Pair{KeyID: 1, VersionID: 11}, actioninsights.Pair{KeyID: 2, VersionID: 21}),
		// Key 1 returns to its first version; key 3 has two versions and
		// counts as the lower.
		inv(4, actioninsights.Pair{KeyID: 1, VersionID: 10}, actioninsights.Pair{KeyID: 3, VersionID: 31}, actioninsights.Pair{KeyID: 3, VersionID: 30}),
		inv(5, actioninsights.Pair{KeyID: 3, VersionID: 31}),
	}
	changes, err := actioninsights.FindChanges(invocations)
	require.NoError(t, err)
	require.Equal(t, []actioninsights.Change{
		{KeyID: 1, Invocation: 2, Previous: 1, VersionID: 11, PrevVersion: 10, NewVersion: true},
		{KeyID: 2, Invocation: 3, Previous: 1, VersionID: 21, PrevVersion: 20, NewVersion: true},
		{KeyID: 1, Invocation: 4, Previous: 3, VersionID: 10, PrevVersion: 11, NewVersion: false},
		{KeyID: 3, Invocation: 5, Previous: 4, VersionID: 31, PrevVersion: 30, NewVersion: true},
	}, changes)

	history, err := actioninsights.History(invocations, 2)
	require.NoError(t, err)
	require.Equal(t, []actioninsights.HistoryEntry{
		{Invocation: 1, VersionID: 20},
		{Invocation: 3, VersionID: 21, PrevVersion: 20, Changed: true, NewVersion: true},
	}, history)
	history, err = actioninsights.History(invocations, 1)
	require.NoError(t, err)
	require.Equal(t, []actioninsights.HistoryEntry{
		{Invocation: 1, VersionID: 10},
		{Invocation: 2, VersionID: 11, PrevVersion: 10, Changed: true, NewVersion: true},
		{Invocation: 3, VersionID: 11, PrevVersion: 11},
		{Invocation: 4, VersionID: 10, PrevVersion: 11, Changed: true},
	}, history)

	_, err = actioninsights.FindChanges([]actioninsights.Invocation{{ID: 1, Index: []byte{0x80}}})
	require.Error(t, err)
}

func TestClassify(t *testing.T) {
	const label = "//cpp/foo:bar"
	for _, tc := range []struct {
		name    string
		changed []string
		want    actioninsights.Cause
	}{
		{"Command", []string{"cmd"}, actioninsights.CauseDirect},
		{"OwnSource", []string{"src:cpp/foo/bar.cc"}, actioninsights.CauseDirect},
		{"OwnModuleMap", []string{"src:bazel-out/k8-opt/bin/cpp/foo/bar.cppmap"}, actioninsights.CauseDirect},
		// The target's own objects and binaries come from its other
		// actions, which report their own causes.
		{"OwnOutput", []string{"//cpp/foo (k8-opt)"}, actioninsights.CauseDependency},
		{"GeneratedDependency", []string{"//cpp/baz (k8-opt)"}, actioninsights.CauseDependency},
		{"OtherPackageSources", []string{"//cpp/baz (sources)"}, actioninsights.CauseDependency},
		{"External", []string{"@ctc_bazel_tools++conda+env"}, actioninsights.CauseDependency},
		{"Stamp", []string{"bazel-out:volatile-status.txt"}, actioninsights.CauseDependency},
		{"Both", []string{"cmd", "@abseil+"}, actioninsights.CauseBoth},
		{"Nothing", nil, actioninsights.CauseUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, actioninsights.Classify(tc.changed, label))
		})
	}
	// An external target's own repository is a direct input.
	require.Equal(t, actioninsights.CauseDirect, actioninsights.Classify([]string{"@openssl+"}, "@@openssl+//:crypto"))
	require.Equal(t, "BOTH", actioninsights.CauseBoth.String())
}

func TestExplainer(t *testing.T) {
	groups := map[int64]map[string]string{
		1: {"cmd": "a", "src:cpp/foo/bar.cc": "b", "//cpp/baz (k8-opt)": "c"},
		2: {"cmd": "a", "src:cpp/foo/bar.cc": "b", "//cpp/baz (k8-opt)": "d"},
		3: {"cmd": "z", "src:cpp/foo/bar.cc": "b", "//cpp/baz (k8-opt)": "d", "@abseil+": "e"},
		4: {"cmd": "z", "src:cpp/foo/bar.cc": "b", "//cpp/baz (k8-opt)": "d", "@abseil+": "e"},
	}
	calls := 0
	e := actioninsights.NewExplainer(func(id int64) (map[string]string, bool) {
		calls++
		g, ok := groups[id]
		return g, ok
	})
	cause, changed := e.Explain(1, 2, "//cpp/foo:bar")
	require.Equal(t, actioninsights.CauseDependency, cause)
	require.Equal(t, []string{"//cpp/baz (k8-opt)"}, changed)

	cause, changed = e.Explain(2, 3, "//cpp/foo:bar")
	require.Equal(t, actioninsights.CauseBoth, cause)
	require.Equal(t, []string{"@abseil+", "cmd"}, changed)

	cause, changed = e.Explain(3, 4, "//cpp/foo:bar")
	require.Equal(t, actioninsights.CauseUnknown, cause)
	require.Empty(t, changed)

	// Missing groups and repeated pairs.
	cause, _ = e.Explain(4, 99, "//cpp/foo:bar")
	require.Equal(t, actioninsights.CauseUnknown, cause)
	before := calls
	e.Explain(1, 2, "//cpp/foo:bar")
	require.Equal(t, before, calls)
}

func TestAggregate(t *testing.T) {
	ch := func(key, invocation int64, newVersion bool, cause actioninsights.Cause, changed ...string) actioninsights.Explained {
		return actioninsights.Explained{
			Change:  actioninsights.Change{KeyID: key, Invocation: invocation, NewVersion: newVersion},
			Cause:   cause,
			Changed: changed,
		}
	}
	churn, causes := actioninsights.Aggregate([]actioninsights.Explained{
		ch(1, 2, true, actioninsights.CauseDependency, "//cpp/baz (k8-opt)"),
		ch(1, 3, true, actioninsights.CauseBoth, "//cpp/baz (k8-opt)", "cmd"),
		// Key 2 returns to a known version.
		ch(2, 3, false, actioninsights.CauseDependency, "//cpp/baz (k8-opt)"),
		ch(1, 4, false, actioninsights.CauseUnknown),
		ch(3, 4, true, actioninsights.CauseDirect, "src:cpp/qux/a.cc"),
	}, map[actioninsights.ExecKey]int64{
		{Invocation: 2, KeyID: 1}: 100,
		{Invocation: 3, KeyID: 2}: 50,
		{Invocation: 4, KeyID: 3}: 7,
	})
	// Ranked by new versions, then changes.
	require.Equal(t, []actioninsights.ChurnRow{
		{
			KeyID: 1, Changes: 3, NewVersions: 2, Dependency: 1, Both: 1, Unknown: 1, ExecutedAfterChange: 1, TimeMs: 100,
			TopCauses: []actioninsights.NameCount{{Name: "//cpp/baz (k8-opt)", Count: 2}, {Name: "cmd", Count: 1}},
		},
		{
			KeyID: 3, Changes: 1, NewVersions: 1, Direct: 1, ExecutedAfterChange: 1, TimeMs: 7,
			TopCauses: []actioninsights.NameCount{{Name: "src:cpp/qux/a.cc", Count: 1}},
		},
		{
			KeyID: 2, Changes: 1, Dependency: 1, ExecutedAfterChange: 1, TimeMs: 50,
			TopCauses: []actioninsights.NameCount{{Name: "//cpp/baz (k8-opt)", Count: 1}},
		},
	}, churn)
	require.Equal(t, []actioninsights.CauseRow{
		{Name: "//cpp/baz (k8-opt)", Kind: execlog.GroupGenerated, Changes: 3, NewVersions: 2, ActionsAffected: 2, TimeMs: 150},
		{Name: "src:cpp/qux/a.cc", Kind: execlog.GroupSource, Changes: 1, NewVersions: 1, ActionsAffected: 1, TimeMs: 7},
		{Name: "cmd", Kind: execlog.GroupCommand, Changes: 1, NewVersions: 1, ActionsAffected: 1, TimeMs: 0},
	}, causes)
}

func TestIndexCache(t *testing.T) {
	c := actioninsights.NewIndexCache(10)
	c.Put(1, make([]byte, 4))
	c.Put(2, make([]byte, 4))
	_, ok := c.Get(1)
	require.True(t, ok)
	// Over budget: the least recently used goes.
	c.Put(3, make([]byte, 4))
	_, ok = c.Get(2)
	require.False(t, ok)
	_, ok = c.Get(1)
	require.True(t, ok)
	// Too large to cache at all.
	c.Put(4, make([]byte, 11))
	_, ok = c.Get(4)
	require.False(t, ok)
	require.Equal(t, 2, c.Len())
}
