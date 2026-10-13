package execlog

import (
	"fmt"
	"path"
	"strings"
)

// CommandGroup is the group of a spawn's arguments, environment, platform
// and param files.
const CommandGroup = "cmd"

// GroupKind says what owns the inputs of a group.
type GroupKind int

const (
	// GroupCommand is the spawn's own command line, environment and platform.
	GroupCommand GroupKind = iota
	// GroupSource is workspace source files.
	GroupSource
	// GroupGenerated is outputs of spawns, named by package and configuration.
	GroupGenerated
	// GroupExternal is files of an external repository.
	GroupExternal
)

func (k GroupKind) String() string {
	switch k {
	case GroupCommand:
		return "COMMAND"
	case GroupSource:
		return "SOURCE"
	case GroupExternal:
		return "EXTERNAL"
	default:
		return "GENERATED"
	}
}

const (
	sourcesSuffix   = " (sources)"
	ownSourcePrefix = "src:"
	unknownPrefix   = "bazel-out:"
)

// KindOfGroup returns the kind of a group name.
func KindOfGroup(name string) GroupKind {
	switch {
	case name == CommandGroup:
		return GroupCommand
	case strings.HasPrefix(name, ownSourcePrefix), strings.HasSuffix(name, sourcesSuffix):
		return GroupSource
	case strings.HasPrefix(name, unknownPrefix), strings.Contains(name, "//"):
		return GroupGenerated
	case strings.HasPrefix(name, "@"):
		return GroupExternal
	}
	return GroupGenerated
}

// IsDirectGroup reports whether the group belongs to the target itself:
// its command, its own sources and Bazel-written files, or its own
// external repository. Outputs of other spawns, including the target's
// own, are dependencies.
func IsDirectGroup(name, targetLabel string) bool {
	switch {
	case name == CommandGroup, strings.HasPrefix(name, ownSourcePrefix):
		return true
	case KindOfGroup(name) == GroupExternal:
		repo, _ := LabelRepoAndPackage(targetLabel)
		return repo != "" && name == "@"+repo
	}
	return false
}

func normalizeLabel(label string) string {
	if rest, ok := strings.CutPrefix(label, "@@//"); ok {
		return "//" + rest
	}
	if rest, ok := strings.CutPrefix(label, "@//"); ok {
		return "//" + rest
	}
	return label
}

// LabelRepoAndPackage splits a label; the main repository is "".
func LabelRepoAndPackage(label string) (string, string) {
	label = normalizeLabel(label)
	repo := ""
	if strings.HasPrefix(label, "@") {
		i := strings.Index(label, "//")
		if i < 0 {
			return strings.TrimLeft(label, "@"), ""
		}
		repo = strings.TrimLeft(label[:i], "@")
		label = label[i:]
	}
	pkg := strings.TrimPrefix(label, "//")
	if i := strings.IndexByte(pkg, ':'); i >= 0 {
		pkg = pkg[:i]
	}
	return repo, pkg
}

func firstSegment(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

type pathClass struct {
	name string
	// srcDir is set for main repository source files.
	srcDir string
	isSrc  bool
	// isParams is set for param files, which belong to the command.
	isParams bool
	// ownerLabel is the target whose module map this is.
	ownerLabel string
}

// classifyPath returns the consumer-independent group of a path. It
// depends on the path alone, so a file gets the same group in every log.
func (l *Log) classifyPath(p string) pathClass {
	switch {
	case strings.HasPrefix(p, "external/"):
		return pathClass{name: "@" + firstSegment(p[len("external/"):])}
	case strings.HasPrefix(p, "../"):
		return pathClass{name: "@" + firstSegment(p[len("../"):])}
	case strings.HasPrefix(p, "bazel-out/"):
		return l.classifyGenerated(p)
	}
	dir := path.Dir(p)
	if dir == "." {
		dir = ""
	}
	return pathClass{name: "//" + dir + sourcesSuffix, srcDir: dir, isSrc: true}
}

// outputPackage guesses the package of an output path relative to bin/:
// its directory, cut before rule-internal directories like _objs.
func outputPackage(rel string) string {
	dir := path.Dir(rel)
	if dir == "." {
		return ""
	}
	segments := strings.Split(dir, "/")
	for i, s := range segments {
		if strings.HasPrefix(s, "_") {
			return strings.Join(segments[:i], "/")
		}
	}
	return dir
}

var solibUnescaper = strings.NewReplacer("_U", "_", "_S", "/", "_B", "\\", "_C", ":", "_A", "@")

// solibOwner returns the target of a shared library symlink under
// _solib_<cpu>/, whose directory is the escaped label, "__" and the
// escaped directory of the library.
func solibOwner(rel string) string {
	root, rest, ok := strings.Cut(rel, "/")
	if !ok || !strings.HasPrefix(root, "_solib_") {
		return ""
	}
	dir, _, ok := strings.Cut(rest, "/")
	if !ok || !strings.HasPrefix(dir, "_U") {
		return ""
	}
	escaped, _, ok := strings.Cut(dir[len("_U"):], "__")
	if !ok {
		return ""
	}
	label := solibUnescaper.Replace(escaped)
	if !strings.Contains(label, "//") {
		return ""
	}
	return normalizeLabel(label)
}

// binOutputOwner returns //<dir>:<name> for bazel-out/<cfg>/bin/<dir>/<name>
// in the main repository, the label of a target named after its output.
func binOutputOwner(p string) string {
	parts := strings.SplitN(p, "/", 4)
	if len(parts) != 4 || parts[0] != "bazel-out" || parts[2] != "bin" || strings.HasPrefix(parts[3], "external/") {
		return ""
	}
	dir := path.Dir(parts[3])
	if dir == "." {
		dir = ""
	}
	return "//" + dir + ":" + path.Base(parts[3])
}

// testLogOwner returns <pkg>:<name> of a path relative to testlogs/.
func testLogOwner(rel string) string {
	dir := path.Dir(rel)
	for {
		base := path.Base(dir)
		if base != "test_attempts" && !strings.HasPrefix(base, "shard_") && !strings.HasPrefix(base, "run_") {
			break
		}
		dir = path.Dir(dir)
	}
	if dir == "." {
		return ":"
	}
	pkg := path.Dir(dir)
	if pkg == "." {
		pkg = ""
	}
	return pkg + ":" + path.Base(dir)
}

func (l *Log) classifyGenerated(p string) pathClass {
	rest := p[len("bazel-out/"):]
	cfg, rest, ok := strings.Cut(rest, "/")
	if !ok {
		// Workspace status files, e.g. bazel-out/volatile-status.txt.
		return pathClass{name: unknownPrefix + cfg}
	}
	if i := strings.Index(rest, ".runfiles/"); i >= 0 {
		inTree := rest[i+len(".runfiles/"):]
		if repo := firstSegment(inTree); repo == inTree {
			// A file of the tree itself, like its MANIFEST.
			rest = rest[:i]
		} else if repo != l.WorkspaceRunfilesDirectory {
			return pathClass{name: "@" + repo}
		} else {
			rest = "bin/" + inTree[len(repo)+1:]
		}
	}
	area, rel, ok := strings.Cut(rest, "/")
	if !ok {
		return pathClass{name: unknownPrefix + cfg}
	}
	switch area {
	case "bin", "genfiles":
		if r, ok := strings.CutPrefix(rel, "external/"); ok {
			return pathClass{name: "@" + firstSegment(r)}
		}
		if label := solibOwner(rel); label != "" {
			repo, pkg := LabelRepoAndPackage(label)
			if repo != "" {
				return pathClass{name: "@" + repo}
			}
			return pathClass{name: "//" + pkg + " (" + cfg + ")"}
		}
		c := pathClass{name: "//" + outputPackage(rel) + " (" + cfg + ")"}
		switch {
		case strings.HasSuffix(rel, ".params"):
			c.isParams = true
		case strings.HasSuffix(rel, ".cppmap"):
			c.ownerLabel = binOutputOwner(strings.TrimSuffix(p, ".cppmap"))
		}
		return c
	case "testlogs":
		return pathClass{name: "//" + testLogOwner(rel) + " (" + cfg + ")"}
	}
	dir := path.Dir(rest)
	if dir == "." {
		return pathClass{name: unknownPrefix + cfg}
	}
	return pathClass{name: unknownPrefix + cfg + "/" + dir}
}

// AddKnownLabels records packages that exist besides those of the log's
// spawns, so that their sources are not taken for a parent's own.
func (l *Log) AddKnownLabels(labels []string) {
	for _, label := range labels {
		l.addKnownPackage(label)
	}
}

func (l *Log) addKnownPackage(label string) {
	if repo, pkg := LabelRepoAndPackage(label); repo == "" {
		l.knownPackages[pkg] = struct{}{}
	}
}

// isOwnSource reports whether sources in dir belong to package pkg: dir is
// pkg or a subdirectory of it that is not inside a known other package.
func (l *Log) isOwnSource(dir, pkg string) bool {
	if dir == pkg {
		return true
	}
	if pkg == "" || !strings.HasPrefix(dir, pkg+"/") {
		return false
	}
	for d := dir; d != pkg; d = path.Dir(d) {
		if _, ok := l.knownPackages[d]; ok {
			return false
		}
	}
	return true
}

type grouper struct {
	gen       uint32
	setStamp  []uint32
	pathStamp []uint32
	// pathGroup caches the group index + 1 of each path.
	pathGroup []int32
	// pathFlags caches the file-specific parts of each path's class.
	pathFlags   []uint8
	cppmapOwner map[uint32]string
	groups      []pathClass
	groupIdx    map[string]int32
	accSum      []uint64
	accCnt      []uint32
	accGen      []uint32
	touched     []int32
	own         map[string]ownAcc
	stack       []uint32
	cmd         int32
}

func (l *Log) getGrouper() *grouper {
	g := l.grouper
	if g == nil {
		g = &grouper{groupIdx: map[string]int32{}, cppmapOwner: map[uint32]string{}}
		g.cmd = g.intern(pathClass{name: CommandGroup})
		l.grouper = g
	}
	if len(g.setStamp) < len(l.entries) {
		g.setStamp = append(g.setStamp, make([]uint32, len(l.entries)-len(g.setStamp))...)
	}
	if len(g.pathStamp) < len(l.paths) {
		n := len(l.paths) - len(g.pathStamp)
		g.pathStamp = append(g.pathStamp, make([]uint32, n)...)
		g.pathGroup = append(g.pathGroup, make([]int32, n)...)
		g.pathFlags = append(g.pathFlags, make([]uint8, n)...)
	}
	return g
}

func (g *grouper) intern(c pathClass) int32 {
	gi, ok := g.groupIdx[c.name]
	if !ok {
		gi = int32(len(g.groups))
		g.groups = append(g.groups, c)
		g.groupIdx[c.name] = gi
		g.accSum = append(g.accSum, 0)
		g.accCnt = append(g.accCnt, 0)
		g.accGen = append(g.accGen, 0)
	}
	return gi
}

const (
	flagParams uint8 = 1 << iota
	flagCppmap
)

// groupOf returns the group of a path and caches its flags.
func (l *Log) groupOf(g *grouper, pathID uint32) int32 {
	if gi := g.pathGroup[pathID]; gi != 0 {
		return gi - 1
	}
	c := l.classifyPath(l.paths[pathID])
	gi := g.intern(c)
	g.pathGroup[pathID] = gi + 1
	if c.isParams {
		g.pathFlags[pathID] |= flagParams
	}
	if c.ownerLabel != "" {
		g.pathFlags[pathID] |= flagCppmap
		g.cppmapOwner[pathID] = c.ownerLabel
	}
	return gi
}

type groupWalk struct {
	l     *Log
	g     *grouper
	repo  string
	pkg   string
	label string
}

func (w *groupWalk) addItem(gi int32, item uint64) {
	g := w.g
	if g.accGen[gi] != g.gen {
		g.accGen[gi] = g.gen
		g.accSum[gi] = 0
		g.accCnt[gi] = 0
		g.touched = append(g.touched, gi)
	}
	g.accSum[gi] += item
	g.accCnt[gi]++
}

func (w *groupWalk) addFile(f fileRec) {
	l, g := w.l, w.g
	if g.pathStamp[f.path] == g.gen {
		return
	}
	g.pathStamp[f.path] = g.gen
	gi := l.groupOf(g, f.path)
	if c := &g.groups[gi]; c.isSrc && w.repo == "" && l.isOwnSource(c.srcDir, w.pkg) {
		w.addOwn(ownSourcePrefix+l.paths[f.path], f.item)
		return
	}
	switch flags := g.pathFlags[f.path]; {
	case flags&flagParams != 0:
		w.addItem(g.cmd, f.item)
		return
	case flags&flagCppmap != 0 && g.cppmapOwner[f.path] == w.label:
		// The target's own module map, written from its attributes.
		w.addOwn(ownSourcePrefix+l.paths[f.path], f.item)
		return
	}
	w.addItem(gi, f.item)
}

func (w *groupWalk) leaf(id uint32) {
	l, g := w.l, w.g
	ref := l.entry(id)
	switch ref.kind {
	case kindFile:
		w.addFile(l.files[ref.idx])
	case kindDirectory:
		if g.setStamp[id] == g.gen {
			return
		}
		g.setStamp[id] = g.gen
		dir := l.dirs[ref.idx]
		if !strings.HasPrefix(l.paths[dir.path], "bazel-out/") {
			for _, f := range dir.files {
				w.addFile(f)
			}
			return
		}
		// A tree artifact: its files belong to the group of the tree.
		gi := l.groupOf(g, dir.path)
		for _, f := range dir.files {
			if g.pathStamp[f.path] != g.gen {
				g.pathStamp[f.path] = g.gen
				w.addItem(gi, f.item)
			}
		}
	case kindInputSet:
		w.walk(id)
	case kindRunfilesTree:
		if g.setStamp[id] == g.gen {
			return
		}
		g.setStamp[id] = g.gen
		rt := l.runfiles[ref.idx]
		w.walk(rt.inputSet)
		tree := l.paths[rt.path]
		// Bazel writes the layout of the tree from the attributes of the
		// binary owning it, which may be the target itself.
		binary := strings.TrimSuffix(tree, ".runfiles")
		add := func(item uint64) { w.addItem(g.intern(l.classifyPath(binary)), item) }
		if binOutputOwner(binary) == w.label {
			add = func(item uint64) { w.addOwn(ownSourcePrefix+tree, item) }
		}
		w.symlinks(rt.symlinks, tree+"/"+l.WorkspaceRunfilesDirectory+"/", add)
		w.symlinks(rt.rootSymlinks, tree+"/", add)
		add(rt.extra)
	}
}

func (w *groupWalk) addOwn(name string, item uint64) {
	acc := w.g.own[name]
	w.g.own[name] = ownAcc{sum: acc.sum + item, cnt: acc.cnt + 1}
}

func (w *groupWalk) symlinks(setID uint32, prefix string, add func(item uint64)) {
	l, g := w.l, w.g
	stack := []uint32{setID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ref := l.entry(id)
		if ref.kind != kindSymlinkEntrySet || g.setStamp[id] == g.gen {
			continue
		}
		g.setStamp[id] = g.gen
		rec := l.symlinkSets[ref.idx]
		stack = append(stack, rec.transitive...)
		for _, e := range rec.entries {
			target, ok := l.entryPath(e.target)
			if !ok {
				continue
			}
			w.leaf(e.target)
			add(itemHash(prefix+l.paths[e.path], l.paths[target]))
		}
	}
}

func (w *groupWalk) walk(setID uint32) {
	l, g := w.l, w.g
	base := len(g.stack)
	g.stack = append(g.stack, setID)
	for len(g.stack) > base {
		id := g.stack[len(g.stack)-1]
		g.stack = g.stack[:len(g.stack)-1]
		ref := l.entry(id)
		if ref.kind != kindInputSet || g.setStamp[id] == g.gen {
			continue
		}
		g.setStamp[id] = g.gen
		rec := l.inputSets[ref.idx]
		g.stack = append(g.stack, rec.transitive...)
		for _, in := range rec.inputs {
			w.leaf(in)
		}
	}
}

func groupValue(sum uint64, cnt uint32) string {
	return fmt.Sprintf("%016x", sum^(uint64(cnt)*0x9e3779b97f4a7c15))
}

// Groups maps a spawn's input group names to hashes of their inputs. Group
// names depend only on the input paths and the spawn's target, not on
// which other spawns the log contains.
func (l *Log) Groups(spawn int) map[string]string {
	sp := &l.Spawns[spawn]
	g := l.getGrouper()
	g.gen++
	g.touched = g.touched[:0]
	g.own = map[string]ownAcc{}
	w := groupWalk{l: l, g: g, label: sp.TargetLabel}
	w.repo, w.pkg = LabelRepoAndPackage(sp.TargetLabel)
	w.addItem(g.cmd, sp.cmdHash)
	w.walk(sp.inputSet)

	out := make(map[string]string, len(g.touched)+len(g.own))
	for _, gi := range g.touched {
		out[g.groups[gi].name] = groupValue(g.accSum[gi], g.accCnt[gi])
	}
	for name, acc := range g.own {
		out[name] = groupValue(acc.sum, acc.cnt)
	}
	return out
}

type ownAcc struct {
	sum uint64
	cnt uint32
}
