//go:build unit

package admin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseAffiliateRecordDateRange(t *testing.T) {
	t.Parallel()

	start := parseAffiliateRecordStartTime("2026-07-15", "Asia/Shanghai")
	end := parseAffiliateRecordEndTime("2026-07-15", "Asia/Shanghai")
	require.NotNil(t, start)
	require.NotNil(t, end)
	require.Equal(t, 2026, start.In(time.FixedZone("UTC+8", 8*60*60)).Year())
	require.Equal(t, time.Hour*24-time.Nanosecond, end.Sub(*start))

	rfc3339 := "2026-07-15T08:30:00+08:00"
	parsed := parseAffiliateRecordStartTime(rfc3339, "UTC")
	require.NotNil(t, parsed)
	require.Equal(t, rfc3339, parsed.Format(time.RFC3339))

	require.Nil(t, parseAffiliateRecordStartTime("not-a-date", "Asia/Shanghai"))
	require.Nil(t, parseAffiliateRecordEndTime("", "Asia/Shanghai"))
}
