package execlogingest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	spawnpb "github.com/bazelbuild/bazel/src/main/protobuf"
	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-portal/ent/gen/ent"
	"github.com/buildbarn/bb-portal/ent/gen/ent/actionexecution"
	"github.com/buildbarn/bb-portal/ent/gen/ent/execlogsummary"
	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/internal/database"
	"github.com/buildbarn/bb-portal/internal/database/buildeventrecorder"
	"github.com/buildbarn/bb-portal/internal/database/dbauthservice"
	"github.com/buildbarn/bb-portal/internal/database/embedded"
	"github.com/buildbarn/bb-portal/internal/database/execlogingest"
	"github.com/buildbarn/bb-portal/internal/mock"
	"github.com/buildbarn/bb-portal/pkg/execlog/execlogtest"
	"github.com/buildbarn/bb-portal/pkg/invocation/files"
	"github.com/buildbarn/bb-portal/pkg/proto/configuration/bb_portal"
	"github.com/buildbarn/bb-portal/test/testutils"
	"github.com/buildbarn/bb-storage/pkg/blobstore"
	"github.com/buildbarn/bb-storage/pkg/blobstore/chunk"
	"github.com/buildbarn/bb-storage/pkg/cas"
	"github.com/buildbarn/bb-storage/pkg/clock"
	"github.com/buildbarn/bb-storage/pkg/digest"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

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

// fakeCAS is a Chunk Storage and Chunk Mapping Storage. Blobs of at
// least twice minChunk bytes are split into minChunk byte chunks.
type fakeCAS struct {
	minChunk int
	chunks   map[string][]byte
	mappings map[string]chunk.Mapping
	// Fails every read if set.
	err error
	// Fails reads of individual chunks.
	chunkErrs  map[string]error
	chunkReads int
}

func newFakeCAS(minChunk int) *fakeCAS {
	return &fakeCAS{
		minChunk:  minChunk,
		chunks:    map[string][]byte{},
		mappings:  map[string]chunk.Mapping{},
		chunkErrs: map[string]error{},
	}
}

func (c *fakeCAS) reader() cas.StreamReader {
	return execlogingest.NewCASReader(fakeChunkStorage{fakeCAS: c}, fakeChunkMappingStorage{fakeCAS: c}, c)
}

func blobDigest(t *testing.T, b []byte) digest.Digest {
	g := digest.MustNewFunction("main", remoteexecution.DigestFunction_SHA256).NewGenerator(int64(len(b)))
	_, err := g.Write(b)
	require.NoError(t, err)
	return g.Sum()
}

// put stores a blob and returns its chunk digests in order.
func (c *fakeCAS) put(t *testing.T, blob []byte) []digest.Digest {
	d := blobDigest(t, blob)
	if len(blob) < 2*c.minChunk {
		c.chunks[d.GetHashString()] = blob
		return []digest.Digest{d}
	}
	var chunkDigests []digest.Digest
	for off := 0; off < len(blob); off += c.minChunk {
		b := blob[off:min(off+c.minChunk, len(blob))]
		cd := blobDigest(t, b)
		c.chunks[cd.GetHashString()] = b
		chunkDigests = append(chunkDigests, cd)
	}
	m, err := chunk.NewMappingFromDigests(chunkDigests, uint64(len(blob)), true)
	require.NoError(t, err)
	c.mappings[d.GetHashString()] = m
	return chunkDigests
}

func (c *fakeCAS) FetchCDCParameters(ctx context.Context, instanceName digest.InstanceName) (*remoteexecution.RepMaxCdcParams, error) {
	return &remoteexecution.RepMaxCdcParams{MinChunkSizeBytes: uint64(c.minChunk), HorizonSizeBytes: uint64(4 * c.minChunk)}, nil
}

type fakeChunkStorage struct {
	blobstore.BlobAccess[*chunk.Chunk]
	*fakeCAS
}

func (c fakeChunkStorage) Get(ctx context.Context, d digest.Digest) (*chunk.Chunk, error) {
	if c.err != nil {
		return nil, c.err
	}
	if err := c.chunkErrs[d.GetHashString()]; err != nil {
		return nil, err
	}
	b, ok := c.chunks[d.GetHashString()]
	if !ok {
		return nil, status.Error(codes.NotFound, "Chunk not found")
	}
	c.chunkReads++
	return chunk.NewChunk(b), nil
}

type fakeChunkMappingStorage struct {
	blobstore.BlobAccess[chunk.Mapping]
	*fakeCAS
}

func (c fakeChunkMappingStorage) Get(ctx context.Context, d digest.Digest) (chunk.Mapping, error) {
	if c.err != nil {
		return chunk.Mapping{}, c.err
	}
	m, ok := c.mappings[d.GetHashString()]
	if !ok {
		return chunk.Mapping{}, status.Error(codes.NotFound, "Chunk mapping not found")
	}
	return m, nil
}

// buildLog has an executed generator and a cache hit consuming its output.
func buildLog(t *testing.T, header, compileDigest string) []byte {
	b := &execlogtest.Builder{}
	b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Invocation_{Invocation: &spawnpb.ExecLogEntry_Invocation{HashFunctionName: "SHA-256"}}})
	src := b.File("lib/gen.py", "aa")
	hdr := b.File("bazel-out/k8-opt/bin/lib/gen.h", header)
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		TargetLabel: "//lib:gen", Mnemonic: "Genrule", Runner: "remote",
		InputSetId: b.Set([]uint32{src}), Digest: execlogtest.Digest("1111"),
		Metrics: &spawnpb.SpawnMetrics{TotalTime: durationpb.New(2 * time.Second), QueueTime: durationpb.New(time.Second)},
	}, hdr)
	obj := b.File("bazel-out/k8-opt/bin/app/main.o", "cc")
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		TargetLabel: "//app:main", Mnemonic: "CppCompile", Runner: "remote cache hit", CacheHit: true,
		InputSetId: b.Set([]uint32{b.File("app/main.cc", "dd"), hdr}), Digest: execlogtest.Digest(compileDigest),
	}, obj)
	raw, err := b.Bytes()
	require.NoError(t, err)
	compressed, err := execlogtest.Compress(raw)
	require.NoError(t, err)
	return compressed
}

func addInvocation(ctx context.Context, t *testing.T, db database.Client, instance *ent.InstanceName, storage *fakeCAS, blob []byte) *ent.BazelInvocation {
	sum := sha256.Sum256(blob)
	storage.put(t, blob)
	fileID, err := buildeventrecorder.SaveSingleFile(ctx, db, instance.ID, files.ParsedBepFile{
		Path:           "execution_log.binpb.zst",
		InstanceName:   "main",
		DigestFunction: 1,
		Hash:           sum[:],
		SizeBytes:      int64(len(blob)),
	})
	require.NoError(t, err)
	return testutils.StartCreateInvocation(db.Ent(), instance).
		SetBepCompleted(true).
		SetExecLogID(fileID).
		SaveX(ctx)
}

func TestIngest(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	storage := newFakeCAS(16)
	svc, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, &bb_portal.ExecLogIngestService{
		PollInterval: durationpb.New(time.Second),
	}, 0, noop.NewTracerProvider())
	require.NoError(t, err)

	instance := testutils.CreateInstanceName(ctx, t, client, "main")
	inv1 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h1", "2222"))
	inv2 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h2", "3333"))
	// Not completed yet.
	pending := testutils.StartCreateInvocation(client, instance).SetExecLogID(inv1.ExecLogID).SaveX(ctx)
	// Missing from the CAS.
	missingFile, err := buildeventrecorder.SaveSingleFile(ctx, db, instance.ID, files.ParsedBepFile{Path: "execution_log.binpb.zst", InstanceName: "main", DigestFunction: 1, Hash: make([]byte, 32), SizeBytes: 5})
	require.NoError(t, err)
	missing := testutils.StartCreateInvocation(client, instance).SetBepCompleted(true).SetExecLogID(missingFile).SaveX(ctx)

	processed, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, processed)

	s1 := client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv1.ID)).OnlyX(ctx)
	require.Equal(t, execlogsummary.StatusDONE, s1.Status)
	require.EqualValues(t, 2, s1.SpawnCount)
	require.EqualValues(t, 1, s1.CacheHits)
	require.EqualValues(t, 1, s1.ExecutedCount)
	require.Equal(t, "main", s1.Rev2InstanceName)
	require.EqualValues(t, 1, s1.DigestFunction)

	failed := client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(missing.ID)).OnlyX(ctx)
	require.Equal(t, execlogsummary.StatusFAILED, failed.Status)
	require.Contains(t, failed.Error, "Chunk not found")
	require.False(t, client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(pending.ID)).ExistX(ctx))

	// Two keys; only the compile has a new version.
	require.Equal(t, 2, client.ActionKey.Query().CountX(ctx))
	require.Equal(t, 3, client.ActionVersion.Query().CountX(ctx))

	s2 := client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv2.ID)).OnlyX(ctx)
	p1, err := actioninsights.DecodeSpawnIndex(s1.SpawnIndex)
	require.NoError(t, err)
	p2, err := actioninsights.DecodeSpawnIndex(s2.SpawnIndex)
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.Len(t, p2, 2)
	// Same keys; only the compile has a new version.
	require.Equal(t, p1[0].KeyID, p2[0].KeyID)
	require.Equal(t, p1[1].KeyID, p2[1].KeyID)
	var prev, next *ent.ActionVersion
	for i := range p1 {
		if p1[i].VersionID != p2[i].VersionID {
			require.Nil(t, prev)
			prev = client.ActionVersion.GetX(ctx, p1[i].VersionID)
			next = client.ActionVersion.GetX(ctx, p2[i].VersionID)
		}
	}
	require.NotNil(t, prev)
	require.Equal(t, prev.Groups["cmd"], next.Groups["cmd"])
	require.Equal(t, prev.Groups["src:app/main.cc"], next.Groups["src:app/main.cc"])
	require.NotEqual(t, prev.Groups["//lib (k8-opt)"], next.Groups["//lib (k8-opt)"])
	require.Len(t, next.Groups, 3)

	execs := client.ActionExecution.Query().Where(actionexecution.BazelInvocationID(inv1.ID)).AllX(ctx)
	require.Len(t, execs, 1)
	require.EqualValues(t, 2000, execs[0].TotalMs)
	require.EqualValues(t, 1000, execs[0].QueueMs)
	require.Equal(t, "remote", execs[0].Runner)

	// Nothing left to do, and completing the pending invocation picks it up.
	processed, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, processed)
	client.BazelInvocation.UpdateOne(pending).SetBepCompleted(true).ExecX(ctx)
	processed, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, 3, client.ActionVersion.Query().CountX(ctx))

	// Deleting invocations cascades to their summaries and executions.
	client.BazelInvocation.DeleteOne(inv1).ExecX(ctx)
	require.Equal(t, 3, client.ExecLogSummary.Query().CountX(ctx))
	require.Equal(t, 2, client.ActionExecution.Query().CountX(ctx))
}

func TestCollectGarbage(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	ctx = dbauthservice.NewContextWithDbAuthServiceBypass(ctx)
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	storage := newFakeCAS(16)
	now := time.Unix(1_000_000, 0)
	clk := mock.NewMockClock(ctrl)
	clk.EXPECT().Now().DoAndReturn(func() time.Time { return now }).AnyTimes()
	// The shortest retention allowed for a 1 s invocation retention.
	retention := execlogingest.MinimumVersionRetention(time.Second)
	require.Equal(t, 48*time.Hour+time.Second, retention)
	svc, err := execlogingest.NewService(db, storage.reader(), clk, &bb_portal.ExecLogIngestService{
		PollInterval:     durationpb.New(time.Second),
		VersionRetention: durationpb.New(50 * time.Hour),
	}, time.Second, noop.NewTracerProvider())
	require.NoError(t, err)

	instance := testutils.CreateInstanceName(ctx, t, client, "main")
	inv1 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h1", "2222"))
	_, err = svc.RunOnce(ctx)
	require.NoError(t, err)

	// Seen again a day later: the shared versions are touched.
	now = now.Add(25 * time.Hour)
	addInvocation(ctx, t, db, instance, storage, buildLog(t, "h2", "3333"))
	_, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, client.ActionVersion.Query().CountX(ctx))

	// Only the first compile version is past retention and unreferenced.
	now = now.Add(26 * time.Hour)
	client.BazelInvocation.DeleteOne(inv1).ExecX(ctx)
	_, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, client.ActionVersion.Query().CountX(ctx))
	require.Equal(t, 2, client.ActionKey.Query().CountX(ctx))

	// Everything expires once no invocation references it.
	now = now.Add(72 * time.Hour)
	client.BazelInvocation.Delete().ExecX(ctx)
	_, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, client.ActionVersion.Query().CountX(ctx))
	require.Equal(t, 0, client.ActionKey.Query().CountX(ctx))
}

// TestIngestFixture times a cold and a warm ingest of EXECLOG_FIXTURE.
func TestIngestFixture(t *testing.T) {
	p := os.Getenv("EXECLOG_FIXTURE")
	if p == "" {
		t.Skip("EXECLOG_FIXTURE not set")
	}
	blob, err := os.ReadFile(p)
	require.NoError(t, err)
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	db := testutils.SetupTestDB(t, dbProvider)
	storage := newFakeCAS(16)
	svc, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, &bb_portal.ExecLogIngestService{
		PollInterval: durationpb.New(time.Second),
	}, 0, noop.NewTracerProvider())
	require.NoError(t, err)
	instance := testutils.CreateInstanceName(ctx, t, db.Ent(), "main")
	for _, run := range []string{"cold", "warm"} {
		inv := addInvocation(ctx, t, db, instance, storage, blob)
		start := time.Now()
		processed, err := svc.RunOnce(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, processed)
		s := db.Ent().ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv.ID)).OnlyX(ctx)
		require.Equal(t, execlogsummary.StatusDONE, s.Status, s.Error)
		t.Logf("%s: total=%s parse=%dms spawns=%d executed=%d index=%dB versions=%d",
			run, time.Since(start), s.ParseMs, s.SpawnCount, s.ExecutedCount, len(s.SpawnIndex), db.Ent().ActionVersion.Query().CountX(ctx))
	}
}

func TestNewServiceVersionRetention(t *testing.T) {
	db := testutils.SetupTestDB(t, dbProvider)
	storage := newFakeCAS(16)
	day := 24 * time.Hour
	for _, tc := range []struct {
		name                string
		version, invocation time.Duration
		ok                  bool
	}{
		{"NoCollection", 0, 0, true},
		{"InvocationsKeptForever", 10 * day, 0, false},
		{"TooShort", 8 * day, 7 * day, false},
		{"Minimum", 9 * day, 7 * day, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, &bb_portal.ExecLogIngestService{
				PollInterval:     durationpb.New(time.Second),
				VersionRetention: durationpb.New(tc.version),
			}, tc.invocation, noop.NewTracerProvider())
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Equal(t, codes.InvalidArgument, status.Code(err), err)
			}
		})
	}
}

// TestIngestRetry checks that storage errors leave logs pending.
func TestIngestRetry(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	ctx = dbauthservice.NewContextWithDbAuthServiceBypass(ctx)
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	storage := newFakeCAS(16)
	now := time.Unix(1_000_000, 0)
	clk := mock.NewMockClock(ctrl)
	clk.EXPECT().Now().DoAndReturn(func() time.Time { return now }).AnyTimes()
	svc, err := execlogingest.NewService(db, storage.reader(), clk, &bb_portal.ExecLogIngestService{
		PollInterval: durationpb.New(time.Second),
	}, 0, noop.NewTracerProvider())
	require.NoError(t, err)
	instance := testutils.CreateInstanceName(ctx, t, client, "main")
	summary := func(inv *ent.BazelInvocation) *ent.ExecLogSummary {
		return client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv.ID)).OnlyX(ctx)
	}

	// The storage is down: the first log stays pending and the others
	// are not attempted.
	inv1 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h1", "2222"))
	inv2 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h2", "3333"))
	storage.err = status.Error(codes.Unavailable, "Storage is restarting")
	processed, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	s1 := summary(inv1)
	require.Equal(t, execlogsummary.StatusPENDING, s1.Status)
	require.Contains(t, s1.Error, "Storage is restarting")
	require.EqualValues(t, 1, s1.Attempts)
	require.False(t, client.ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv2.ID)).ExistX(ctx))

	// Once it is back, untried logs go first and the failed one is
	// retried after the claim went stale.
	storage.err = nil
	processed, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, execlogsummary.StatusDONE, summary(inv2).Status)
	require.Equal(t, execlogsummary.StatusPENDING, summary(inv1).Status)
	now = now.Add(31 * time.Minute)
	processed, err = svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	s1 = summary(inv1)
	require.Equal(t, execlogsummary.StatusDONE, s1.Status)
	require.Empty(t, s1.Error)
	require.EqualValues(t, 2, s1.Attempts)

	// A log that keeps failing is abandoned with its last error.
	inv3 := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h3", "4444"))
	storage.err = status.Error(codes.Unavailable, "Storage is restarting")
	for range 3 {
		_, err = svc.RunOnce(ctx)
		require.NoError(t, err)
		now = now.Add(31 * time.Minute)
	}
	s3 := summary(inv3)
	require.Equal(t, execlogsummary.StatusFAILED, s3.Status)
	require.Contains(t, s3.Error, "Abandoned after repeated attempts: ")
	require.Contains(t, s3.Error, "Storage is restarting")
}

func TestIngestSizeLimits(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	for _, tc := range []struct {
		name   string
		config *bb_portal.ExecLogIngestService
		want   string
	}{
		{"Compressed", &bb_portal.ExecLogIngestService{MaximumLogSizeBytes: 10}, "above the limit of 10"},
		{"Decompressed", &bb_portal.ExecLogIngestService{MaximumDecompressedLogSizeBytes: 10}, "decompresses to more than the limit of 10 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB(t, dbProvider)
			storage := newFakeCAS(16)
			tc.config.PollInterval = durationpb.New(time.Second)
			svc, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, tc.config, 0, noop.NewTracerProvider())
			require.NoError(t, err)
			instance := testutils.CreateInstanceName(ctx, t, db.Ent(), "main")
			inv := addInvocation(ctx, t, db, instance, storage, buildLog(t, "h1", "2222"))
			_, err = svc.RunOnce(ctx)
			require.NoError(t, err)
			s := db.Ent().ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv.ID)).OnlyX(ctx)
			require.Equal(t, execlogsummary.StatusFAILED, s.Status)
			require.Contains(t, s.Error, tc.want)
		})
	}
}

// localLog has a local test run without an action digest.
func localLog(t *testing.T, data string) []byte {
	b := &execlogtest.Builder{}
	b.Add(&spawnpb.ExecLogEntry{Type: &spawnpb.ExecLogEntry_Invocation_{Invocation: &spawnpb.ExecLogEntry_Invocation{HashFunctionName: "SHA-256"}}})
	b.Spawn(&spawnpb.ExecLogEntry_Spawn{
		Args: []string{"test-setup.sh"}, TargetLabel: "//app:test", Mnemonic: "TestRunner", Runner: "local",
		InputSetId: b.Set([]uint32{b.File("app/data.txt", data)}),
		Metrics:    &spawnpb.SpawnMetrics{TotalTime: durationpb.New(time.Second)},
	}, b.File("bazel-out/k8-opt/testlogs/app/test/test.log", "tl"))
	raw, err := b.Bytes()
	require.NoError(t, err)
	compressed, err := execlogtest.Compress(raw)
	require.NoError(t, err)
	return compressed
}

// TestIngestWithoutDigest checks that spawns without an action digest get
// a version per distinct input.
func TestIngestWithoutDigest(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	db := testutils.SetupTestDB(t, dbProvider)
	client := db.Ent()
	storage := newFakeCAS(16)
	svc, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, &bb_portal.ExecLogIngestService{
		PollInterval: durationpb.New(time.Second),
	}, 0, noop.NewTracerProvider())
	require.NoError(t, err)
	instance := testutils.CreateInstanceName(ctx, t, client, "main")
	for _, data := range []string{"d1", "d1", "d2"} {
		addInvocation(ctx, t, db, instance, storage, localLog(t, data))
	}
	processed, err := svc.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, processed)
	versions := client.ActionVersion.Query().AllX(ctx)
	require.Len(t, versions, 2)
	for _, v := range versions {
		require.Len(t, v.DigestHash, 32)
		require.Zero(t, v.DigestSizeBytes)
	}
	require.Equal(t, 3, client.ActionExecution.Query().CountX(ctx))
}

// TestIngestChunkedStorage checks reading logs stored as one chunk and as
// a chunk mapping, and failures to read either.
func TestIngestChunkedStorage(t *testing.T) {
	ctx := dbauthservice.NewContextWithDbAuthServiceBypass(context.Background())
	for _, tc := range []struct {
		name     string
		minChunk int
		// Breaks the stored blob, given its chunk digests.
		breakBlob  func(storage *fakeCAS, chunks []digest.Digest)
		wantStatus execlogsummary.Status
		wantError  string
	}{
		{name: "SingleChunk", minChunk: 1 << 20, wantStatus: execlogsummary.StatusDONE},
		{name: "MultipleChunks", minChunk: 16, wantStatus: execlogsummary.StatusDONE},
		{
			name:     "MissingSingleChunk",
			minChunk: 1 << 20,
			breakBlob: func(storage *fakeCAS, chunks []digest.Digest) {
				delete(storage.chunks, chunks[0].GetHashString())
			},
			wantStatus: execlogsummary.StatusFAILED,
			wantError:  "Chunk not found",
		},
		{
			name:     "MissingMapping",
			minChunk: 16,
			breakBlob: func(storage *fakeCAS, chunks []digest.Digest) {
				clear(storage.mappings)
			},
			wantStatus: execlogsummary.StatusFAILED,
			wantError:  "Chunk mapping not found",
		},
		{
			name:     "MissingChunk",
			minChunk: 16,
			breakBlob: func(storage *fakeCAS, chunks []digest.Digest) {
				delete(storage.chunks, chunks[len(chunks)/2].GetHashString())
			},
			wantStatus: execlogsummary.StatusFAILED,
			wantError:  "Chunk not found",
		},
		{
			name:     "UnavailableChunk",
			minChunk: 16,
			breakBlob: func(storage *fakeCAS, chunks []digest.Digest) {
				storage.chunkErrs[chunks[len(chunks)-1].GetHashString()] = status.Error(codes.Unavailable, "Shard is restarting")
			},
			wantStatus: execlogsummary.StatusPENDING,
			wantError:  "Shard is restarting",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB(t, dbProvider)
			storage := newFakeCAS(tc.minChunk)
			svc, err := execlogingest.NewService(db, storage.reader(), clock.SystemClock, &bb_portal.ExecLogIngestService{
				PollInterval: durationpb.New(time.Second),
			}, 0, noop.NewTracerProvider())
			require.NoError(t, err)
			instance := testutils.CreateInstanceName(ctx, t, db.Ent(), "main")
			blob := buildLog(t, "h1", "2222")
			inv := addInvocation(ctx, t, db, instance, storage, blob)
			chunks := storage.put(t, blob)
			if tc.minChunk == 16 {
				require.Greater(t, len(chunks), 2)
			} else {
				require.Len(t, chunks, 1)
			}
			if tc.breakBlob != nil {
				tc.breakBlob(storage, chunks)
			}

			processed, err := svc.RunOnce(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			s := db.Ent().ExecLogSummary.Query().Where(execlogsummary.BazelInvocationID(inv.ID)).OnlyX(ctx)
			require.Equal(t, tc.wantStatus, s.Status)
			if tc.wantError == "" {
				require.Empty(t, s.Error)
				require.EqualValues(t, 2, s.SpawnCount)
				require.Equal(t, len(chunks), storage.chunkReads)
			} else {
				require.Contains(t, s.Error, tc.wantError)
			}
		})
	}
}
