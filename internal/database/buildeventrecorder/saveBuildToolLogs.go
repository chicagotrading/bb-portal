package buildeventrecorder

import (
	"context"

	bes "github.com/bazelbuild/bazel/src/main/java/com/google/devtools/build/lib/buildeventstream/proto"
	"github.com/buildbarn/bb-portal/internal/database"
	"github.com/buildbarn/bb-portal/pkg/invocation/files"
	"github.com/buildbarn/bb-storage/pkg/util"
)

// execLogName is the build tool log of --execution_log_compact_file.
const execLogName = "execution_log.binpb.zst"

func getBuildToolLog(buildToolLogs *bes.BuildToolLogs, names ...string) *files.ParsedBepFile {
	for _, log := range buildToolLogs.GetLog() {
		for _, name := range names {
			if log.Name == name {
				return files.ParseBepFile(log)
			}
		}
	}
	return nil
}

func (r *buildEventRecorder) saveBuildToolLogs(ctx context.Context, tx database.Handle, buildToolLogs *bes.BuildToolLogs) error {
	if buildToolLogs == nil {
		return nil
	}
	update := tx.Ent().BazelInvocation.UpdateOneID(r.InvocationDbID)
	changed := false
	if profileFile := getBuildToolLog(buildToolLogs, "command.profile.gz", "command.profile.json"); profileFile != nil {
		fileDbID, err := SaveSingleFile(ctx, tx, r.InstanceNameDbID, *profileFile)
		if err != nil {
			return util.StatusWrap(err, "Failed to save profile file to database")
		}
		update.SetProfileID(fileDbID)
		changed = true
	}
	if execLogFile := getBuildToolLog(buildToolLogs, execLogName); execLogFile != nil {
		fileDbID, err := SaveSingleFile(ctx, tx, r.InstanceNameDbID, *execLogFile)
		if err != nil {
			return util.StatusWrap(err, "Failed to save execution log file to database")
		}
		update.SetExecLogID(fileDbID)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := update.Exec(ctx); err != nil {
		return util.StatusWrap(err, "Failed to add build tool logs to invocation")
	}
	return nil
}
