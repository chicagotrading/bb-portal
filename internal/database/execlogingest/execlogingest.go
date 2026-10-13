// Package execlogingest ingests compact execution logs.
package execlogingest

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	remoteexecution "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-portal/ent/gen/ent"
	"github.com/buildbarn/bb-portal/ent/gen/ent/actionexecution"
	"github.com/buildbarn/bb-portal/ent/gen/ent/execlogsummary"
	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/buildbarn/bb-portal/internal/database"
	"github.com/buildbarn/bb-portal/internal/database/dbauthservice"
	"github.com/buildbarn/bb-portal/internal/database/sqlc"
	"github.com/buildbarn/bb-portal/pkg/execlog"
	"github.com/buildbarn/bb-portal/pkg/proto/configuration/bb_portal"
	"github.com/buildbarn/bb-storage/pkg/blobstore"
	"github.com/buildbarn/bb-storage/pkg/blobstore/chunk"
	"github.com/buildbarn/bb-storage/pkg/capabilities"
	"github.com/buildbarn/bb-storage/pkg/cas"
	"github.com/buildbarn/bb-storage/pkg/clock"
	"github.com/buildbarn/bb-storage/pkg/digest"
	"github.com/buildbarn/bb-storage/pkg/program"
	"github.com/buildbarn/bb-storage/pkg/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpc_codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// A PENDING claim older than this is assumed to have died.
	staleClaim = 30 * time.Minute
	// Bounds retries of a log that crashes the process.
	maxAttempts = 2
	// Rows per statement for batched SQL.
	chunkSize = 5000
	// Bound on stored error text.
	maxErrorLength = 2000
	// How often to garbage collect unused versions and keys.
	gcInterval = time.Hour
	// last_seen_at is only bumped once it is older than this.
	touchInterval = 24 * time.Hour
	// Allowance for the database cleanup running late, on top of the
	// invocation retention and touchInterval.
	cleanupLag = 24 * time.Hour
)

// Service ingests one execution log at a time.
type Service struct {
	db                   database.Client
	casReader            cas.StreamReader
	clock                clock.Clock
	pollInterval         time.Duration
	versionRetention     time.Duration
	maxLogSizeBytes      int64
	maxDecompressedBytes int64
	tracer               trace.Tracer
	lastGC               time.Time
}

// MinimumVersionRetention is the shortest version retention that keeps
// every version referenced by an invocation kept for invocationRetention.
// A version's last_seen_at lags by up to touchInterval.
func MinimumVersionRetention(invocationRetention time.Duration) time.Duration {
	return invocationRetention + touchInterval + cleanupLag
}

// NewCASReader streams blobs out of a Chunk Storage (CS) and Chunk
// Mapping Storage (CMS). It bypasses any chunk cache, so that large logs
// do not evict it.
func NewCASReader(chunkStorage blobstore.BlobAccess[*chunk.Chunk], chunkMappingStorage blobstore.BlobAccess[chunk.Mapping], cdcParametersFetcher capabilities.CDCParametersFetcher) cas.StreamReader {
	return cas.NewStorageBackedStreamReader(
		cas.NewChunkBytesReader(chunkStorage),
		blobstore.NewBlobAccessMappingFetcher(chunkMappingStorage),
		cdcParametersFetcher,
	)
}

// NewService creates a Service streaming logs out of the chunked CAS.
// invocationRetention is how long the database cleanup keeps invocations,
// or zero if it never deletes them.
func NewService(
	db database.Client,
	casReader cas.StreamReader,
	clock clock.Clock,
	configuration *bb_portal.ExecLogIngestService,
	invocationRetention time.Duration,
	tracerProvider trace.TracerProvider,
) (*Service, error) {
	if db == nil {
		return nil, status.Error(grpc_codes.InvalidArgument, "Execution log ingestion requires a database")
	}
	if casReader == nil {
		return nil, status.Error(grpc_codes.InvalidArgument, "Execution log ingestion requires a Content Addressable Storage")
	}
	if err := configuration.GetPollInterval().CheckValid(); err != nil {
		return nil, util.StatusWrap(err, "Failed to parse pollInterval")
	}
	if vr := configuration.GetVersionRetention(); vr != nil {
		if err := vr.CheckValid(); err != nil {
			return nil, util.StatusWrap(err, "Failed to parse versionRetention")
		}
	}
	versionRetention := configuration.GetVersionRetention().AsDuration()
	if versionRetention > 0 {
		// Invocations refer to versions only through their spawn index,
		// so collecting a version still in use corrupts their churn.
		if invocationRetention <= 0 {
			return nil, status.Error(grpc_codes.InvalidArgument, "versionRetention requires the database cleanup configuration's invocationRetention, as versions of invocations kept forever must be kept too")
		}
		if minimum := MinimumVersionRetention(invocationRetention); versionRetention < minimum {
			return nil, status.Errorf(grpc_codes.InvalidArgument, "versionRetention is %s, but must be at least %s: the invocation retention of %s plus %s", versionRetention, minimum, invocationRetention, minimum-invocationRetention)
		}
	}
	return &Service{
		db:                   db,
		casReader:            casReader,
		clock:                clock,
		pollInterval:         configuration.GetPollInterval().AsDuration(),
		versionRetention:     versionRetention,
		maxLogSizeBytes:      configuration.GetMaximumLogSizeBytes(),
		maxDecompressedBytes: configuration.GetMaximumDecompressedLogSizeBytes(),
		tracer:               tracerProvider.Tracer("github.com/buildbarn/bb-portal/internal/database/execlogingest"),
	}, nil
}

// Start runs the ingestion loop in the group.
func (s *Service) Start(group program.Group) {
	group.Go(func(ctx context.Context, siblingsGroup, dependenciesGroup program.Group) error {
		ctx = dbauthservice.NewContextWithDbAuthServiceBypass(ctx)
		for {
			if _, err := s.RunOnce(ctx); err != nil {
				slog.WarnContext(ctx, "Execution log ingestion failed", "err", err)
			}
			jitter := time.Duration((rand.Float64()*0.1 - 0.05) * float64(s.pollInterval))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(s.pollInterval + jitter):
			}
		}
	})
}

// RunOnce ingests pending logs one at a time and returns the count.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	now := s.clock.Now()
	staleBefore := now.Add(-staleClaim)
	if _, err := s.db.Sqlc().FailAbandonedExecLogSummaries(ctx, sqlc.FailAbandonedExecLogSummariesParams{
		Now:         now,
		StaleBefore: staleBefore,
		MaxAttempts: maxAttempts,
	}); err != nil {
		return 0, util.StatusWrap(err, "Failed to fail abandoned execution logs")
	}
	processed := 0
	for ctx.Err() == nil {
		rows, err := s.db.Sqlc().FindExecLogsToIngest(ctx, sqlc.FindExecLogsToIngestParams{
			StaleBefore: s.clock.Now().Add(-staleClaim),
			MaxAttempts: maxAttempts,
			BatchLimit:  1,
		})
		if err != nil {
			return processed, util.StatusWrap(err, "Failed to find execution logs to ingest")
		}
		if len(rows) == 0 {
			break
		}
		claim, err := s.db.Sqlc().ClaimExecLogSummary(ctx, sqlc.ClaimExecLogSummaryParams{
			BazelInvocationID: rows[0].BazelInvocationID,
			Now:               s.clock.Now(),
			StaleBefore:       s.clock.Now().Add(-staleClaim),
			MaxAttempts:       maxAttempts,
		})
		if errors.Is(err, sql.ErrNoRows) {
			// Claimed by another replica in the meantime.
			continue
		}
		if err != nil {
			return processed, util.StatusWrap(err, "Failed to claim execution log")
		}
		retry := s.ingestAndRecord(ctx, claim.ID, rows[0])
		processed++
		if retry {
			// The storage is likely unavailable; stop instead of using
			// up the attempts of every pending log.
			break
		}
	}
	if s.versionRetention > 0 && s.clock.Now().Sub(s.lastGC) >= gcInterval {
		s.lastGC = s.clock.Now()
		if err := s.collectGarbage(ctx); err != nil {
			return processed, err
		}
	}
	return processed, nil
}

// isRetryable reports whether an error is likely transient, such as the
// storage restarting, so the log should be ingested again later.
func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case grpc_codes.Unavailable, grpc_codes.DeadlineExceeded, grpc_codes.ResourceExhausted, grpc_codes.Aborted, grpc_codes.Canceled:
		return true
	}
	return false
}

// ingestAndRecord ingests a claimed log and records a failure. A retryable
// failure leaves the log PENDING, to be claimed again after staleClaim
// until it runs out of attempts, and returns true.
func (s *Service) ingestAndRecord(ctx context.Context, summaryID int64, row sqlc.FindExecLogsToIngestRow) bool {
	ctx, span := s.tracer.Start(ctx, "ExecLogIngest.Ingest", trace.WithAttributes(attribute.Int64("invocation_db_id", row.BazelInvocationID)))
	defer span.End()
	err := s.Ingest(ctx, summaryID, row)
	if err == nil {
		return false
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, "Failed to ingest execution log")
	retry := isRetryable(err)
	slog.WarnContext(ctx, "Failed to ingest execution log", "invocation_db_id", row.BazelInvocationID, "retry", retry, "err", err)
	msg := err.Error()
	if len(msg) > maxErrorLength {
		msg = msg[:maxErrorLength]
	}
	update := s.db.Ent().ExecLogSummary.UpdateOneID(summaryID).SetError(msg)
	if !retry {
		update.SetStatus(execlogsummary.StatusFAILED).SetFinishedAt(s.clock.Now())
	}
	if err := update.Exec(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to record execution log failure", "invocation_db_id", row.BazelInvocationID, "err", err)
	}
	return retry
}

func (s *Service) logDigest(row sqlc.FindExecLogsToIngestRow) (digest.Digest, error) {
	instanceName, err := digest.NewInstanceName(row.Rev2InstanceName)
	if err != nil {
		return digest.BadDigest, err
	}
	hash := hex.EncodeToString(row.Hash)
	fn, err := instanceName.GetDigestFunction(remoteexecution.DigestFunction_Value(row.DigestFunction), len(hash))
	if err != nil {
		return digest.BadDigest, err
	}
	return fn.NewDigest(hash, row.SizeBytes)
}

type keyTuple struct {
	label, mnemonic, output string
}

type versionTuple struct {
	keyID int64
	hash  string
	size  int64
}

// Ingest stores the keys, versions, executions and index of a log.
func (s *Service) Ingest(ctx context.Context, summaryID int64, row sqlc.FindExecLogsToIngestRow) error {
	// Size limits are not retryable, so they are not ResourceExhausted.
	if s.maxLogSizeBytes > 0 && row.SizeBytes > s.maxLogSizeBytes {
		return status.Errorf(grpc_codes.InvalidArgument, "Execution log is %d bytes, above the limit of %d", row.SizeBytes, s.maxLogSizeBytes)
	}
	d, err := s.logDigest(row)
	if err != nil {
		return util.StatusWrap(err, "Invalid execution log digest")
	}
	start := s.clock.Now()
	// Holds one chunk at a time and checks the blob digest at the end.
	r, err := s.casReader.ReadStream(ctx, d)
	if err != nil {
		return util.StatusWrap(err, "Failed to read execution log")
	}
	l, err := execlog.ParseCompressed(r, s.maxDecompressedBytes)
	if errors.Is(err, execlog.ErrTooLarge) {
		return status.Errorf(grpc_codes.InvalidArgument, "Execution log decompresses to more than the limit of %d bytes", s.maxDecompressedBytes)
	}
	if err != nil {
		return util.StatusWrap(err, "Failed to read execution log")
	}
	parseMs := s.clock.Now().Sub(start).Milliseconds()

	// Packages seen in earlier logs keep their sources apart from those
	// of parent packages, whether or not this log builds them.
	labels, err := s.db.Sqlc().ListActionKeyLabels(ctx, row.InstanceNameID)
	if err != nil {
		return util.StatusWrap(err, "Failed to list known targets")
	}
	l.AddKnownLabels(labels)
	l.FillMissingDigests()

	keyIDs, err := s.upsertKeys(ctx, row.InstanceNameID, l)
	if err != nil {
		return err
	}
	versionIDs, err := s.upsertVersions(ctx, l, keyIDs)
	if err != nil {
		return err
	}

	pairs := make([]actioninsights.Pair, 0, len(l.Spawns))
	var executions []execution
	for i := range l.Spawns {
		sp := &l.Spawns[i]
		keyID := keyIDs[keyTuple{sp.TargetLabel, sp.Mnemonic, sp.PrimaryOutput}]
		versionID := versionIDs[versionTuple{keyID, sp.Digest.Hash, sp.Digest.SizeBytes}]
		pairs = append(pairs, actioninsights.Pair{KeyID: keyID, VersionID: versionID})
		if !sp.CacheHit {
			executions = append(executions, execution{spawn: sp, keyID: keyID, versionID: versionID})
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.saveResult(ctx, tx.Ent(), summaryID, row, l, executions, parseMs, pairs); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type execution struct {
	spawn     *execlog.Spawn
	keyID     int64
	versionID int64
}

func (s *Service) saveResult(ctx context.Context, tx *ent.Client, summaryID int64, row sqlc.FindExecLogsToIngestRow, l *execlog.Log, executions []execution, parseMs int64, pairs []actioninsights.Pair) error {
	if _, err := tx.ActionExecution.Delete().Where(actionexecution.BazelInvocationID(row.BazelInvocationID)).Exec(ctx); err != nil {
		return util.StatusWrap(err, "Failed to delete previous executions")
	}
	var earliest time.Time
	for i := range l.Spawns {
		st := l.Spawns[i].Metrics.StartTime
		if !st.IsZero() && (earliest.IsZero() || st.Before(earliest)) {
			earliest = st
		}
	}
	const bulk = 1000
	for i := 0; i < len(executions); i += bulk {
		var creates []*ent.ActionExecutionCreate
		for _, e := range executions[i:min(i+bulk, len(executions))] {
			m := e.spawn.Metrics
			c := tx.ActionExecution.Create().
				SetBazelInvocationID(row.BazelInvocationID).
				SetActionKeyID(e.keyID).
				SetActionVersionID(e.versionID).
				SetRunner(e.spawn.Runner).
				SetExitCode(e.spawn.ExitCode).
				SetTotalMs(m.TotalMs).
				SetQueueMs(m.QueueMs).
				SetExecWallMs(m.ExecWallMs).
				SetFetchMs(m.FetchMs).
				SetUploadMs(m.UploadMs).
				SetInputBytes(m.InputBytes).
				SetInputFiles(m.InputFiles).
				SetMemoryPeakBytes(m.MemoryPeakBytes)
			if !m.StartTime.IsZero() {
				c.SetStartOffsetMs(m.StartTime.Sub(earliest).Milliseconds())
			}
			creates = append(creates, c)
		}
		if err := tx.ActionExecution.CreateBulk(creates...).Exec(ctx); err != nil {
			return util.StatusWrap(err, "Failed to save executions")
		}
	}
	if err := tx.ExecLogSummary.UpdateOneID(summaryID).
		SetStatus(execlogsummary.StatusDONE).
		ClearError().
		SetFinishedAt(s.clock.Now()).
		SetRev2InstanceName(row.Rev2InstanceName).
		SetDigestFunction(row.DigestFunction).
		SetSpawnCount(int64(len(l.Spawns))).
		SetCacheHits(int64(len(l.Spawns) - len(executions))).
		SetExecutedCount(int64(len(executions))).
		SetParseMs(parseMs).
		SetBytes(l.Bytes).
		SetSpawnIndex(actioninsights.EncodeSpawnIndex(pairs)).
		Exec(ctx); err != nil {
		return util.StatusWrap(err, "Failed to save execution log summary")
	}
	return nil
}

func (s *Service) upsertKeys(ctx context.Context, instanceNameID int64, l *execlog.Log) (map[keyTuple]int64, error) {
	unique := map[keyTuple]struct{}{}
	for i := range l.Spawns {
		sp := &l.Spawns[i]
		unique[keyTuple{sp.TargetLabel, sp.Mnemonic, sp.PrimaryOutput}] = struct{}{}
	}
	keys := make([]keyTuple, 0, len(unique))
	for k := range unique {
		keys = append(keys, k)
	}
	ids := make(map[keyTuple]int64, len(keys))
	for i := 0; i < len(keys); i += chunkSize {
		chunk := keys[i:min(i+chunkSize, len(keys))]
		labels := make([]string, len(chunk))
		mnemonics := make([]string, len(chunk))
		outputs := make([]string, len(chunk))
		for j, k := range chunk {
			labels[j], mnemonics[j], outputs[j] = k.label, k.mnemonic, k.output
		}
		if err := s.db.Sqlc().InsertMissingActionKeys(ctx, sqlc.InsertMissingActionKeysParams{
			InstanceNameID: instanceNameID,
			TargetLabels:   labels,
			Mnemonics:      mnemonics,
			PrimaryOutputs: outputs,
		}); err != nil {
			return nil, util.StatusWrap(err, "Failed to insert action keys")
		}
		rows, err := s.db.Sqlc().GetActionKeyIDs(ctx, sqlc.GetActionKeyIDsParams{
			InstanceNameID: instanceNameID,
			TargetLabels:   labels,
			Mnemonics:      mnemonics,
			PrimaryOutputs: outputs,
		})
		if err != nil {
			return nil, util.StatusWrap(err, "Failed to get action keys")
		}
		for _, r := range rows {
			ids[keyTuple{r.TargetLabel, r.Mnemonic, r.PrimaryOutput}] = r.ID
		}
	}
	if len(ids) != len(keys) {
		return nil, status.Errorf(grpc_codes.Internal, "Resolved %d of %d action keys", len(ids), len(keys))
	}
	return ids, nil
}

type versionChunk struct {
	keyIDs []int64
	hashes [][]byte
	sizes  []int64
	tuples []versionTuple
}

func (s *Service) lookupVersions(ctx context.Context, c versionChunk, ids map[versionTuple]int64) error {
	rows, err := s.db.Sqlc().GetActionVersionIDs(ctx, sqlc.GetActionVersionIDsParams{
		ActionKeyIds:    c.keyIDs,
		DigestHashes:    c.hashes,
		DigestSizeBytes: c.sizes,
	})
	if err != nil {
		return util.StatusWrap(err, "Failed to get action versions")
	}
	for _, r := range rows {
		ids[versionTuple{r.ActionKeyID, hex.EncodeToString(r.DigestHash), r.DigestSizeBytes}] = r.ID
	}
	return nil
}

// upsertVersions computes groups only for versions not seen before.
func (s *Service) upsertVersions(ctx context.Context, l *execlog.Log, keyIDs map[keyTuple]int64) (map[versionTuple]int64, error) {
	// First spawn of each version, used to compute its groups.
	first := map[versionTuple]int{}
	var all []versionTuple
	for i := range l.Spawns {
		sp := &l.Spawns[i]
		v := versionTuple{keyIDs[keyTuple{sp.TargetLabel, sp.Mnemonic, sp.PrimaryOutput}], sp.Digest.Hash, sp.Digest.SizeBytes}
		if _, ok := first[v]; !ok {
			first[v] = i
			all = append(all, v)
		}
	}
	chunks := func(vs []versionTuple) ([]versionChunk, error) {
		var out []versionChunk
		for i := 0; i < len(vs); i += chunkSize {
			var c versionChunk
			for _, v := range vs[i:min(i+chunkSize, len(vs))] {
				h, err := hex.DecodeString(v.hash)
				if err != nil {
					return nil, status.Errorf(grpc_codes.InvalidArgument, "Invalid action digest %#v", v.hash)
				}
				if h == nil {
					h = []byte{}
				}
				c.keyIDs = append(c.keyIDs, v.keyID)
				c.hashes = append(c.hashes, h)
				c.sizes = append(c.sizes, v.size)
				c.tuples = append(c.tuples, v)
			}
			out = append(out, c)
		}
		return out, nil
	}

	ids := make(map[versionTuple]int64, len(all))
	allChunks, err := chunks(all)
	if err != nil {
		return nil, err
	}
	for _, c := range allChunks {
		if err := s.lookupVersions(ctx, c, ids); err != nil {
			return nil, err
		}
	}
	now := s.clock.Now()
	existing := make([]int64, 0, len(ids))
	for _, id := range ids {
		existing = append(existing, id)
	}
	for i := 0; i < len(existing); i += chunkSize {
		if _, err := s.db.Sqlc().TouchActionVersions(ctx, sqlc.TouchActionVersionsParams{
			Now:         now,
			Ids:         existing[i:min(i+chunkSize, len(existing))],
			TouchBefore: now.Add(-touchInterval),
		}); err != nil {
			return nil, util.StatusWrap(err, "Failed to touch action versions")
		}
	}

	var missing []versionTuple
	for _, v := range all {
		if _, ok := ids[v]; !ok {
			missing = append(missing, v)
		}
	}
	missingChunks, err := chunks(missing)
	if err != nil {
		return nil, err
	}
	for _, c := range missingChunks {
		groups := make([]string, len(c.tuples))
		for j, v := range c.tuples {
			b, err := json.Marshal(l.Groups(first[v]))
			if err != nil {
				return nil, util.StatusWrap(err, "Failed to encode input groups")
			}
			groups[j] = string(b)
		}
		if err := s.db.Sqlc().InsertMissingActionVersions(ctx, sqlc.InsertMissingActionVersionsParams{
			Now:             now,
			ActionKeyIds:    c.keyIDs,
			DigestHashes:    c.hashes,
			DigestSizeBytes: c.sizes,
			Groups:          groups,
		}); err != nil {
			return nil, util.StatusWrap(err, "Failed to insert action versions")
		}
		if err := s.lookupVersions(ctx, c, ids); err != nil {
			return nil, err
		}
	}
	if len(ids) != len(all) {
		return nil, status.Errorf(grpc_codes.Internal, "Resolved %d of %d action versions", len(ids), len(all))
	}
	return ids, nil
}

func (s *Service) collectGarbage(ctx context.Context) error {
	seenBefore := s.clock.Now().Add(-s.versionRetention)
	for ctx.Err() == nil {
		n, err := s.db.Sqlc().DeleteUnusedActionVersions(ctx, sqlc.DeleteUnusedActionVersionsParams{SeenBefore: seenBefore, BatchLimit: chunkSize})
		if err != nil {
			return util.StatusWrap(err, "Failed to delete unused action versions")
		}
		if n < chunkSize {
			break
		}
	}
	for ctx.Err() == nil {
		n, err := s.db.Sqlc().DeleteUnusedActionKeys(ctx, chunkSize)
		if err != nil {
			return util.StatusWrap(err, "Failed to delete unused action keys")
		}
		if n < chunkSize {
			break
		}
	}
	return nil
}
