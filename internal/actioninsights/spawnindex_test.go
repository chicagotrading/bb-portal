package actioninsights_test

import (
	"testing"

	"github.com/buildbarn/bb-portal/internal/actioninsights"
	"github.com/stretchr/testify/require"
)

func TestSpawnIndexRoundTrip(t *testing.T) {
	pairs := []actioninsights.Pair{{KeyID: 300, VersionID: 9}, {KeyID: 5, VersionID: 1 << 40}, {KeyID: 300, VersionID: 9}, {KeyID: 7, VersionID: 2}}
	b := actioninsights.EncodeSpawnIndex(pairs)
	got, err := actioninsights.DecodeSpawnIndex(b)
	require.NoError(t, err)
	require.Equal(t, []actioninsights.Pair{{KeyID: 5, VersionID: 1 << 40}, {KeyID: 7, VersionID: 2}, {KeyID: 300, VersionID: 9}}, got)

	empty, err := actioninsights.DecodeSpawnIndex(actioninsights.EncodeSpawnIndex(nil))
	require.NoError(t, err)
	require.Empty(t, empty)

	_, err = actioninsights.DecodeSpawnIndex([]byte{0x80})
	require.Error(t, err)
}

func TestSpawnIndexVersion(t *testing.T) {
	b := actioninsights.EncodeSpawnIndex([]actioninsights.Pair{{KeyID: 5, VersionID: 8}, {KeyID: 5, VersionID: 3}, {KeyID: 9, VersionID: 1}})
	for key, want := range map[int64]int64{5: 3, 9: 1, 1: 0, 7: 0, 10: 0} {
		v, ok, err := b.Version(key)
		require.NoError(t, err)
		require.Equal(t, want != 0, ok, key)
		require.Equal(t, want, v, key)
	}
}
