//go:build unit

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildAffiliateRecordWhere(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	where, args := buildAffiliateRecordWhere(service.AffiliateRecordFilter{
		Search:  "  Alice  ",
		StartAt: &start,
		EndAt:   &end,
	}, "created_at", []string{"email", "user_id::text"})

	require.Equal(t,
		"WHERE created_at >= $1 AND created_at <= $2 AND (LOWER(email) LIKE $3 OR LOWER(user_id::text) LIKE $3)",
		where,
	)
	require.Equal(t, []any{start, end, "%alice%"}, args)
}

func TestBuildAffiliateRecordOrderByUsesAllowlist(t *testing.T) {
	t.Parallel()

	columns := map[string]string{"amount": "ledger.amount", "created_at": "ledger.created_at"}
	require.Equal(t,
		"ORDER BY ledger.amount ASC NULLS LAST",
		buildAffiliateRecordOrderBy(service.AffiliateRecordFilter{SortBy: "amount"}, columns, "ledger.created_at"),
	)
	require.Equal(t,
		"ORDER BY ledger.created_at DESC NULLS LAST",
		buildAffiliateRecordOrderBy(service.AffiliateRecordFilter{
			SortBy:   "amount; DROP TABLE users",
			SortDesc: true,
		}, columns, "ledger.created_at"),
	)
}
