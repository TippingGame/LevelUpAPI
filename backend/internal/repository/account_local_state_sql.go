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
		AND NOT %s
	)`,
		typeCol,
		service.AccountTypeAPIKey,
		service.AccountTypeBedrock,
		credentialsCol,
		accountCustomErrorPolicyActiveRawSQL(credentialsCol),
	)
}

func accountOpenAIOAuthRelayPoolTempUnschedIgnoredRawSQL(tableAlias string) string {
	platformCol := qualifyAccountSQLColumn(tableAlias, "platform")
	typeCol := qualifyAccountSQLColumn(tableAlias, "type")
	reasonCol := qualifyAccountSQLColumn(tableAlias, "temp_unschedulable_reason")
	return fmt.Sprintf(`(
		%[1]s = '%[2]s'
		AND %[3]s = '%[4]s'
		AND (
			COALESCE(%[5]s, '') LIKE '%%"matched_keyword":"%[6]s"%%'
			OR COALESCE(%[5]s, '') ILIKE '%%upstream relay pool unavailable%%'
		)
	)`,
		platformCol,
		service.PlatformOpenAI,
		typeCol,
		service.AccountTypeOAuth,
		reasonCol,
		service.TempUnschedKeywordUpstreamRelayPoolUnavailable,
	)
}

func accountCustomErrorPolicyActiveRawSQL(credentialsCol string) string {
	return fmt.Sprintf(`(
		LOWER(COALESCE(%[1]s->>'custom_error_codes_enabled', '')) = 'true'
		AND %s
	)`, credentialsCol, accountCustomErrorPolicyValueActiveRawSQL(fmt.Sprintf("%s->'custom_error_codes'", credentialsCol)))
}

func accountCustomErrorPolicyValueActiveRawSQL(jsonbExpr string) string {
	return fmt.Sprintf(`CASE jsonb_typeof(%[1]s)
		WHEN 'array' THEN EXISTS (
			SELECT 1
			FROM jsonb_array_elements(%[1]s) AS custom_error_code(value)
			WHERE %s
		)
		WHEN 'string' THEN %s
		WHEN 'number' THEN %s
		ELSE FALSE
	END`,
		jsonbExpr,
		accountCustomErrorPolicyScalarActiveRawSQL("custom_error_code.value"),
		accountCustomErrorPolicyStringActiveRawSQL(jsonbExpr),
		accountCustomErrorPolicyNumberActiveRawSQL(jsonbExpr),
	)
}

func accountCustomErrorPolicyScalarActiveRawSQL(jsonbExpr string) string {
	return fmt.Sprintf(`CASE jsonb_typeof(%[1]s)
		WHEN 'string' THEN %s
		WHEN 'number' THEN %s
		ELSE FALSE
	END`,
		jsonbExpr,
		accountCustomErrorPolicyStringActiveRawSQL(jsonbExpr),
		accountCustomErrorPolicyNumberActiveRawSQL(jsonbExpr),
	)
}

func accountCustomErrorPolicyNumberActiveRawSQL(jsonbExpr string) string {
	textExpr := fmt.Sprintf("(%s #>> '{}')", jsonbExpr)
	return fmt.Sprintf(`(
		%[1]s ~ '^[0-9]+(\.[0-9]+)?$'
		AND (%[1]s)::numeric >= 100
		AND (%[1]s)::numeric < 600
	)`, textExpr)
}

func accountCustomErrorPolicyStringActiveRawSQL(jsonbExpr string) string {
	textExpr := fmt.Sprintf("COALESCE(%s #>> '{}', '')", jsonbExpr)
	normalizedExpr := fmt.Sprintf("replace(replace(%s, '，', ','), ' ', '')", textExpr)
	return fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM regexp_split_to_table(%s, ',') AS custom_error_token(token)
		WHERE %s
	)`, normalizedExpr, accountCustomErrorPolicyStringTokenActiveRawSQL("custom_error_token.token"))
}

func accountCustomErrorPolicyStringTokenActiveRawSQL(tokenExpr string) string {
	return fmt.Sprintf(`(
		%[1]s <> ''
		AND (
			(%[1]s ~ '^[0-9]+$' AND (%[1]s)::numeric BETWEEN 100 AND 599)
			OR (
				%[1]s ~ '^[0-9]+-[0-9]+$'
				AND (split_part(%[1]s, '-', 1))::numeric BETWEEN 100 AND 599
				AND (split_part(%[1]s, '-', 2))::numeric BETWEEN 100 AND 599
				AND (split_part(%[1]s, '-', 1))::numeric <= (split_part(%[1]s, '-', 2))::numeric
			)
		)
	)`, tokenExpr)
}

func qualifyAccountSQLColumn(tableAlias, column string) string {
	tableAlias = strings.TrimSpace(tableAlias)
	if tableAlias == "" {
		return column
	}
	return tableAlias + "." + column
}
