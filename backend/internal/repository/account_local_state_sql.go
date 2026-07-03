package repository

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func accountRespectsLocalSystemErrorStateSQL(tableAlias string) string {
	return fmt.Sprintf("NOT %s", accountIgnoresLocalSystemErrorStateRawSQL(tableAlias))
}

func accountIgnoresLocalSystemErrorStateRawSQL(tableAlias string) string {
	typeCol := qualifyAccountSQLColumn(tableAlias, "type")
	credentialsCol := qualifyAccountSQLColumn(tableAlias, "credentials")
	return fmt.Sprintf(`(
		%s IN ('%s', '%s')
		AND LOWER(COALESCE(%s->>'pool_mode', '')) = 'true'
		AND LOWER(COALESCE(%s->>'custom_error_codes_enabled', '')) <> 'true'
	)`,
		typeCol,
		service.AccountTypeAPIKey,
		service.AccountTypeBedrock,
		credentialsCol,
		credentialsCol,
	)
}

func qualifyAccountSQLColumn(tableAlias, column string) string {
	tableAlias = strings.TrimSpace(tableAlias)
	if tableAlias == "" {
		return column
	}
	return tableAlias + "." + column
}
