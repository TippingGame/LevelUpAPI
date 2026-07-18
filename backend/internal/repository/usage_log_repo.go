package repository

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	gocache "github.com/patrickmn/go-cache"
)

const rawUsageLogModelColumn = "model"

const usageLogEffectivePlatformExpr = "COALESCE(NULLIF(g.platform,''), a.platform)"

// usageLogSuccessFilterUL excludes zero-cost failure placeholder rows from
// aggregate statistics while retaining successful image/video billed rows.
const usageLogSuccessFilterUL = "ul.actual_cost > 0"

var placeholderPattern = regexp.MustCompile(`\$\d+`)

// rawUsageLogModelColumn preserves the exact stored usage_logs.model semantics for direct filters.
// Historical rows may contain upstream/billing model values, while newer rows store requested_model.
// Requested/upstream/mapping analytics must use resolveModelDimensionExpression instead.

// dateFormatWhitelist 将 granularity 参数映射为 PostgreSQL TO_CHAR 格式字符串，防止外部输入直接拼入 SQL
var dateFormatWhitelist = map[string]string{
	"hour":  "YYYY-MM-DD HH24:00",
	"day":   "YYYY-MM-DD",
	"week":  "IYYY-IW",
	"month": "YYYY-MM",
}

// safeDateFormat 根据白名单获取 dateFormat，未匹配时返回默认值
func safeDateFormat(granularity string) string {
	if f, ok := dateFormatWhitelist[granularity]; ok {
		return f
	}
	return "YYYY-MM-DD"
}

// appendRawUsageLogModelWhereCondition keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelWhereCondition(conditions []string, args []any, model string) ([]string, []any) {
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", rawUsageLogModelColumn, len(args)+1))
	args = append(args, model)
	return conditions, args
}

func appendUsageLogBillingModeWhereCondition(conditions []string, args []any, billingMode string) ([]string, []any) {
	return appendUsageLogBillingModeWhereConditionWithAlias(conditions, args, billingMode, "")
}

func appendUsageLogBillingModeWhereConditionWithAlias(conditions []string, args []any, billingMode string, alias string) ([]string, []any) {
	mode := strings.TrimSpace(billingMode)
	if mode == "" {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	placeholder := fmt.Sprintf("$%d", len(args)+1)
	switch service.BillingMode(mode) {
	case service.BillingModeImage:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) > 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	case service.BillingModeVideo:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	case service.BillingModeToken:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) <= 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	default:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	}
	args = append(args, mode)
	return conditions, args
}

func appendUsageLogBillingModeQueryFilter(query string, args []any, billingMode string, alias string) (string, []any) {
	conditions, args := appendUsageLogBillingModeWhereConditionWithAlias(nil, args, billingMode, alias)
	if len(conditions) == 0 {
		return query, args
	}
	return query + " AND " + conditions[0], args
}

func appendUsageLogModelWhereCondition(conditions []string, args []any, model string, source string) ([]string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelWhereCondition(conditions, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", resolveModelDimensionExpression(source), len(args)+1))
	args = append(args, model)
	return conditions, args
}

// appendRawUsageLogModelQueryFilter keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelQueryFilter(query string, args []any, model string) (string, []any) {
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", rawUsageLogModelColumn, len(args)+1)
	args = append(args, model)
	return query, args
}

func appendUsageLogModelQueryFilter(query string, args []any, model string, source string) (string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelQueryFilter(query, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", resolveModelDimensionExpression(source), len(args)+1)
	args = append(args, model)
	return query, args
}

func buildRequestTypeFilterConditionWithAlias(startArgIndex int, requestType int16, alias string) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = FALSE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = TRUE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sopenai_ws_mode = TRUE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix), []any{requestTypeArg}
	default:
		return fmt.Sprintf("%srequest_type = $%d", prefix, startArgIndex), []any{requestTypeArg}
	}
}

type usageLogRepository struct {
	client *dbent.Client
	sql    sqlExecutor
	db     *sql.DB

	createBatchOnce     sync.Once
	createBatchCh       chan usageLogCreateRequest
	bestEffortBatchOnce sync.Once
	bestEffortBatchCh   chan usageLogBestEffortRequest
	bestEffortRecent    *gocache.Cache
}

func NewUsageLogRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageLogRepository {
	return newUsageLogRepositoryWithSQL(client, sqlDB)
}

func newUsageLogRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageLogRepository {
	// 使用 scanSingleRow 替代 QueryRowContext，保证 ent.Tx 作为 sqlExecutor 可用。
	repo := &usageLogRepository{client: client, sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		repo.db = db
	}
	repo.bestEffortRecent = gocache.New(usageLogBestEffortRecentTTL, time.Minute)
	return repo
}

func (r *usageLogRepository) fillDashboardUsageStatsWithSnapshots(ctx context.Context, stats *DashboardStats, startUTC, endUTC, todayUTC, now time.Time) error {
	if isUsageSnapshotBusinessFullDayRange(startUTC, endUTC) {
		if err := r.fillDashboardRangeStatsWithSnapshots(ctx, stats, startUTC, endUTC); err != nil {
			return err
		}
		if err := r.fillDashboardTodayAndActiveStatsFromUsageLogs(ctx, stats, todayUTC, now); err != nil {
			return err
		}
		return nil
	}
	return r.fillDashboardUsageStatsFromUsageLogs(ctx, stats, startUTC, endUTC, todayUTC, now)
}

func (r *usageLogRepository) fillDashboardRangeStatsWithSnapshots(ctx context.Context, stats *DashboardStats, startUTC, endUTC time.Time) error {
	startDate, endDate := usageSnapshotBusinessDateRange(startUTC, endUTC)
	rawConditions, rawArgs := buildRawUsageStatsSnapshotConditions(UsageLogFilters{StartTime: &startUTC, EndTime: &endUTC})
	rawWhere := buildWhere(rawConditions)
	rawQuery := shiftPostgresPlaceholders(fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS total_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost,
			COALESCE(SUM(actual_cost), 0) AS total_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS total_account_cost,
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms
		FROM usage_logs ul
		%s
	`, rawWhere), 2)
	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(total_requests), 0) AS total_requests,
			COALESCE(SUM(total_input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(total_output_tokens), 0) AS total_output_tokens,
			COALESCE(SUM(total_cache_creation_tokens), 0) AS total_cache_creation_tokens,
			COALESCE(SUM(total_cache_read_tokens), 0) AS total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost,
			COALESCE(SUM(total_actual_cost), 0) AS total_actual_cost,
			COALESCE(SUM(total_account_cost), 0) AS total_account_cost,
			COALESCE(SUM(total_duration_ms), 0) AS total_duration_ms
		FROM (
			SELECT
				total_requests,
				input_tokens AS total_input_tokens,
				output_tokens AS total_output_tokens,
				cache_creation_tokens AS total_cache_creation_tokens,
				cache_read_tokens AS total_cache_read_tokens,
				total_cost,
				actual_cost AS total_actual_cost,
				account_cost AS total_account_cost,
				total_duration_ms
			FROM usage_daily_dimension_snapshots s
			WHERE s.bucket_date >= $1::date AND s.bucket_date < $2::date
			UNION ALL
			%s
		) usage_totals
	`, rawQuery)
	args := append([]any{startDate, endDate}, rawArgs...)
	var totalDurationMs int64
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		args,
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.TotalAccountCost,
		&totalDurationMs,
	); err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(totalDurationMs) / float64(stats.TotalRequests)
	}
	return nil
}

func (r *usageLogRepository) fillDashboardTodayAndActiveStatsFromUsageLogs(ctx context.Context, stats *DashboardStats, todayUTC, now time.Time) error {
	todayEnd := todayUTC.Add(24 * time.Hour)
	todayStatsQuery := `
		SELECT
			COUNT(*) AS today_requests,
			COALESCE(SUM(input_tokens), 0) AS today_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS today_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS today_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS today_cost,
			COALESCE(SUM(actual_cost), 0) AS today_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS today_account_cost
		FROM usage_logs
		WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		todayStatsQuery,
		[]any{todayUTC, todayEnd},
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
		&stats.TodayAccountCost,
	); err != nil {
		return err
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	hourStart := now.UTC().Truncate(time.Hour)
	hourEnd := hourStart.Add(time.Hour)
	activeUsersQuery := `
		WITH scoped AS (
			SELECT user_id, created_at
			FROM usage_logs
			WHERE created_at >= LEAST($1::timestamptz, $3::timestamptz)
				AND created_at < GREATEST($2::timestamptz, $4::timestamptz)
		)
		SELECT
			COUNT(DISTINCT CASE WHEN created_at >= $1::timestamptz AND created_at < $2::timestamptz THEN user_id END) AS active_users,
			COUNT(DISTINCT CASE WHEN created_at >= $3::timestamptz AND created_at < $4::timestamptz THEN user_id END) AS hourly_active_users
		FROM scoped
	`
	return scanSingleRow(ctx, r.sql, activeUsersQuery, []any{todayUTC, todayEnd, hourStart, hourEnd}, &stats.ActiveUsers, &stats.HourlyActiveUsers)
}

// GetUserAccountSharingDashboard returns owned-account self usage and external public-share settlement stats.
func (r *usageLogRepository) GetUserAccountSharingDashboard(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) (*usagestats.AccountSharingDashboardStats, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user id must be positive")
	}
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -7)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	accounts, summary, err := r.getUserAccountSharingAccountStats(ctx, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	trend, err := r.getUserAccountSharingTrend(ctx, userID, startTime, endTime, granularity)
	if err != nil {
		return nil, err
	}

	endDisplay := endTime
	if endDisplay.After(startTime) {
		endDisplay = endDisplay.Add(-time.Nanosecond)
	}
	return &usagestats.AccountSharingDashboardStats{
		Summary:     summary,
		Accounts:    accounts,
		Trend:       trend,
		StartDate:   startTime.Format("2006-01-02"),
		EndDate:     endDisplay.Format("2006-01-02"),
		Granularity: granularity,
	}, nil
}

func (r *usageLogRepository) getUserAccountSharingAccountStats(ctx context.Context, userID int64, startTime, endTime time.Time) ([]usagestats.AccountSharingAccountStat, usagestats.AccountSharingSummary, error) {
	query := `
		WITH self_usage AS (
			SELECT
				ul.account_id,
				COUNT(*) AS self_requests,
				COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS self_tokens,
				COALESCE(SUM(ul.actual_cost), 0) AS self_actual_cost,
				COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS self_account_cost
			FROM usage_logs ul
			JOIN accounts a ON a.id = ul.account_id
			WHERE a.owner_user_id = $1
			  AND ul.user_id = $1
			  AND ul.created_at >= $2
			  AND ul.created_at < $3
			GROUP BY ul.account_id
		),
		external_usage AS (
			SELECT
				account_id,
				COUNT(*) AS external_requests,
				COALESCE(SUM(consumer_charge), 0) AS external_consumer_charge,
				COALESCE(SUM(account_cost), 0) AS external_account_cost,
				COALESCE(SUM(owner_credit), 0) AS external_owner_credit,
				COALESCE(SUM(platform_fee), 0) AS external_platform_fee
			FROM account_share_settlement_entries
			WHERE owner_user_id = $1
			  AND consumer_user_id <> owner_user_id
			  AND status = 'applied'
			  AND created_at >= $2
			  AND created_at < $3
			GROUP BY account_id
		)
		SELECT
			a.id,
			a.name,
			a.platform,
			a.share_mode,
			a.share_status,
			COALESCE(s.self_requests, 0),
			COALESCE(s.self_tokens, 0),
			COALESCE(s.self_actual_cost, 0),
			COALESCE(s.self_account_cost, 0),
			COALESCE(e.external_requests, 0),
			COALESCE(e.external_consumer_charge, 0),
			COALESCE(e.external_account_cost, 0),
			COALESCE(e.external_owner_credit, 0),
			COALESCE(e.external_platform_fee, 0)
		FROM accounts a
		LEFT JOIN self_usage s ON s.account_id = a.id
		LEFT JOIN external_usage e ON e.account_id = a.id
		WHERE a.owner_user_id = $1
		  AND a.deleted_at IS NULL
		ORDER BY
			(COALESCE(s.self_account_cost, 0) + COALESCE(e.external_account_cost, 0)) DESC,
			a.created_at DESC,
			a.id DESC
	`

	rows, err := r.sql.QueryContext(ctx, query, userID, startTime, endTime)
	if err != nil {
		return nil, usagestats.AccountSharingSummary{}, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]usagestats.AccountSharingAccountStat, 0)
	summary := usagestats.AccountSharingSummary{}
	for rows.Next() {
		var item usagestats.AccountSharingAccountStat
		if err := rows.Scan(
			&item.AccountID,
			&item.Name,
			&item.Platform,
			&item.ShareMode,
			&item.ShareStatus,
			&item.SelfRequests,
			&item.SelfTokens,
			&item.SelfActualCost,
			&item.SelfAccountCost,
			&item.ExternalRequests,
			&item.ExternalConsumerCharge,
			&item.ExternalAccountCost,
			&item.ExternalOwnerCredit,
			&item.ExternalPlatformFee,
		); err != nil {
			return nil, usagestats.AccountSharingSummary{}, err
		}
		accounts = append(accounts, item)
		summary.OwnedAccounts++
		switch {
		case item.ShareMode == service.AccountShareModePrivate:
			summary.PrivateAccounts++
		case item.ShareMode == service.AccountShareModePublic && item.ShareStatus == service.AccountShareStatusPending:
			summary.PublicPendingAccounts++
		case item.ShareMode == service.AccountShareModePublic && item.ShareStatus == service.AccountShareStatusApproved:
			summary.PublicApprovedAccounts++
		case item.ShareMode == service.AccountShareModePublic && item.ShareStatus == service.AccountShareStatusSuspended:
			summary.PublicSuspendedAccounts++
		}
		summary.SelfRequests += item.SelfRequests
		summary.SelfTokens += item.SelfTokens
		summary.SelfActualCost += item.SelfActualCost
		summary.SelfAccountCost += item.SelfAccountCost
		summary.ExternalRequests += item.ExternalRequests
		summary.ExternalConsumerCharge += item.ExternalConsumerCharge
		summary.ExternalAccountCost += item.ExternalAccountCost
		summary.ExternalOwnerCredit += item.ExternalOwnerCredit
		summary.ExternalPlatformFee += item.ExternalPlatformFee
	}
	if err := rows.Err(); err != nil {
		return nil, usagestats.AccountSharingSummary{}, err
	}
	summary.TotalAccountCost = summary.SelfAccountCost + summary.ExternalAccountCost
	summary.BalanceNetChange = summary.ExternalOwnerCredit - summary.SelfActualCost
	return accounts, summary, nil
}

func (r *usageLogRepository) getUserAccountSharingTrend(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) (results []usagestats.AccountSharingTrendPoint, err error) {
	dateFormat := safeDateFormat(granularity)
	query := fmt.Sprintf(`
		WITH self_usage AS (
			SELECT
				TO_CHAR(ul.created_at, '%s') AS date,
				COUNT(*) AS self_requests,
				COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) AS self_tokens,
				COALESCE(SUM(ul.actual_cost), 0) AS self_actual_cost,
				COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS self_account_cost
			FROM usage_logs ul
			JOIN accounts a ON a.id = ul.account_id
			WHERE a.owner_user_id = $1
			  AND ul.user_id = $1
			  AND ul.created_at >= $2
			  AND ul.created_at < $3
			GROUP BY date
		),
		external_usage AS (
			SELECT
				TO_CHAR(created_at, '%s') AS date,
				COUNT(*) AS external_requests,
				COALESCE(SUM(consumer_charge), 0) AS external_consumer_charge,
				COALESCE(SUM(account_cost), 0) AS external_account_cost,
				COALESCE(SUM(owner_credit), 0) AS external_owner_credit,
				COALESCE(SUM(platform_fee), 0) AS external_platform_fee
			FROM account_share_settlement_entries
			WHERE owner_user_id = $1
			  AND consumer_user_id <> owner_user_id
			  AND status = 'applied'
			  AND created_at >= $2
			  AND created_at < $3
			GROUP BY date
		)
		SELECT
			COALESCE(s.date, e.date) AS date,
			COALESCE(s.self_requests, 0),
			COALESCE(s.self_tokens, 0),
			COALESCE(s.self_actual_cost, 0),
			COALESCE(s.self_account_cost, 0),
			COALESCE(e.external_requests, 0),
			COALESCE(e.external_consumer_charge, 0),
			COALESCE(e.external_account_cost, 0),
			COALESCE(e.external_owner_credit, 0),
			COALESCE(e.external_platform_fee, 0)
		FROM self_usage s
		FULL OUTER JOIN external_usage e ON e.date = s.date
		ORDER BY date ASC
	`, dateFormat, dateFormat)

	rows, err := r.sql.QueryContext(ctx, query, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	for rows.Next() {
		var item usagestats.AccountSharingTrendPoint
		if err := rows.Scan(
			&item.Date,
			&item.SelfRequests,
			&item.SelfTokens,
			&item.SelfActualCost,
			&item.SelfAccountCost,
			&item.ExternalRequests,
			&item.ExternalConsumerCharge,
			&item.ExternalAccountCost,
			&item.ExternalOwnerCredit,
			&item.ExternalPlatformFee,
		); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *usageLogRepository) attachEndpointStats(ctx context.Context, stats *UsageStats, filters UsageLogFilters) (*UsageStats, error) {
	if stats == nil {
		return nil, fmt.Errorf("usage stats is nil")
	}

	start := time.Unix(0, 0).UTC()
	if filters.StartTime != nil {
		start = *filters.StartTime
	}
	end := time.Now().UTC()
	if filters.EndTime != nil {
		end = *filters.EndTime
	}

	endpoints, endpointErr := r.GetEndpointStatsWithFilters(ctx, start, end, filters.UserID, filters.APIKeyID, filters.AccountID, filters.GroupID, filters.Model, filters.RequestType, filters.Stream, filters.BillingType)
	if endpointErr != nil {
		logger.LegacyPrintf("repository.usage_log", "GetEndpointStatsWithFilters failed in GetStatsWithFilters: %v", endpointErr)
		endpoints = []EndpointStat{}
	}
	upstreamEndpoints, upstreamEndpointErr := r.GetUpstreamEndpointStatsWithFilters(ctx, start, end, filters.UserID, filters.APIKeyID, filters.AccountID, filters.GroupID, filters.Model, filters.RequestType, filters.Stream, filters.BillingType)
	if upstreamEndpointErr != nil {
		logger.LegacyPrintf("repository.usage_log", "GetUpstreamEndpointStatsWithFilters failed in GetStatsWithFilters: %v", upstreamEndpointErr)
		upstreamEndpoints = []EndpointStat{}
	}
	endpointPaths, endpointPathErr := r.getEndpointPathStatsWithFilters(
		ctx,
		start,
		end,
		filters.UserID,
		filters.APIKeyID,
		filters.AccountID,
		filters.GroupID,
		filters.Model,
		filters.ModelFilterSource,
		filters.RequestType,
		filters.Stream,
		filters.BillingType,
		filters.BillingMode,
	)
	if endpointPathErr != nil {
		logger.LegacyPrintf("repository.usage_log", "getEndpointPathStatsWithFilters failed in GetStatsWithFilters: %v", endpointPathErr)
		endpointPaths = []EndpointStat{}
	}
	stats.Endpoints = endpoints
	stats.UpstreamEndpoints = upstreamEndpoints
	stats.EndpointPaths = endpointPaths

	return stats, nil
}

// resolveAccountUsageStatsScopeIDs expands display-only account stats to prior
// account rows that represent the same external account, including soft-deleted
// rows. Runtime quota, scheduling, and billing paths must keep using account.ID.
func (r *usageLogRepository) resolveAccountUsageStatsScopeIDs(ctx context.Context, accountID int64) ([]int64, error) {
	if accountID <= 0 {
		return []int64{}, nil
	}

	query := `
		WITH current_account AS (
			SELECT id, owner_user_id, platform, type, credentials, extra
			FROM accounts
			WHERE id = $1
		),
		account_scope AS (
			SELECT DISTINCT a.id
			FROM accounts a
			JOIN current_account c
			  ON a.platform = c.platform
			 AND a.type = c.type
			 AND a.owner_user_id IS NOT DISTINCT FROM c.owner_user_id
			WHERE a.id = c.id
			   OR (
				c.platform = 'openai'
				AND c.type = 'oauth'
				AND (
					(
						NULLIF(BTRIM(c.credentials->>'organization_id'), '') IS NOT NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'organization_id'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'organization_id'), ''))
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_user_id'), '') = NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '')
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'organization_id'), '') IS NOT NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '') IS NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_account_id'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'organization_id'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'organization_id'), ''))
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_user_id'), '') IS NULL
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_account_id'), '') = NULLIF(BTRIM(c.credentials->>'chatgpt_account_id'), '')
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'organization_id'), '') IS NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '') IS NOT NULL
						AND NULLIF(BTRIM(a.credentials->>'organization_id'), '') IS NULL
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_user_id'), '') = NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '')
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'organization_id'), '') IS NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_user_id'), '') IS NULL
						AND NULLIF(BTRIM(c.credentials->>'chatgpt_account_id'), '') IS NOT NULL
						AND NULLIF(BTRIM(a.credentials->>'organization_id'), '') IS NULL
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_user_id'), '') IS NULL
						AND NULLIF(BTRIM(a.credentials->>'chatgpt_account_id'), '') = NULLIF(BTRIM(c.credentials->>'chatgpt_account_id'), '')
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'email'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'email'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'email'), ''))
					)
				)
			   )
			   OR (
				c.platform = 'anthropic'
				AND c.type = 'oauth'
				AND (
					(
						COALESCE(NULLIF(BTRIM(c.extra->>'org_uuid'), ''), NULLIF(BTRIM(c.credentials->>'org_uuid'), '')) IS NOT NULL
						AND COALESCE(NULLIF(BTRIM(c.extra->>'account_uuid'), ''), NULLIF(BTRIM(c.credentials->>'account_uuid'), '')) IS NOT NULL
						AND LOWER(COALESCE(NULLIF(BTRIM(a.extra->>'org_uuid'), ''), NULLIF(BTRIM(a.credentials->>'org_uuid'), ''))) =
							LOWER(COALESCE(NULLIF(BTRIM(c.extra->>'org_uuid'), ''), NULLIF(BTRIM(c.credentials->>'org_uuid'), '')))
						AND LOWER(COALESCE(NULLIF(BTRIM(a.extra->>'account_uuid'), ''), NULLIF(BTRIM(a.credentials->>'account_uuid'), ''))) =
							LOWER(COALESCE(NULLIF(BTRIM(c.extra->>'account_uuid'), ''), NULLIF(BTRIM(c.credentials->>'account_uuid'), '')))
					)
					OR (
						COALESCE(NULLIF(BTRIM(c.extra->>'account_uuid'), ''), NULLIF(BTRIM(c.credentials->>'account_uuid'), '')) IS NOT NULL
						AND COALESCE(NULLIF(BTRIM(c.extra->>'org_uuid'), ''), NULLIF(BTRIM(c.credentials->>'org_uuid'), '')) IS NULL
						AND COALESCE(NULLIF(BTRIM(a.extra->>'org_uuid'), ''), NULLIF(BTRIM(a.credentials->>'org_uuid'), '')) IS NULL
						AND LOWER(COALESCE(NULLIF(BTRIM(a.extra->>'account_uuid'), ''), NULLIF(BTRIM(a.credentials->>'account_uuid'), ''))) =
							LOWER(COALESCE(NULLIF(BTRIM(c.extra->>'account_uuid'), ''), NULLIF(BTRIM(c.credentials->>'account_uuid'), '')))
					)
					OR (
						COALESCE(NULLIF(BTRIM(c.extra->>'org_uuid'), ''), NULLIF(BTRIM(c.credentials->>'org_uuid'), '')) IS NOT NULL
						AND COALESCE(NULLIF(BTRIM(c.extra->>'account_uuid'), ''), NULLIF(BTRIM(c.credentials->>'account_uuid'), '')) IS NULL
						AND COALESCE(NULLIF(BTRIM(a.extra->>'account_uuid'), ''), NULLIF(BTRIM(a.credentials->>'account_uuid'), '')) IS NULL
						AND LOWER(COALESCE(NULLIF(BTRIM(a.extra->>'org_uuid'), ''), NULLIF(BTRIM(a.credentials->>'org_uuid'), ''))) =
							LOWER(COALESCE(NULLIF(BTRIM(c.extra->>'org_uuid'), ''), NULLIF(BTRIM(c.credentials->>'org_uuid'), '')))
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'email_address'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'email_address'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'email_address'), ''))
					)
				)
			   )
			   OR (
				c.platform = 'gemini'
				AND c.type = 'oauth'
				AND NULLIF(BTRIM(c.credentials->>'project_id'), '') IS NOT NULL
				AND LOWER(COALESCE(NULLIF(BTRIM(a.credentials->>'oauth_type'), ''), 'code_assist')) =
					LOWER(COALESCE(NULLIF(BTRIM(c.credentials->>'oauth_type'), ''), 'code_assist'))
				AND LOWER(NULLIF(BTRIM(a.credentials->>'project_id'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'project_id'), ''))
			   )
			   OR (
				c.platform = 'antigravity'
				AND c.type = 'oauth'
				AND (
					(
						NULLIF(BTRIM(c.credentials->>'project_id'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'project_id'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'project_id'), ''))
					)
					OR (
						NULLIF(BTRIM(c.credentials->>'email'), '') IS NOT NULL
						AND LOWER(NULLIF(BTRIM(a.credentials->>'email'), '')) = LOWER(NULLIF(BTRIM(c.credentials->>'email'), ''))
					)
				)
			   )
		)
		SELECT id FROM account_scope ORDER BY id
	`

	rows, err := r.sql.QueryContext(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accountIDs := make([]int64, 0, 1)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		accountIDs = append(accountIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return []int64{accountID}, nil
	}
	return accountIDs, nil
}

func (r *usageLogRepository) getModelStatsForAccountIDs(ctx context.Context, startTime, endTime time.Time, accountIDs []int64, source string) (results []ModelStat, err error) {
	if len(accountIDs) == 0 {
		return []ModelStat{}, nil
	}

	modelExpr := resolveModelDimensionExpression(source)
	query := fmt.Sprintf(`
		SELECT
			%s as model,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as account_cost
		FROM usage_logs
		WHERE account_id = ANY($1) AND created_at >= $2 AND created_at < $3
		GROUP BY %s
		ORDER BY total_tokens DESC
	`, modelExpr, modelExpr)

	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results, err = scanModelStatsRows(rows)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (r *usageLogRepository) getEndpointStatsByColumnForAccountIDs(ctx context.Context, endpointColumn string, startTime, endTime time.Time, accountIDs []int64) (results []EndpointStat, err error) {
	if len(accountIDs) == 0 {
		return []EndpointStat{}, nil
	}
	switch endpointColumn {
	case "inbound_endpoint", "upstream_endpoint":
	default:
		return nil, fmt.Errorf("unsupported account endpoint stats column: %s", endpointColumn)
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(TRIM(%s), ''), 'unknown') AS endpoint,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) AS total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as actual_cost
		FROM usage_logs
		WHERE account_id = ANY($1) AND created_at >= $2 AND created_at < $3
		GROUP BY endpoint
		ORDER BY requests DESC
	`, endpointColumn)

	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]EndpointStat, 0)
	for rows.Next() {
		var row EndpointStat
		if err := rows.Scan(&row.Endpoint, &row.Requests, &row.TotalTokens, &row.Cost, &row.ActualCost); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func accountUsageStatsAccountIDStrings(accountIDs []int64) []string {
	result := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		result = append(result, strconv.FormatInt(id, 10))
	}
	return result
}

func accountUsageStatsDateLabel(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("01/02")
}

func (r *usageLogRepository) getAccountHourlyCostsByDate(ctx context.Context, startTime, endTime time.Time, accountIDs []int64) (map[string]float64, error) {
	result := make(map[string]float64)
	if len(accountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			TO_CHAR(created_at, 'YYYY-MM-DD') AS date,
			COALESCE(SUM(CASE WHEN direction = 'debit' THEN amount ELSE -amount END), 0) AS hourly_cost
		FROM user_balance_ledger
		WHERE metadata->>'account_id' = ANY($1)
			AND reason IN ($2, $3, $4)
			AND created_at >= $5
			AND created_at < $6
		GROUP BY date
		ORDER BY date ASC
	`
	rows, err := r.sql.QueryContext(
		ctx,
		query,
		pq.Array(accountUsageStatsAccountIDStrings(accountIDs)),
		"account_share_mode_seat_prepay",
		"account_share_mode_seat_refund",
		"account_share_mode_seat_waiver_refund",
		startTime,
		endTime,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var date string
		var hourlyCost float64
		if err := rows.Scan(&date, &hourlyCost); err != nil {
			return nil, err
		}
		result[date] = hourlyCost
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *usageLogRepository) actualCostSnapshotMetric(userID, apiKeyID, accountID int64) string {
	if accountID > 0 && userID == 0 && apiKeyID == 0 {
		return "account_cost"
	}
	return "actual_cost"
}

func (r *usageLogRepository) queryUsageStatsWithSnapshots(ctx context.Context, filters UsageLogFilters, actualCostMetric string) (*UsageStats, error) {
	if actualCostMetric != "account_cost" {
		actualCostMetric = "actual_cost"
	}
	if filters.StartTime == nil || filters.EndTime == nil || !isUsageSnapshotBusinessFullDayRange(*filters.StartTime, *filters.EndTime) {
		return r.queryUsageStatsLiveOnly(ctx, filters, actualCostMetric)
	}
	rawConditions, rawArgs := buildRawUsageStatsSnapshotConditions(filters)
	snapshotConditions, snapshotArgs := buildSnapshotUsageStatsConditions(filters)
	rawWhere := buildWhere(rawConditions)
	snapshotWhere := buildWhere(snapshotConditions)

	args := make([]any, 0, len(snapshotArgs)+len(rawArgs))
	args = append(args, snapshotArgs...)
	rawQuery := shiftPostgresPlaceholders(fmt.Sprintf(`
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as total_account_cost,
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0) as total_duration_ms
		FROM usage_logs ul
		%s
	`, rawWhere), len(args))
	args = append(args, rawArgs...)

	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(total_requests), 0) as total_requests,
			COALESCE(SUM(total_input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(total_output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(total_cache_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(total_cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(total_cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(total_actual_cost), 0) as total_actual_cost,
			COALESCE(SUM(total_account_cost), 0) as total_account_cost,
			CASE
				WHEN COALESCE(SUM(total_requests), 0) = 0 THEN 0
				ELSE COALESCE(SUM(total_duration_ms), 0)::float / SUM(total_requests)
			END as avg_duration_ms
		FROM (
			SELECT
				total_requests,
				input_tokens as total_input_tokens,
				output_tokens as total_output_tokens,
				(cache_creation_tokens + cache_read_tokens) as total_cache_tokens,
				cache_creation_tokens as total_cache_creation_tokens,
				cache_read_tokens as total_cache_read_tokens,
				total_cost,
				%s as total_actual_cost,
				account_cost as total_account_cost,
				total_duration_ms
			FROM usage_daily_dimension_snapshots s
			%s
			UNION ALL
			%s
		) usage_totals
	`, actualCostMetric, snapshotWhere, rawQuery)

	stats := &UsageStats{}
	var totalAccountCost float64
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		args,
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&totalAccountCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalAccountCost = &totalAccountCost
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return stats, nil
}

func (r *usageLogRepository) queryUsageStatsLiveOnly(ctx context.Context, filters UsageLogFilters, actualCostMetric string) (*UsageStats, error) {
	if actualCostMetric != "account_cost" {
		actualCostMetric = "actual_cost"
	}
	conditions, args := buildLiveUsageStatsConditions(filters)
	actualCostExpr := "COALESCE(SUM(actual_cost), 0)"
	if actualCostMetric == "account_cost" {
		actualCostExpr = "COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0)"
	}
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			%s as total_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as total_account_cost,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM usage_logs
		%s
	`, actualCostExpr, buildWhere(conditions))
	stats := &UsageStats{}
	var totalAccountCost float64
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		args,
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&totalAccountCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalAccountCost = &totalAccountCost
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return stats, nil
}

func (r *usageLogRepository) getDailyUsageTrendWithSnapshots(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) (results []TrendDataPoint, err error) {
	filters := UsageLogFilters{UserID: userID, APIKeyID: apiKeyID, AccountID: accountID, GroupID: groupID, Model: model, RequestType: requestType, Stream: stream, BillingType: billingType, StartTime: &startTime, EndTime: &endTime}
	rawConditions, rawArgs := buildRawUsageStatsSnapshotConditions(filters)
	snapshotConditions, snapshotArgs := buildSnapshotUsageStatsConditions(filters)
	rawWhere := buildWhere(rawConditions)
	snapshotWhere := buildWhere(snapshotConditions)

	args := make([]any, 0, len(snapshotArgs)+len(rawArgs))
	args = append(args, snapshotArgs...)
	rawQuery := shiftPostgresPlaceholders(fmt.Sprintf(`
		SELECT
			TO_CHAR(created_at, 'YYYY-MM-DD') as date,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			COALESCE(SUM(actual_cost), 0) as actual_cost
		FROM usage_logs ul
		%s
		GROUP BY date
	`, rawWhere), len(args))
	args = append(args, rawArgs...)

	query := fmt.Sprintf(`
		SELECT
			date,
			COALESCE(SUM(requests), 0) as requests,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(cost), 0) as cost,
			COALESCE(SUM(actual_cost), 0) as actual_cost
		FROM (
			SELECT
				TO_CHAR(bucket_date::timestamp, 'YYYY-MM-DD') as date,
				total_requests as requests,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) as total_tokens,
				total_cost as cost,
				actual_cost
			FROM usage_daily_dimension_snapshots s
			%s
			UNION ALL
			%s
		) trend
		GROUP BY date
		ORDER BY date ASC
	`, snapshotWhere, rawQuery)

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	return scanTrendRows(rows)
}

func (r *usageLogRepository) getModelStatsWithSnapshots(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, requestType *int16, stream *bool, billingType *int8, source string) (results []ModelStat, err error) {
	filters := UsageLogFilters{UserID: userID, APIKeyID: apiKeyID, AccountID: accountID, GroupID: groupID, RequestType: requestType, Stream: stream, BillingType: billingType, StartTime: &startTime, EndTime: &endTime}
	rawConditions, rawArgs := buildRawUsageStatsSnapshotConditions(filters)
	snapshotConditions, snapshotArgs := buildSnapshotUsageStatsConditions(filters)
	modelExpr := resolveModelDimensionExpression(source)
	snapshotModelExpr := resolveSnapshotModelDimensionExpression(source)
	rawWhere := buildWhere(rawConditions)
	snapshotWhere := buildWhere(snapshotConditions)

	args := make([]any, 0, len(snapshotArgs)+len(rawArgs))
	args = append(args, snapshotArgs...)
	actualCostMetric := r.actualCostSnapshotMetric(userID, apiKeyID, accountID)
	rawActualCostExpr := "COALESCE(SUM(actual_cost), 0) as actual_cost"
	if actualCostMetric == "account_cost" {
		rawActualCostExpr = "COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as actual_cost"
	}
	rawQuery := shiftPostgresPlaceholders(fmt.Sprintf(`
		SELECT
			%s as model,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			%s,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as account_cost
		FROM usage_logs ul
		%s
		GROUP BY %s
	`, modelExpr, rawActualCostExpr, rawWhere, modelExpr), len(args))
	args = append(args, rawArgs...)

	query := fmt.Sprintf(`
		SELECT
			model,
			COALESCE(SUM(requests), 0) as requests,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(cost), 0) as cost,
			COALESCE(SUM(actual_cost), 0) as actual_cost,
			COALESCE(SUM(account_cost), 0) as account_cost
		FROM (
			SELECT
				%s as model,
				total_requests as requests,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) as total_tokens,
				total_cost as cost,
				%s as actual_cost,
				account_cost
			FROM usage_daily_dimension_snapshots s
			%s
			UNION ALL
			%s
		) model_stats
		GROUP BY model
		ORDER BY total_tokens DESC
	`, snapshotModelExpr, actualCostMetric, snapshotWhere, rawQuery)

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()
	return scanModelStatsRows(rows)
}

func (r *usageLogRepository) hydrateUsageLogWalletDeductions(ctx context.Context, logs []service.UsageLog) error {
	if len(logs) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(logs))
	byID := make(map[int64]*service.UsageLog, len(logs))
	for i := range logs {
		if logs[i].ID <= 0 {
			continue
		}
		ids = append(ids, logs[i].ID)
		byID[logs[i].ID] = &logs[i]
	}
	if len(ids) == 0 {
		return nil
	}
	if err := r.hydrateUsageLogPointsDeductions(ctx, byID, ids); err != nil {
		return err
	}
	if err := r.hydrateUsageLogBalanceDeductions(ctx, byID, ids); err != nil {
		return err
	}
	for i := range logs {
		logs[i].BillingWalletType = usageLogWalletType(logs[i])
	}
	return nil
}

func (r *usageLogRepository) hydrateUsageLogPointsDeductions(ctx context.Context, byID map[int64]*service.UsageLog, ids []int64) error {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT ref_id, COALESCE(SUM(amount), 0)::double precision
		FROM points_ledger
		WHERE ref_type = 'usage_log'
			AND reason = 'usage_charge'
			AND direction = 'debit'
			AND ref_id = ANY($1)
		GROUP BY ref_id
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var amount float64
		if err := rows.Scan(&id, &amount); err != nil {
			return err
		}
		if log := byID[id]; log != nil {
			log.PointsDeducted = amount
		}
	}
	return rows.Err()
}

func (r *usageLogRepository) hydrateUsageLogBalanceDeductions(ctx context.Context, byID map[int64]*service.UsageLog, ids []int64) error {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT ref_id, COALESCE(SUM(amount), 0)::double precision
		FROM user_balance_ledger
		WHERE ref_type = 'usage_log'
			AND reason = 'usage_charge'
			AND direction = 'debit'
			AND ref_id = ANY($1)
		GROUP BY ref_id
	`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var amount float64
		if err := rows.Scan(&id, &amount); err != nil {
			return err
		}
		if log := byID[id]; log != nil {
			log.BalanceDeducted = amount
		}
	}
	return rows.Err()
}

func usageLogWalletType(log service.UsageLog) string {
	if log.BillingType == service.BillingTypeSubscription {
		return "subscription"
	}
	hasPoints := log.PointsDeducted > 0
	hasBalance := log.BalanceDeducted > 0
	switch {
	case hasPoints && hasBalance:
		return "mixed"
	case hasPoints:
		return "points"
	case hasBalance:
		return "balance"
	default:
		return "none"
	}
}

func buildWhere(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func appendRequestTypeOrStreamWhereCondition(conditions []string, args []any, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, fmt.Sprintf("stream = $%d", len(args)+1))
		args = append(args, *stream)
	}
	return conditions, args
}

func appendRequestTypeOrStreamQueryFilter(query string, args []any, requestType *int16, stream *bool) (string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		query += " AND " + condition
		args = append(args, conditionArgs...)
		return query, args
	}
	if stream != nil {
		query += fmt.Sprintf(" AND stream = $%d", len(args)+1)
		args = append(args, *stream)
	}
	return query, args
}

func buildLiveUsageStatsConditions(filters UsageLogFilters) ([]string, []any) {
	conditions := make([]string, 0, 10)
	args := make([]any, 0, 10)
	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	conditions, args = appendRawUsageLogModelWhereCondition(conditions, args, filters.Model)
	conditions, args = appendRequestTypeOrStreamWhereCondition(conditions, args, filters.RequestType, filters.Stream)
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	if filters.BillingMode != "" {
		conditions = append(conditions, fmt.Sprintf("billing_mode = $%d", len(args)+1))
		args = append(args, filters.BillingMode)
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}
	return conditions, args
}

func buildRawUsageStatsSnapshotConditions(filters UsageLogFilters) ([]string, []any) {
	conditions := make([]string, 0, 11)
	args := make([]any, 0, 11)
	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	if strings.TrimSpace(filters.Model) != "" {
		conditions = append(conditions, fmt.Sprintf("ul.%s = $%d", rawUsageLogModelColumn, len(args)+1))
		args = append(args, filters.Model)
	}
	conditions, args = appendAliasedRequestTypeOrStreamWhereCondition(conditions, args, "ul", filters.RequestType, filters.Stream)
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("ul.billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	if filters.BillingMode != "" {
		conditions = append(conditions, fmt.Sprintf("ul.billing_mode = $%d", len(args)+1))
		args = append(args, filters.BillingMode)
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("ul.created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("ul.created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}
	conditions = append(conditions, `NOT EXISTS (
		SELECT 1
		FROM usage_daily_dimension_snapshots s
		WHERE s.bucket_date = (ul.created_at AT TIME ZONE 'Asia/Shanghai')::date
	)`)
	return conditions, args
}

func buildSnapshotUsageStatsConditions(filters UsageLogFilters) ([]string, []any) {
	conditions := make([]string, 0, 11)
	args := make([]any, 0, 11)
	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("s.group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	if strings.TrimSpace(filters.Model) != "" {
		conditions = append(conditions, fmt.Sprintf("s.model = $%d", len(args)+1))
		args = append(args, filters.Model)
	}
	if filters.RequestType != nil {
		condition, conditionArgs := buildSnapshotRequestTypeCondition(len(args)+1, *filters.RequestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
	} else if filters.Stream != nil {
		streamState := int16(0)
		if *filters.Stream {
			streamState = 1
		}
		conditions = append(conditions, fmt.Sprintf("s.stream_state = $%d", len(args)+1))
		args = append(args, streamState)
	}
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("s.billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	if filters.BillingMode != "" {
		conditions = append(conditions, fmt.Sprintf("s.billing_mode = $%d", len(args)+1))
		args = append(args, filters.BillingMode)
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("s.bucket_date >= $%d::date", len(args)+1))
		args = append(args, usageSnapshotBusinessDate(*filters.StartTime))
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("s.bucket_date < $%d::date", len(args)+1))
		args = append(args, usageSnapshotBusinessDate(*filters.EndTime))
	}
	return conditions, args
}

func usageSnapshotBusinessLocation() *time.Location {
	loc, err := time.LoadLocation(usageSnapshotBusinessTimezone)
	if err != nil {
		return time.FixedZone(usageSnapshotBusinessTimezone, 8*60*60)
	}
	return loc
}

func usageSnapshotBusinessDate(t time.Time) string {
	return t.In(usageSnapshotBusinessLocation()).Format("2006-01-02")
}

func usageSnapshotBusinessDateRange(start, end time.Time) (string, string) {
	return usageSnapshotBusinessDate(start), usageSnapshotBusinessDate(end)
}

func isUsageSnapshotBusinessFullDayRange(start, end time.Time) bool {
	if !end.After(start) {
		return false
	}
	loc := usageSnapshotBusinessLocation()
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	return startLocal.Hour() == 0 &&
		startLocal.Minute() == 0 &&
		startLocal.Second() == 0 &&
		startLocal.Nanosecond() == 0 &&
		endLocal.Hour() == 0 &&
		endLocal.Minute() == 0 &&
		endLocal.Second() == 0 &&
		endLocal.Nanosecond() == 0
}

func appendAliasedRequestTypeOrStreamWhereCondition(conditions []string, args []any, alias string, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, qualifyUsageLogCondition(condition, alias))
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, fmt.Sprintf("%s.stream = $%d", alias, len(args)+1))
		args = append(args, *stream)
	}
	return conditions, args
}

func qualifyUsageLogCondition(condition string, alias string) string {
	replacer := strings.NewReplacer(
		"request_type", alias+".request_type",
		"stream", alias+".stream",
		"openai_ws_mode", alias+".openai_ws_mode",
	)
	return replacer.Replace(condition)
}

func buildSnapshotRequestTypeCondition(startArgIndex int, requestType int16) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(s.request_type = $%d OR (s.request_type = %d AND s.stream_state = 0))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(s.request_type = $%d OR (s.request_type = %d AND s.stream_state = 1))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	default:
		return fmt.Sprintf("s.request_type = $%d", startArgIndex), []any{requestTypeArg}
	}
}

func resolveSnapshotModelDimensionExpression(modelType string) string {
	requestedExpr := "COALESCE(NULLIF(TRIM(requested_model), ''), model)"
	switch usagestats.NormalizeModelSource(modelType) {
	case usagestats.ModelSourceUpstream:
		return fmt.Sprintf("COALESCE(NULLIF(TRIM(upstream_model), ''), %s)", requestedExpr)
	case usagestats.ModelSourceMapping:
		return fmt.Sprintf("(%s || ' -> ' || COALESCE(NULLIF(TRIM(upstream_model), ''), %s))", requestedExpr, requestedExpr)
	default:
		return requestedExpr
	}
}

func shiftPostgresPlaceholders(query string, offset int) string {
	if offset <= 0 {
		return query
	}
	return placeholderPattern.ReplaceAllStringFunc(query, func(match string) string {
		n, err := strconv.Atoi(strings.TrimPrefix(match, "$"))
		if err != nil {
			return match
		}
		return fmt.Sprintf("$%d", n+offset)
	})
}

// buildRequestTypeFilterCondition 在 request_type 过滤时兼容 legacy 字段，避免历史数据漏查。
func buildRequestTypeFilterCondition(startArgIndex int, requestType int16) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND stream = FALSE AND openai_ws_mode = FALSE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND stream = TRUE AND openai_ws_mode = FALSE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND openai_ws_mode = TRUE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	default:
		return fmt.Sprintf("request_type = $%d", startArgIndex), []any{requestTypeArg}
	}
}
