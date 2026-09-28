package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownstreamPricingSyncMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("241_channel_downstream_pricing_sync.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(sqlBytes))
	require.Contains(t, sql, "create table if not exists channel_downstream_pricing_sync")
	require.Contains(t, sql, "channel_id")
	require.Contains(t, sql, "model_overrides")
	require.Contains(t, sql, "managed_models")
	require.Contains(t, sql, "upstream_snapshot")
	require.Contains(t, sql, "snapshot_fetched_at")
	require.NotContains(t, sql, "drop table")
}
