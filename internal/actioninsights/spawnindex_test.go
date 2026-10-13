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
