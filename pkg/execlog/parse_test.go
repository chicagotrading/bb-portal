package execlog_test

import (
	"bytes"
	"sort"
	"testing"
	"time"

	spawnpb "github.com/bazelbuild/bazel/src/main/protobuf"
	"github.com/buildbarn/bb-portal/pkg/execlog"
	"github.com/buildbarn/bb-portal/pkg/execlog/execlogtest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func digest(h string) *spawnpb.Digest {
	return execlogtest.Digest(h)
}

// omitProducers leaves out the spawns producing the compile's inputs, as
// in an incremental build where they were up to date.
const omitProducers = "omit:producers"

// buildLog writes a C++ style log; overrides replace digests by key.
func buildLog(t *testing.T, overrides map[string]string) []byte {
	h := func(path, def string) string {
		if v, ok := overrides[path]; ok {
			return v
		}
		return def
	}
	_, omit := overrides[omitProducers]
	b := &execlogtest.Builder{}
	b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Invocation_{Invocation: &spawnpb.ExecLogEntry_Invocation{
		HashFunctionName:           "SHA-256",
		WorkspaceRunfilesDirectory: "_main",
		Id:                         "inv-1",
	}}})

	// //lib/gen:hdr generates a header.
	genSrc := b.File("lib/gen/gen.py", h("lib/gen/gen.py", "aa"))
	genOut := b.File("bazel-out/k8-opt/bin/lib/gen/gen.h", h("bazel-out/k8-opt/bin/lib/gen/gen.h", "ab"))
	if !omit {
		b.Spawn(&spawnpb.ExecLogEntry_Spawn{
			Args: []string{"python", "gen.py"}, TargetLabel: "@@//lib/gen:hdr", Mnemonic: "Genrule",
			InputSetId: b.Set([]uint32{genSrc}), Runner: "remote", Digest: digest("d1"),
			Metrics: &spawnpb.SpawnMetrics{
				TotalTime:         durationpb.New(1500 * time.Millisecond),
				QueueTime:         durationpb.New(200 * time.Millisecond),
				ExecutionWallTime: durationpb.New(1000 * time.Millisecond),
				InputBytes:        42,
				StartTime:         timestamppb.New(time.Unix(100, 0)),
			},
		}, genOut)
	}

	// //lib/tree:tree produces a tree artifact.
	treeOut := b.Dir("bazel-out/k8-opt/bin/lib/tree/out", map[string]string{"a.txt": h("tree/a.txt", "t1")})
	if !omit {
		b.Spawn(&spawnpb.ExecLogEntry_Spawn{
			Args: []string{"mktree"}, TargetLabel: "//lib/tree:tree", Mnemonic: "MkTree",
			Runner: "remote cache hit", CacheHit: true, Digest: digest("d2"),
		}, treeOut)

		// A symlink action output.
		b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_SymlinkAction_{SymlinkAction: &spawnpb.ExecLogEntry_SymlinkAction{
			InputPath: "lib/link/real.h", OutputPath: "bazel-out/k8-opt/bin/lib/link/real.h", TargetLabel: "//lib/link:link", Mnemonic: "Symlink",
		}}})
	}

	// The tree again under a new ID, plus a member listed on its own.
	treeIn := b.Dir("bazel-out/k8-opt/bin/lib/tree/out", map[string]string{"a.txt": h("tree/a.txt", "t1")})
	treeMember := b.File("bazel-out/k8-opt/bin/lib/tree/out/sub/b.txt", h("tree/b.txt", "t2"))
	linked := b.File("bazel-out/k8-opt/bin/lib/link/real.h", h("link", "l1"))

	ownSrc := b.File("app/main.cc", h("app/main.cc", "m1"))
	ownHdr := b.File("app/main.h", h("app/main.h", "m2"))
	ownSubdir := b.File("app/detail/impl.h", h("app/detail/impl.h", "m3"))
	knownSubpackage := b.File("app/known/k.h", h("app/known/k.h", "k1"))
	headerOnly := b.File("app/hdronly/h.h", h("app/hdronly/h.h", "ho"))
	headerOnlyModuleMap := b.File("bazel-out/k8-opt/bin/app/hdronly/hdronly.cppmap", h("hdronly.cppmap", "hm"))
	ownModuleMap := b.File("bazel-out/k8-opt/bin/app/main.cppmap", h("main.cppmap", "mm"))
	depModuleMap := b.File("bazel-out/k8-opt/bin/lib/util/util.cppmap", h("util.cppmap", "um"))
	otherSrc := b.File("lib/util/util.h", h("lib/util/util.h", "u1"))
	otherSrc2 := b.File("lib/util/more.h", h("lib/util/more.h", "u2"))
	ext := b.File("external/abseil+/absl/base.h", h("external/abseil+/absl/base.h", "e1"))
	genIn := b.File("bazel-out/k8-opt/bin/lib/gen/gen.h", h("bazel-out/k8-opt/bin/lib/gen/gen.h", "ab"))
	orphan := b.File("bazel-out/k8-opt/bin/lib/orphan/x.h", h("orphan", "o1"))
	extGen := b.File("bazel-out/k8-opt/bin/external/zlib+/zconf.h", h("zconf", "z1"))
	solib := b.File("bazel-out/k8-opt/bin/_solib_k8/_U_S_Slib_Sutil_Cutil___Ulib_Sutil/libutil.so", h("libutil.so", "s1"))
	stamp := b.File("bazel-out/volatile-status.txt", h("volatile-status", "vs"))

	// Nested transitive sets, with otherSrc and ext listed twice.
	deps := b.Set([]uint32{otherSrc, ext}, b.Set([]uint32{otherSrc2, ext}))
	gen := b.Set([]uint32{genIn, treeIn, treeMember, linked, orphan, extGen, solib, stamp, depModuleMap}, deps)
	objOut := b.File("bazel-out/k8-opt/bin/app/_objs/main/main.o", h("main.o", "o"))
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args:        []string{"clang", "-c", "app/main.cc"},
		EnvVars:     []*spawnpb.EnvironmentVariable{{Name: "PATH", Value: h("env:PATH", "/bin")}},
		Platform:    &spawnpb.Platform{Properties: []*spawnpb.Platform_Property{{Name: "OSFamily", Value: "linux"}}},
		TargetLabel: "//app:main", Mnemonic: "CppCompile",
		InputSetId: b.Set([]uint32{ownSrc, ownHdr, ownSubdir, knownSubpackage, headerOnly, headerOnlyModuleMap, ownModuleMap}, gen, deps),
		Runner:     "remote", Digest: digest(h("digest:compile", "d3")), ExitCode: 0,
		Metrics: &spawnpb.SpawnMetrics{TotalTime: durationpb.New(3 * time.Second), MeasuredMemoryPeakBytes: 7},
	}, objOut)

	// A link and a test with a runfiles tree.
	bin := b.File("bazel-out/k8-opt/bin/app/main", h("main", "bin"))
	params := b.File("bazel-out/k8-opt/bin/app/main-2.params", h("main-2.params", "p1"))
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args: []string{"clang", "@bazel-out/k8-opt/bin/app/main-2.params"}, TargetLabel: "//app:main", Mnemonic: "CppLink",
		InputSetId: b.Set([]uint32{objOut, params}), Runner: "remote cache hit", CacheHit: true, Digest: digest("d4"),
	}, bin)
	rfArtifacts := b.Set([]uint32{bin, b.File("external/rules_python+/python/runfiles.py", h("rp", "r1"))})
	symlinks := b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_SymlinkEntrySet_{SymlinkEntrySet: &spawnpb.ExecLogEntry_SymlinkEntrySet{
		DirectEntries: map[string]uint32{h("symlink:path", "app/data.txt"): b.File("lib/data/real.txt", h("data", "x1"))},
	}}})
	tree := b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_RunfilesTree_{RunfilesTree: &spawnpb.ExecLogEntry_RunfilesTree{
		Path:                "bazel-out/k8-opt/bin/app/main.runfiles",
		InputSetId:          rfArtifacts,
		SymlinksId:          symlinks,
		EmptyFiles:          []string{"app/__init__.py"},
		RepoMappingManifest: &spawnpb.ExecLogEntry_File{Digest: digest(h("repo_mapping", "rm"))},
	}}})
	rfExt := b.File("bazel-out/k8-opt/bin/app/main.runfiles/rules_cc+/cc/x.h", h("rfext", "c1"))
	testLog := b.File("bazel-out/k8-opt/testlogs/app/main_test/test.log", "tl")
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args: []string{"test-setup.sh"}, TargetLabel: "//app:main_test", Mnemonic: "TestRunner",
		InputSetId: b.Set([]uint32{tree, rfExt}), Runner: "linux-sandbox", Digest: digest("d5"),
		ExitCode: 1, Status: "NON_ZERO_EXIT",
	}, testLog)
	// A spawn with an invalid output and no digest.
	b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Spawn_{Spawn: &spawnpb.ExecLogEntry_Spawn{
		Args:        []string{h("cmd:local", "cc")},
		TargetLabel: "@@abseil+//absl:base", Mnemonic: "CppCompile",
		Outputs: []*spawnpb.ExecLogEntry_Output{{Type: &spawnpb.ExecLogEntry_Output_InvalidOutputPath{InvalidOutputPath: "bazel-out/k8-opt/bin/external/abseil+/x.o"}}},
	}}})

	// A test that writes no test.xml, which Bazel then generates from its
	// test.log in a second TestRunner spawn.
	xml := "bazel-out/k8-opt/testlogs/app/main/main_test/test.xml"
	outputsDir := b.Dir("bazel-out/k8-opt/testlogs/app/main/main_test/test.outputs", nil)
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args: []string{"external/bazel_tools/tools/test/test-setup.sh", "app/main"}, TargetLabel: "//app/main:main_test", Mnemonic: "TestRunner",
		InputSetId: b.Set([]uint32{bin}), Runner: "remote", Digest: digest("d6"),
		Outputs: []*spawnpb.ExecLogEntry_Output{{Type: &spawnpb.ExecLogEntry_Output_InvalidOutputPath{InvalidOutputPath: xml}}},
	}, outputsDir)
	mainTestLog := b.File("bazel-out/k8-opt/testlogs/app/main/main_test/test.log", h("main_test.log", "ml"))
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args: []string{"external/bazel_tools/tools/test/generate-xml.sh", "test.log", "test.xml"}, TargetLabel: "//app/main:main_test", Mnemonic: "TestRunner",
		InputSetId: b.Set([]uint32{mainTestLog}), Runner: "remote", Digest: digest("d7"),
	}, b.File(xml, "tx"))
	raw, err := b.Bytes()
	require.NoError(t, err)
	return raw
}

func parse(t *testing.T, raw []byte) *execlog.Log {
	compressed, err := execlogtest.Compress(raw)
	require.NoError(t, err)
	l, err := execlog.ParseCompressed(bytes.NewReader(compressed), 0)
	require.NoError(t, err)
	l.AddKnownLabels([]string{"//app/known:k"})
	return l
}

func spawnIndex(t *testing.T, l *execlog.Log, label, mnemonic string) int {
	for i, s := range l.Spawns {
		if s.TargetLabel == label && s.Mnemonic == mnemonic {
			return i
		}
	}
	t.Fatalf("no %s spawn of %s", mnemonic, label)
	return -1
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestParseSpawns(t *testing.T) {
	raw := buildLog(t, nil)
	l := parse(t, raw)
	require.Equal(t, "SHA-256", l.HashFunctionName)
	require.Equal(t, "inv-1", l.InvocationID)
	require.Equal(t, int64(len(raw)), l.Bytes)
	require.Len(t, l.Spawns, 8)

	gen := l.Spawns[0]
	require.Equal(t, "//lib/gen:hdr", gen.TargetLabel)
	require.Equal(t, "Genrule", gen.Mnemonic)
	require.Equal(t, "bazel-out/k8-opt/bin/lib/gen/gen.h", gen.PrimaryOutput)
	require.Equal(t, execlog.Digest{Hash: "d1", SizeBytes: 2}, gen.Digest)
	require.False(t, gen.CacheHit)
	require.Equal(t, execlog.Metrics{TotalMs: 1500, QueueMs: 200, ExecWallMs: 1000, InputBytes: 42, StartTime: time.Unix(100, 0).UTC()}, gen.Metrics)

	tree := l.Spawns[1]
	require.True(t, tree.CacheHit)
	require.Equal(t, "bazel-out/k8-opt/bin/lib/tree/out", tree.PrimaryOutput)

	test := l.Spawns[4]
	require.Equal(t, int32(1), test.ExitCode)
	require.Equal(t, "NON_ZERO_EXIT", test.Status)
	require.Equal(t, "linux-sandbox", test.Runner)

	invalid := l.Spawns[5]
	require.Equal(t, "@@abseil+//absl:base", invalid.TargetLabel)
	require.Equal(t, "bazel-out/k8-opt/bin/external/abseil+/x.o", invalid.PrimaryOutput)
	require.Empty(t, invalid.Digest.Hash)

	// The test and its test.xml generation share the primary output but
	// not the mnemonic, so they are different actions.
	run, xml := l.Spawns[6], l.Spawns[7]
	require.Equal(t, "TestRunner", run.Mnemonic)
	require.Equal(t, execlog.TestXMLMnemonic, xml.Mnemonic)
	require.Equal(t, "bazel-out/k8-opt/testlogs/app/main/main_test/test.xml", run.PrimaryOutput)
	require.Equal(t, run.PrimaryOutput, xml.PrimaryOutput)
}

func TestParseSizeLimit(t *testing.T) {
	raw := buildLog(t, nil)
	compressed, err := execlogtest.Compress(raw)
	require.NoError(t, err)
	_, err = execlog.ParseCompressed(bytes.NewReader(compressed), int64(len(raw)))
	require.NoError(t, err)
	_, err = execlog.ParseCompressed(bytes.NewReader(compressed), int64(len(raw))-1)
	require.ErrorIs(t, err, execlog.ErrTooLarge)
}

func TestGroups(t *testing.T) {
	l := parse(t, buildLog(t, nil))
	require.Equal(t, []string{
		"//app/hdronly (k8-opt)",
		"//app/hdronly (sources)",
		"//app/known (sources)",
		"//lib/gen (k8-opt)",
		"//lib/link (k8-opt)",
		"//lib/orphan (k8-opt)",
		"//lib/tree (k8-opt)",
		"//lib/tree/out/sub (k8-opt)",
		"//lib/util (k8-opt)",
		"//lib/util (sources)",
		"@abseil+",
		"@zlib+",
		"bazel-out:volatile-status.txt",
		"cmd",
		"src:app/detail/impl.h",
		"src:app/main.cc",
		"src:app/main.h",
		"src:bazel-out/k8-opt/bin/app/main.cppmap",
	}, keys(l.Groups(2)))

	// The param file is part of the command.
	require.Equal(t, []string{"//app (k8-opt)", "cmd"}, keys(l.Groups(3)))

	require.Equal(t, []string{
		"//app (k8-opt)",
		"//lib/data (sources)",
		"@rules_cc+",
		"@rules_python+",
		"cmd",
	}, keys(l.Groups(4)))

	// The generated test.xml depends on the test's log.
	require.Equal(t, []string{"//app/main:main_test (k8-opt)", "cmd"}, keys(l.Groups(7)))

	// Groups are stable across calls.
	require.Equal(t, l.Groups(2), l.Groups(2))
}

// TestGroupsWithoutProducers checks that group names do not depend on
// which other spawns the log has, as when an incremental build skips
// up-to-date producers.
func TestGroupsWithoutProducers(t *testing.T) {
	full := parse(t, buildLog(t, nil))
	partial := parse(t, buildLog(t, map[string]string{omitProducers: ""}))
	require.Equal(t,
		full.Groups(spawnIndex(t, full, "//app:main", "CppCompile")),
		partial.Groups(spawnIndex(t, partial, "//app:main", "CppCompile")))
}

func changedGroups(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if b[k] != v {
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

func TestGroupChanges(t *testing.T) {
	base := parse(t, buildLog(t, nil))
	for _, tc := range []struct {
		name     string
		override map[string]string
		spawn    int
		want     []string
	}{
		{"OwnSource", map[string]string{"app/main.h": "m9"}, 2, []string{"src:app/main.h"}},
		{"OwnSubdirectorySource", map[string]string{"app/detail/impl.h": "m9"}, 2, []string{"src:app/detail/impl.h"}},
		{"SubpackageSource", map[string]string{"app/known/k.h": "k9"}, 2, []string{"//app/known (sources)"}},
		{"OwnModuleMap", map[string]string{"main.cppmap": "mm9"}, 2, []string{"src:bazel-out/k8-opt/bin/app/main.cppmap"}},
		{"DependencyModuleMap", map[string]string{"util.cppmap": "um9"}, 2, []string{"//lib/util (k8-opt)"}},
		{"OtherPackage", map[string]string{"lib/util/more.h": "u9"}, 2, []string{"//lib/util (sources)"}},
		{"External", map[string]string{"external/abseil+/absl/base.h": "e9"}, 2, []string{"@abseil+"}},
		{"Generated", map[string]string{"bazel-out/k8-opt/bin/lib/gen/gen.h": "ab9"}, 2, []string{"//lib/gen (k8-opt)"}},
		{"TreeMember", map[string]string{"tree/b.txt": "t9"}, 2, []string{"//lib/tree/out/sub (k8-opt)"}},
		{"TreeDirectory", map[string]string{"tree/a.txt": "t9"}, 2, []string{"//lib/tree (k8-opt)"}},
		// Named after the package of the library, not _solib_k8.
		{"SharedLibrary", map[string]string{"libutil.so": "s9"}, 2, []string{"//lib/util (k8-opt)"}},
		{"Stamp", map[string]string{"volatile-status": "vs9"}, 2, []string{"bazel-out:volatile-status.txt"}},
		{"Environment", map[string]string{"env:PATH": "/usr/bin"}, 2, []string{"cmd"}},
		{"ParamFile", map[string]string{"main-2.params": "p9"}, 3, []string{"cmd"}},
		{"OwnObject", map[string]string{"main.o": "o9"}, 3, []string{"//app (k8-opt)"}},
		{"RunfilesBinary", map[string]string{"main": "bin9"}, 4, []string{"//app (k8-opt)"}},
		{"RunfilesExternal", map[string]string{"rp": "r9"}, 4, []string{"@rules_python+"}},
		{"RunfilesOfExternal", map[string]string{"rfext": "c9"}, 4, []string{"@rules_cc+"}},
		{"RunfilesSymlinkTarget", map[string]string{"data": "x9"}, 4, []string{"//lib/data (sources)"}},
		{"RunfilesSymlinkPath", map[string]string{"symlink:path": "app/data2.txt"}, 4, []string{"//app (k8-opt)"}},
		{"RepoMapping", map[string]string{"repo_mapping": "rm9"}, 4, []string{"//app (k8-opt)"}},
		{"TestLog", map[string]string{"main_test.log": "ml9"}, 7, []string{"//app/main:main_test (k8-opt)"}},
		{"Unrelated", map[string]string{"lib/gen/gen.py": "ff"}, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := parse(t, buildLog(t, tc.override))
			require.Equal(t, tc.want, changedGroups(base.Groups(tc.spawn), changed.Groups(tc.spawn)))
		})
	}
}

// TestOwnRunfilesTree checks that the layout of a binary's own runfiles
// tree belongs to the binary.
func TestOwnRunfilesTree(t *testing.T) {
	build := func(symlinkPath string) *execlog.Log {
		b := &execlogtest.Builder{}
		bin := b.File("bazel-out/k8-opt/bin/app/tool", "bin")
		symlinks := b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_SymlinkEntrySet_{SymlinkEntrySet: &spawnpb.ExecLogEntry_SymlinkEntrySet{
			DirectEntries: map[string]uint32{symlinkPath: b.File("lib/data/real.txt", "x1")},
		}}})
		tree := b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_RunfilesTree_{RunfilesTree: &spawnpb.ExecLogEntry_RunfilesTree{
			Path: "bazel-out/k8-opt/bin/app/tool.runfiles", InputSetId: b.Set([]uint32{bin}), SymlinksId: symlinks,
		}}})
		b.Spawn(&spawnpb.ExecLogEntry_Spawn{
			Args: []string{"tool"}, TargetLabel: "//app:tool", Mnemonic: "Tool",
			InputSetId: b.Set([]uint32{tree}), Digest: digest("d1"),
		})
		raw, err := b.Bytes()
		require.NoError(t, err)
		return parse(t, raw)
	}
	a, b := build("app/data.txt"), build("app/data2.txt")
	require.Equal(t, []string{"//app (k8-opt)", "//lib/data (sources)", "cmd", "src:bazel-out/k8-opt/bin/app/tool.runfiles"}, keys(a.Groups(0)))
	require.Equal(t, []string{"src:bazel-out/k8-opt/bin/app/tool.runfiles"}, changedGroups(a.Groups(0), b.Groups(0)))
}

func TestFillMissingDigests(t *testing.T) {
	l := parse(t, buildLog(t, nil))
	l.FillMissingDigests()
	require.Equal(t, execlog.Digest{Hash: "d1", SizeBytes: 2}, l.Spawns[0].Digest)
	d := l.Spawns[5].Digest
	require.Len(t, d.Hash, 64)
	require.Zero(t, d.SizeBytes)

	again := parse(t, buildLog(t, nil))
	again.FillMissingDigests()
	require.Equal(t, d, again.Spawns[5].Digest)

	changed := parse(t, buildLog(t, map[string]string{"cmd:local": "cc2"}))
	changed.FillMissingDigests()
	require.NotEqual(t, d, changed.Spawns[5].Digest)
}

func TestGroupKinds(t *testing.T) {
	for name, want := range map[string]execlog.GroupKind{
		"cmd":             execlog.GroupCommand,
		"src:app/main.cc": execlog.GroupSource,
		"src:bazel-out/k8-opt/bin/app/main.cppmap": execlog.GroupSource,
		"//lib/util (sources)":                     execlog.GroupSource,
		"//lib/gen (k8-opt)":                       execlog.GroupGenerated,
		"//app:main_test (k8-opt)":                 execlog.GroupGenerated,
		"bazel-out:volatile-status.txt":            execlog.GroupGenerated,
		"@abseil+":                                 execlog.GroupExternal,
		"@+download_gcc+gcc":                       execlog.GroupExternal,
		"@ctc_bazel_tools++conda+e":                execlog.GroupExternal,
	} {
		require.Equal(t, want, execlog.KindOfGroup(name), name)
	}
}

func TestIsDirectGroup(t *testing.T) {
	require.True(t, execlog.IsDirectGroup("cmd", "//app:main"))
	require.True(t, execlog.IsDirectGroup("src:app/main.cc", "//app:main"))
	require.True(t, execlog.IsDirectGroup("src:bazel-out/k8-opt/bin/app/main.cppmap", "//app:main"))
	require.True(t, execlog.IsDirectGroup("@openssl+", "@@openssl+//:crypto"))
	require.False(t, execlog.IsDirectGroup("@abseil+", "@@openssl+//:crypto"))
	require.False(t, execlog.IsDirectGroup("@abseil+", "//app:main"))
	require.False(t, execlog.IsDirectGroup("//lib/util (sources)", "//app:main"))
	require.False(t, execlog.IsDirectGroup("//lib/gen (k8-opt)", "//app:main"))
	// The target's own outputs come from other spawns.
	require.False(t, execlog.IsDirectGroup("//app (k8-opt)", "//app:main"))
	require.False(t, execlog.IsDirectGroup("//app:main_test (k8-opt)", "//app:main_test"))
	require.False(t, execlog.IsDirectGroup("bazel-out:volatile-status.txt", "//app:main"))
}

func TestLabelRepoAndPackage(t *testing.T) {
	for label, want := range map[string][2]string{
		"//app:main":           {"", "app"},
		"@@//app/sub:main":     {"", "app/sub"},
		"//:root":              {"", ""},
		"@@openssl+//:crypto":  {"openssl+", ""},
		"@abseil+//absl:base":  {"abseil+", "absl"},
		"@@+ext+repo//a/b:c.h": {"+ext+repo", "a/b"},
	} {
		repo, pkg := execlog.LabelRepoAndPackage(label)
		require.Equal(t, want, [2]string{repo, pkg}, label)
	}
}
