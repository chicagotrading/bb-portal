package execlog_test

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/buildbarn/bb-portal/pkg/execlog"
)

// TestFixture parses the log named by EXECLOG_FIXTURE and reports timings.
func TestFixture(t *testing.T) {
	p := os.Getenv("EXECLOG_FIXTURE")
	if p == "" {
		t.Skip("EXECLOG_FIXTURE not set")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	l, err := execlog.ParseCompressed(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	parsed := time.Since(start)
	hits, groups := 0, 0
	for i := range l.Spawns {
		if l.Spawns[i].CacheHit {
			hits++
		}
		groups += len(l.Groups(i))
	}
	total := time.Since(start)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("entries=%d bytes=%d spawns=%d hits=%d groups=%d parse=%s parse+groups(all)=%s heapSys=%dMiB",
		l.Entries, l.Bytes, len(l.Spawns), hits, groups, parsed, total, m.HeapSys>>20)
	if os.Getenv("EXECLOG_DUMP") != "" {
		for i := 0; i < len(l.Spawns); i += len(l.Spawns)/20 + 1 {
			s := l.Spawns[i]
			t.Logf("%s %s %s %s %v", s.TargetLabel, s.Mnemonic, s.PrimaryOutput, s.Runner, l.Groups(i))
		}
	}
}
