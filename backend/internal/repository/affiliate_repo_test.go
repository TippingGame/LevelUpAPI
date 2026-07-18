package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffiliateUserOverviewSQLUsesAppliedShareSettlements(t *testing.T) {
	query := strings.Join(strings.Fields(affiliateUserOverviewSQL), " ")

	require.Contains(t, query, "FROM account_share_settlement_entries")
	require.Contains(t, query, "SUM(invite_credit)")
	require.Contains(t, query, "status = 'applied'")
	require.Contains(t, query, "u.balance::double precision")
}

func TestAffiliateRecordQueriesUseSettlementAndTransferAuditFields(t *testing.T) {
	source, err := os.ReadFile("affiliate_repo.go")
	require.NoError(t, err)
	content := string(source)

	require.Contains(t, content, "FROM account_share_settlement_entries ase")
	require.Contains(t, content, "ase.invite_credit::double precision")
	require.Contains(t, content, "ual.amount::double precision")
	require.Contains(t, content, "ual.balance_after::double precision")
	require.NotContains(t, content, "parseAffiliateRebateAmount")
	require.NotContains(t, content, `"current_balance": "u.balance"`)
}
