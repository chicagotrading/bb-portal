package config_test

import (
	"testing"

	"github.com/buildbarn/bb-portal/pkg/proto/configuration/bb_portal"
	"github.com/buildbarn/bb-portal/pkg/proto/configuration/bb_seed"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/stretchr/testify/require"
)

func TestPortalConfiguration(t *testing.T) {
	var configuration bb_portal.ApplicationConfiguration
	require.NoError(t, util.UnmarshalConfigurationFromFile("portal.jsonnet", &configuration))
	cas := configuration.GetContentAddressableStorageServer().GetContentAddressableStorage()
	require.NotNil(t, cas.GetChunkStorage().GetGrpc())
	require.NotNil(t, cas.GetChunkMappingStorage().GetGrpc())
	require.NotNil(t, configuration.GetExecLogIngestServiceConfiguration().GetPollInterval())
	require.NotNil(t, configuration.GetSchedulerServiceConfiguration().GetDrainAuthorizer())
}

func TestSeedConfiguration(t *testing.T) {
	var configuration bb_seed.ApplicationConfiguration
	require.NoError(t, util.UnmarshalConfigurationFromFile("seed.jsonnet", &configuration))
}
