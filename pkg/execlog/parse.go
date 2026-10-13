// Package execlog parses Bazel compact execution logs.
package execlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	spawnpb "github.com/bazelbuild/bazel/src/main/protobuf"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/types/known/durationpb"
)

// maxEntryID bounds the entry table so a corrupt log cannot exhaust memory.
const maxEntryID = 1 << 28

// Digest is an action or file digest.
type Digest struct {
	Hash      string
	SizeBytes int64
}

// Metrics holds the SpawnMetrics fields kept by the portal.
type Metrics struct {
	TotalMs         int64
	QueueMs         int64
	ExecWallMs      int64
	FetchMs         int64
	UploadMs        int64
	InputBytes      int64
	InputFiles      int64
	MemoryPeakBytes int64
	StartTime       time.Time
}

// Spawn is an executed (or cache hit) spawn of the log.
type Spawn struct {
	TargetLabel   string
	Mnemonic      string
	PrimaryOutput string
	Digest        Digest
	Runner        string
	CacheHit      bool
	ExitCode      int32
	Status        string
	Metrics       Metrics

	inputSet uint32
	cmdHash  uint64
}

// Log is a parsed compact execution log; it keeps entries for Groups.
type Log struct {
	HashFunctionName           string
	WorkspaceRunfilesDirectory string
	InvocationID               string
	Spawns                     []Spawn
	// Entries is the number of log entries read.
	Entries int64
	// Bytes is the uncompressed size of the log.
	Bytes int64

	entries     []entryRef
	paths       []string
	pathIDs     map[string]uint32
	files       []fileRec
	dirs        []dirRec
	inputSets   []setRec
	symlinkSets []symlinkSetRec
	runfiles    []runfilesRec
	// knownPackages are main repository packages, see AddKnownLabels.
	knownPackages map[string]struct{}

	grouper *grouper
}

type entryKind uint8

const (
	kindNone entryKind = iota
	kindFile
	kindDirectory
	kindInputSet
	kindRunfilesTree
	kindSymlinkEntrySet
)

type entryRef struct {
	kind entryKind
	idx  uint32
}

// fileRec is an interned path and a hash of (path, contents).
type fileRec struct {
	path uint32
	item uint64
}

type dirRec struct {
	path  uint32
	files []fileRec
}

type setRec struct {
	inputs     []uint32
	transitive []uint32
}

type symlinkEntry struct {
	path   uint32
	target uint32
}

type symlinkSetRec struct {
	entries    []symlinkEntry
	transitive []uint32
}

type runfilesRec struct {
	path         uint32
	inputSet     uint32
	symlinks     uint32
	rootSymlinks uint32
	// extra is a hash over empty files and the repo mapping manifest.
	extra uint64
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ErrTooLarge is returned when a log decompresses to more than the limit.
var ErrTooLarge = errors.New("decompressed execution log is too large")

// limitReader fails with ErrTooLarge once more than n bytes are read.
type limitReader struct {
	r        io.Reader
	n        int64
	tooLarge bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.tooLarge {
		return 0, ErrTooLarge
	}
	if l.n <= 0 {
		var probe [1]byte
		n, err := l.r.Read(probe[:])
		if n > 0 {
			l.tooLarge = true
			return 0, ErrTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}

// ParseCompressed parses a log as published, zstd compressed. If
// maxBytes is positive, a log decompressing to more fails with
// ErrTooLarge.
func ParseCompressed(r io.Reader, maxBytes int64) (*Log, error) {
	dec, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(1<<27))
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	if maxBytes > 0 {
		return Parse(&limitReader{r: dec, n: maxBytes})
	}
	return Parse(dec)
}

// Parse parses an uncompressed compact execution log.
func Parse(r io.Reader) (*Log, error) {
	cr := &countingReader{r: r}
	br := bufio.NewReaderSize(cr, 1<<20)
	l := &Log{
		WorkspaceRunfilesDirectory: "_main",
		pathIDs:                    map[string]uint32{},
		knownPackages:              map[string]struct{}{},
	}
	opts := protodelim.UnmarshalOptions{MaxSize: 1 << 30}
	var entry spawnpb.ExecLogEntry
	for {
		entry.Reset()
		if err := opts.UnmarshalFrom(br, &entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		l.Entries++
		if err := l.add(&entry); err != nil {
			return nil, err
		}
	}
	l.Bytes = cr.n
	return l, nil
}

func (l *Log) intern(p string) uint32 {
	if id, ok := l.pathIDs[p]; ok {
		return id
	}
	id := uint32(len(l.paths))
	l.paths = append(l.paths, p)
	l.pathIDs[p] = id
	return id
}

func (l *Log) setEntry(id uint32, kind entryKind, idx int) error {
	if id == 0 {
		return nil
	}
	if id >= maxEntryID {
		return errors.New("execution log entry ID " + strconv.FormatUint(uint64(id), 10) + " is too large")
	}
	if int(id) >= len(l.entries) {
		n := max(int(id)+1, 2*len(l.entries))
		grown := make([]entryRef, n)
		copy(grown, l.entries)
		l.entries = grown
	}
	l.entries[id] = entryRef{kind: kind, idx: uint32(idx)}
	return nil
}

func (l *Log) entry(id uint32) entryRef {
	if id == 0 || int(id) >= len(l.entries) {
		return entryRef{}
	}
	return l.entries[id]
}

func itemHash(parts ...string) uint64 {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	var sum [sha256.Size]byte
	return binary.LittleEndian.Uint64(h.Sum(sum[:0]))
}

func fileItem(p string, d *spawnpb.Digest) uint64 {
	return itemHash(p, d.GetHash(), strconv.FormatInt(d.GetSizeBytes(), 10))
}

func (l *Log) add(e *spawnpb.ExecLogEntry) error {
	id := e.GetId()
	switch t := e.Type.(type) {
	case *spawnpb.ExecLogEntry_Invocation_:
		l.HashFunctionName = t.Invocation.GetHashFunctionName()
		if d := t.Invocation.GetWorkspaceRunfilesDirectory(); d != "" {
			l.WorkspaceRunfilesDirectory = d
		}
		l.InvocationID = t.Invocation.GetId()
	case *spawnpb.ExecLogEntry_File_:
		p := t.File.GetPath()
		if m, ok := strings.CutSuffix(p, ".cppmap"); ok {
			// Header-only libraries have no spawns but still get a module map.
			if owner := binOutputOwner(m); owner != "" {
				l.addKnownPackage(owner)
			}
		}
		l.files = append(l.files, fileRec{path: l.intern(p), item: fileItem(p, t.File.GetDigest())})
		return l.setEntry(id, kindFile, len(l.files)-1)
	case *spawnpb.ExecLogEntry_UnresolvedSymlink_:
		p := t.UnresolvedSymlink.GetPath()
		l.files = append(l.files, fileRec{path: l.intern(p), item: itemHash(p, "symlink", t.UnresolvedSymlink.GetTargetPath())})
		return l.setEntry(id, kindFile, len(l.files)-1)
	case *spawnpb.ExecLogEntry_Directory_:
		dir := t.Directory.GetPath()
		rec := dirRec{path: l.intern(dir), files: make([]fileRec, 0, len(t.Directory.GetFiles()))}
		for _, f := range t.Directory.GetFiles() {
			p := dir + "/" + f.GetPath()
			rec.files = append(rec.files, fileRec{path: l.intern(p), item: fileItem(p, f.GetDigest())})
		}
		l.dirs = append(l.dirs, rec)
		return l.setEntry(id, kindDirectory, len(l.dirs)-1)
	case *spawnpb.ExecLogEntry_InputSet_:
		l.inputSets = append(l.inputSets, setRec{
			inputs:     t.InputSet.GetInputIds(),
			transitive: t.InputSet.GetTransitiveSetIds(),
		})
		return l.setEntry(id, kindInputSet, len(l.inputSets)-1)
	case *spawnpb.ExecLogEntry_SymlinkEntrySet_:
		rec := symlinkSetRec{transitive: t.SymlinkEntrySet.GetTransitiveSetIds()}
		for rel, target := range t.SymlinkEntrySet.GetDirectEntries() {
			rec.entries = append(rec.entries, symlinkEntry{path: l.intern(rel), target: target})
		}
		l.symlinkSets = append(l.symlinkSets, rec)
		return l.setEntry(id, kindSymlinkEntrySet, len(l.symlinkSets)-1)
	case *spawnpb.ExecLogEntry_RunfilesTree_:
		rt := t.RunfilesTree
		parts := append([]string{"runfiles"}, rt.GetEmptyFiles()...)
		if m := rt.GetRepoMappingManifest(); m != nil {
			parts = append(parts, "_repo_mapping", m.GetDigest().GetHash())
		}
		l.runfiles = append(l.runfiles, runfilesRec{
			path:         l.intern(rt.GetPath()),
			inputSet:     rt.GetInputSetId(),
			symlinks:     rt.GetSymlinksId(),
			rootSymlinks: rt.GetRootSymlinksId(),
			extra:        itemHash(parts...),
		})
		return l.setEntry(id, kindRunfilesTree, len(l.runfiles)-1)
	case *spawnpb.ExecLogEntry_Spawn_:
		l.addSpawn(t.Spawn)
	}
	return nil
}

func (l *Log) entryPath(id uint32) (uint32, bool) {
	ref := l.entry(id)
	switch ref.kind {
	case kindFile:
		return l.files[ref.idx].path, true
	case kindDirectory:
		return l.dirs[ref.idx].path, true
	case kindRunfilesTree:
		return l.runfiles[ref.idx].path, true
	}
	return 0, false
}

func durationMs(d *durationpb.Duration) int64 {
	if d == nil {
		return 0
	}
	return d.AsDuration().Milliseconds()
}

// TestXMLMnemonic is the mnemonic given to the spawn that generates a
// test's test.xml from its test.log. Bazel calls it TestRunner too, with
// the same first output as the test itself.
const TestXMLMnemonic = "TestRunner (generate-xml)"

func spawnMnemonic(s *spawnpb.ExecLogEntry_Spawn) string {
	m := s.GetMnemonic()
	if args := s.GetArgs(); m == "TestRunner" && len(args) > 0 && path.Base(args[0]) == "generate-xml.sh" {
		return TestXMLMnemonic
	}
	return m
}

func (l *Log) addSpawn(s *spawnpb.ExecLogEntry_Spawn) {
	label := normalizeLabel(s.GetTargetLabel())
	l.addKnownPackage(label)
	sp := Spawn{
		TargetLabel: label,
		Mnemonic:    spawnMnemonic(s),
		Digest:      Digest{Hash: s.GetDigest().GetHash(), SizeBytes: s.GetDigest().GetSizeBytes()},
		Runner:      s.GetRunner(),
		CacheHit:    s.GetCacheHit(),
		ExitCode:    s.GetExitCode(),
		Status:      s.GetStatus(),
		inputSet:    s.GetInputSetId(),
		cmdHash:     commandHash(s),
	}
	if m := s.GetMetrics(); m != nil {
		sp.Metrics = Metrics{
			TotalMs:         durationMs(m.GetTotalTime()),
			QueueMs:         durationMs(m.GetQueueTime()),
			ExecWallMs:      durationMs(m.GetExecutionWallTime()),
			FetchMs:         durationMs(m.GetFetchTime()),
			UploadMs:        durationMs(m.GetUploadTime()),
			InputBytes:      m.GetInputBytes(),
			InputFiles:      m.GetInputFiles(),
			MemoryPeakBytes: m.GetMeasuredMemoryPeakBytes(),
		}
		if st := m.GetStartTime(); st != nil {
			sp.Metrics.StartTime = st.AsTime()
		}
	}
	// The primary output is the first, valid or not: tests list test.xml
	// first whether or not they wrote it.
	if outputs := s.GetOutputs(); len(outputs) > 0 {
		switch t := outputs[0].Type.(type) {
		case *spawnpb.ExecLogEntry_Output_OutputId:
			if id, ok := l.entryPath(t.OutputId); ok {
				sp.PrimaryOutput = l.paths[id]
			}
		case *spawnpb.ExecLogEntry_Output_InvalidOutputPath:
			sp.PrimaryOutput = t.InvalidOutputPath
		}
	}
	l.Spawns = append(l.Spawns, sp)
}

// FillMissingDigests gives spawns without an action digest, such as local
// executions without a remote cache, a digest over their command and input
// groups so that their changes are tracked. Its size is 0, which no real
// action digest has.
func (l *Log) FillMissingDigests() {
	for i := range l.Spawns {
		if l.Spawns[i].Digest.Hash != "" {
			continue
		}
		groups := l.Groups(i)
		names := make([]string, 0, len(groups))
		for name := range groups {
			names = append(names, name)
		}
		sort.Strings(names)
		h := sha256.New()
		for _, name := range names {
			h.Write([]byte(name))
			h.Write([]byte{0})
			h.Write([]byte(groups[name]))
			h.Write([]byte{0})
		}
		l.Spawns[i].Digest = Digest{Hash: hex.EncodeToString(h.Sum(nil))}
	}
}

func commandHash(s *spawnpb.ExecLogEntry_Spawn) uint64 {
	parts := make([]string, 0, len(s.GetArgs())+2*len(s.GetEnvVars())+2*len(s.GetPlatform().GetProperties())+3)
	parts = append(parts, strconv.Itoa(len(s.GetArgs())))
	parts = append(parts, s.GetArgs()...)
	parts = append(parts, strconv.Itoa(len(s.GetEnvVars())))
	for _, e := range s.GetEnvVars() {
		parts = append(parts, e.GetName(), e.GetValue())
	}
	parts = append(parts, strconv.Itoa(len(s.GetPlatform().GetProperties())))
	for _, p := range s.GetPlatform().GetProperties() {
		parts = append(parts, p.GetName(), p.GetValue())
	}
	return itemHash(parts...)
}
