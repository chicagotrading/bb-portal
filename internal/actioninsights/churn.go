package actioninsights

import (
	"slices"
	"sort"

	"github.com/buildbarn/bb-portal/pkg/execlog"
)

// Invocation is an analyzed invocation and its spawn index.
type Invocation struct {
	ID    int64
	Index SpawnIndex
}

// Change is a key whose version differs from its previous one.
type Change struct {
	KeyID int64
	// Invocation and Previous are invocation IDs.
	Invocation  int64
	Previous    int64
	VersionID   int64
	PrevVersion int64
	// NewVersion is false when the key returns to a version it had in an
	// earlier invocation, as when builds of two branches alternate.
	NewVersion bool
}

// FindChanges returns the changes over invocations ordered oldest first.
// A key with several versions in one invocation counts as its lowest.
func FindChanges(invocations []Invocation) ([]Change, error) {
	type last struct {
		invocation int64
		version    int64
		// Versions seen before the last one, kept per key: most keys
		// have one, while a set over all pairs grows with every spawn.
		earlier []int64
	}
	seen := map[int64]*last{}
	var out []Change
	for _, inv := range invocations {
		var prevKey int64 = -1
		if err := inv.Index.ForEach(func(p Pair) bool {
			if p.KeyID == prevKey {
				return true
			}
			prevKey = p.KeyID
			l, ok := seen[p.KeyID]
			if !ok {
				seen[p.KeyID] = &last{invocation: inv.ID, version: p.VersionID}
				return true
			}
			if l.version != p.VersionID {
				out = append(out, Change{
					KeyID:       p.KeyID,
					Invocation:  inv.ID,
					Previous:    l.invocation,
					VersionID:   p.VersionID,
					PrevVersion: l.version,
					NewVersion:  !slices.Contains(l.earlier, p.VersionID),
				})
				l.earlier = append(l.earlier, l.version)
				l.version = p.VersionID
			}
			l.invocation = inv.ID
			return true
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// HistoryEntry is the version of a key in one invocation.
type HistoryEntry struct {
	Invocation  int64
	VersionID   int64
	PrevVersion int64
	Changed     bool
	NewVersion  bool
}

// History returns a key's versions in the invocations that have it.
func History(invocations []Invocation, keyID int64) ([]HistoryEntry, error) {
	var out []HistoryEntry
	var prev int64
	have := false
	seen := map[int64]struct{}{}
	for _, inv := range invocations {
		v, ok, err := inv.Index.Version(keyID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		e := HistoryEntry{Invocation: inv.ID, VersionID: v}
		_, known := seen[v]
		if have {
			e.PrevVersion = prev
			e.Changed = v != prev
			e.NewVersion = e.Changed && !known
		}
		out = append(out, e)
		seen[v] = struct{}{}
		prev, have = v, true
	}
	return out, nil
}

// Cause says whether a change came from the action or its dependencies.
type Cause int

const (
	// CauseUnknown is a digest change without a fingerprint difference.
	CauseUnknown Cause = iota
	// CauseDirect is a change of the command or the target's own inputs.
	CauseDirect
	// CauseDependency is a change of inputs owned by other targets.
	CauseDependency
	// CauseBoth is a change of both kinds.
	CauseBoth
)

func (c Cause) String() string {
	switch c {
	case CauseDirect:
		return "DIRECT"
	case CauseDependency:
		return "DEPENDENCY"
	case CauseBoth:
		return "BOTH"
	}
	return "UNKNOWN"
}

// ChangedGroups returns the sorted names of groups that differ.
func ChangedGroups(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Classify returns the cause of a change of an action of targetLabel.
func Classify(changed []string, targetLabel string) Cause {
	direct, dependency := false, false
	for _, g := range changed {
		if execlog.IsDirectGroup(g, targetLabel) {
			direct = true
		} else {
			dependency = true
		}
	}
	switch {
	case direct && dependency:
		return CauseBoth
	case direct:
		return CauseDirect
	case dependency:
		return CauseDependency
	}
	return CauseUnknown
}

// Explained is a change with its cause.
type Explained struct {
	Change
	Cause   Cause
	Changed []string
}

// Explainer classifies changes, computing each version pair's diff once.
type Explainer struct {
	groups func(versionID int64) (map[string]string, bool)
	cache  map[[2]int64][]string
}

// NewExplainer returns an Explainer reading version groups from groups.
func NewExplainer(groups func(versionID int64) (map[string]string, bool)) *Explainer {
	return &Explainer{groups: groups, cache: map[[2]int64][]string{}}
}

// Explain returns the cause and changed groups between two versions.
func (e *Explainer) Explain(prev, next int64, targetLabel string) (Cause, []string) {
	k := [2]int64{prev, next}
	changed, ok := e.cache[k]
	if !ok {
		a, okA := e.groups(prev)
		b, okB := e.groups(next)
		if okA && okB {
			changed = ChangedGroups(a, b)
		}
		e.cache[k] = changed
	}
	return Classify(changed, targetLabel), changed
}

// NameCount is a group name and how often it changed.
type NameCount struct {
	Name  string
	Count int
}

// ChurnRow aggregates the changes of one key.
type ChurnRow struct {
	KeyID   int64
	Changes int
	// NewVersions counts changes to a version not seen before.
	NewVersions int
	Direct      int
	Dependency  int
	Both        int
	Unknown     int
	// ExecutedAfterChange counts changes after which the key was executed.
	ExecutedAfterChange int
	// TimeMs is executed time in invocations where the key changed.
	TimeMs    int64
	TopCauses []NameCount
}

// CauseRow aggregates how often a group changed under some action.
type CauseRow struct {
	Name            string
	Kind            execlog.GroupKind
	Changes         int
	NewVersions     int
	ActionsAffected int
	TimeMs          int64
}

// ExecKey identifies the executions of a key in an invocation.
type ExecKey struct {
	Invocation int64
	KeyID      int64
}

const topCauses = 5

// Aggregate builds churn rows per key and cause rows per group.
func Aggregate(changes []Explained, execMs map[ExecKey]int64) ([]ChurnRow, []CauseRow) {
	rows := map[int64]*ChurnRow{}
	keyCauses := map[int64]map[string]int{}
	type causeAcc struct {
		row  CauseRow
		keys map[int64]struct{}
	}
	causes := map[string]*causeAcc{}
	for _, c := range changes {
		r := rows[c.KeyID]
		if r == nil {
			r = &ChurnRow{KeyID: c.KeyID}
			rows[c.KeyID] = r
			keyCauses[c.KeyID] = map[string]int{}
		}
		r.Changes++
		if c.NewVersion {
			r.NewVersions++
		}
		switch c.Cause {
		case CauseDirect:
			r.Direct++
		case CauseDependency:
			r.Dependency++
		case CauseBoth:
			r.Both++
		default:
			r.Unknown++
		}
		ms, executed := execMs[ExecKey{Invocation: c.Invocation, KeyID: c.KeyID}]
		if executed {
			r.ExecutedAfterChange++
			r.TimeMs += ms
		}
		for _, g := range c.Changed {
			keyCauses[c.KeyID][g]++
			a := causes[g]
			if a == nil {
				a = &causeAcc{row: CauseRow{Name: g, Kind: execlog.KindOfGroup(g)}, keys: map[int64]struct{}{}}
				causes[g] = a
			}
			a.row.Changes++
			if c.NewVersion {
				a.row.NewVersions++
			}
			a.keys[c.KeyID] = struct{}{}
			a.row.TimeMs += ms
		}
	}

	churn := make([]ChurnRow, 0, len(rows))
	for id, r := range rows {
		for name, n := range keyCauses[id] {
			r.TopCauses = append(r.TopCauses, NameCount{Name: name, Count: n})
		}
		sort.Slice(r.TopCauses, func(i, j int) bool {
			if r.TopCauses[i].Count != r.TopCauses[j].Count {
				return r.TopCauses[i].Count > r.TopCauses[j].Count
			}
			return r.TopCauses[i].Name < r.TopCauses[j].Name
		})
		if len(r.TopCauses) > topCauses {
			r.TopCauses = r.TopCauses[:topCauses]
		}
		churn = append(churn, *r)
	}
	// New versions first: alternating between known versions is cheap,
	// as each of them is likely cached.
	sort.Slice(churn, func(i, j int) bool {
		if churn[i].NewVersions != churn[j].NewVersions {
			return churn[i].NewVersions > churn[j].NewVersions
		}
		if churn[i].Changes != churn[j].Changes {
			return churn[i].Changes > churn[j].Changes
		}
		if churn[i].TimeMs != churn[j].TimeMs {
			return churn[i].TimeMs > churn[j].TimeMs
		}
		return churn[i].KeyID < churn[j].KeyID
	})

	causeRows := make([]CauseRow, 0, len(causes))
	for _, a := range causes {
		a.row.ActionsAffected = len(a.keys)
		causeRows = append(causeRows, a.row)
	}
	sort.Slice(causeRows, func(i, j int) bool {
		if causeRows[i].NewVersions != causeRows[j].NewVersions {
			return causeRows[i].NewVersions > causeRows[j].NewVersions
		}
		if causeRows[i].Changes != causeRows[j].Changes {
			return causeRows[i].Changes > causeRows[j].Changes
		}
		if causeRows[i].TimeMs != causeRows[j].TimeMs {
			return causeRows[i].TimeMs > causeRows[j].TimeMs
		}
		return causeRows[i].Name < causeRows[j].Name
	})
	return churn, causeRows
}
