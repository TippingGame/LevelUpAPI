// Package repository 实现数据访问层（Repository Pattern）。
//
// 该包提供了与数据库交互的所有操作，包括 CRUD、复杂查询和批量操作。
// 采用 Repository 模式将数据访问逻辑与业务逻辑分离，便于测试和维护。
//
// 主要特性：
//   - 使用 Ent ORM 进行类型安全的数据库操作
//   - 对于复杂查询（如批量更新、聚合统计）使用原生 SQL
//   - 提供统一的错误翻译机制，将数据库错误转换为业务错误
//   - 支持软删除，所有查询自动过滤已删除记录
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbaccountgroup "github.com/Wei-Shaw/sub2api/ent/accountgroup"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	dbpredicate "github.com/Wei-Shaw/sub2api/ent/predicate"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
)

// accountRepository 实现 service.AccountRepository 接口。
// 提供 AI API 账户的完整数据访问功能。
//
// 设计说明：
//   - client: Ent 客户端，用于类型安全的 ORM 操作
//   - sql: 原生 SQL 执行器，用于复杂查询和批量操作
//   - schedulerCache: 调度器缓存，用于在账号状态变更时同步快照
type accountRepository struct {
	client *dbent.Client // Ent ORM 客户端
	sql    sqlExecutor   // 原生 SQL 执行接口
	// schedulerCache 用于在账号状态变更时主动同步快照到缓存，
	// 确保粘性会话能及时感知账号不可用状态。
	// Used to proactively sync account snapshot to cache when status changes,
	// ensuring sticky sessions can promptly detect unavailable accounts.
	schedulerCache service.SchedulerCache
}

var schedulerNeutralExtraKeyPrefixes = []string{
	"codex_primary_",
	"codex_secondary_",
	"codex_5h_",
	"codex_7d_",
	"passive_usage_",
}

var schedulerNeutralExtraKeys = map[string]struct{}{
	"codex_usage_updated_at":     {},
	"session_window_utilization": {},
}

var schedulerRelevantExtraKeys = map[string]struct{}{
	"openai_responses_mode":      {},
	"openai_responses_supported": {},
}

const postgresParameterBatchSize = 50000

// NewAccountRepository 创建账户仓储实例。
// 这是对外暴露的构造函数，返回接口类型以便于依赖注入。
func NewAccountRepository(client *dbent.Client, sqlDB *sql.DB, schedulerCache service.SchedulerCache) service.AccountRepository {
	return newAccountRepositoryWithSQL(client, sqlDB, schedulerCache)
}

// newAccountRepositoryWithSQL 是内部构造函数，支持依赖注入 SQL 执行器。
// 这种设计便于单元测试时注入 mock 对象。
func newAccountRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor, schedulerCache service.SchedulerCache) *accountRepository {
	return &accountRepository{client: client, sql: sqlq, schedulerCache: schedulerCache}
}

func translateAccountPersistenceError(err error, notFound *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}
	if isUniqueViolationOnIndex(err, ownedAccountIdentityUniqueIndexSet) {
		return service.ErrOwnedAccountAlreadyExists.WithCause(err)
	}
	return translatePersistenceError(err, notFound, nil)
}

func (r *accountRepository) Create(ctx context.Context, account *service.Account) error {
	if account == nil {
		return service.ErrAccountNilInput
	}

	builder := r.client.Account.Create().
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetLoadFactorPaidCeiling(normalizeLoadFactorPaidCeiling(account.LoadFactorPaidCeiling)).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.PrivatePriority != nil {
		builder.SetPrivatePriority(*account.PrivatePriority)
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}

	account.ID = created.ID
	account.CreatedAt = created.CreatedAt
	account.UpdatedAt = created.UpdatedAt
	if account.Status == service.StatusError {
		if err := r.syncAccountErrorSince(ctx, account.ID, account.Status); err != nil {
			return err
		}
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account create failed: account=%d err=%v", account.ID, err)
	}
	return nil
}

func (r *accountRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	m, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, service.ErrAccountNotFound
	}
	return &accounts[0], nil
}

func (r *accountRepository) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	if len(ids) == 0 {
		return []*service.Account{}, nil
	}

	uniqueIDs := uniquePositiveInt64s(ids)
	if len(uniqueIDs) == 0 {
		return []*service.Account{}, nil
	}

	entAccounts := make([]*dbent.Account, 0, len(uniqueIDs))
	for start := 0; start < len(uniqueIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		batch, err := r.client.Account.
			Query().
			Where(dbaccount.IDIn(uniqueIDs[start:end]...)).
			WithProxy().
			All(ctx)
		if err != nil {
			return nil, err
		}
		entAccounts = append(entAccounts, batch...)
	}
	if len(entAccounts) == 0 {
		return []*service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(entAccounts))
	entByID := make(map[int64]*dbent.Account, len(entAccounts))
	for _, acc := range entAccounts {
		entByID[acc.ID] = acc
		accountIDs = append(accountIDs, acc.ID)
	}

	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	listingIDsByAccount, err := r.loadAccountShareModeListingIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outByID := make(map[int64]*service.Account, len(entAccounts))
	for _, entAcc := range entAccounts {
		out := accountEntityToService(entAcc)
		if out == nil {
			continue
		}

		// Prefer the preloaded proxy edge when available.
		if entAcc.Edges.Proxy != nil {
			out.Proxy = proxyEntityToService(entAcc.Edges.Proxy)
		}

		if groups, ok := groupsByAccount[entAcc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[entAcc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[entAcc.ID]; ok {
			out.AccountGroups = ags
		}
		if listingID, ok := listingIDsByAccount[entAcc.ID]; ok {
			id := listingID
			out.AccountShareModeListingID = &id
		}
		outByID[entAcc.ID] = out
	}

	// Preserve input order (first occurrence), and ignore missing IDs.
	out := make([]*service.Account, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		if _, ok := entByID[id]; !ok {
			continue
		}
		if acc, ok := outByID[id]; ok && acc != nil {
			out = append(out, acc)
		}
	}

	return out, nil
}

// ExistsByID 检查指定 ID 的账号是否存在。
// 相比 GetByID，此方法性能更优，因为：
//   - 使用 Exist() 方法生成 SELECT EXISTS 查询，只返回布尔值
//   - 不加载完整的账号实体及其关联数据（Groups、Proxy 等）
//   - 适用于删除前的存在性检查等只需判断有无的场景
func (r *accountRepository) ExistsByID(ctx context.Context, id int64) (bool, error) {
	exists, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Exist(ctx)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *accountRepository) IsAccountShareModeListingAccount(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM account_share_listings
		WHERE account_id = $1
			AND deleted_at IS NULL
		LIMIT 1
	`, id)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var listingID int64
	if err := rows.Scan(&listingID); err != nil {
		return false, err
	}
	return true, rows.Err()
}

func (r *accountRepository) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*service.Account, error) {
	if crsAccountID == "" {
		return nil, nil
	}

	// 使用 sqljson.ValueEQ 生成 JSON 路径过滤，避免手写 SQL 片段导致语法兼容问题。
	m, err := r.client.Account.Query().
		Where(func(s *entsql.Selector) {
			s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, crsAccountID, sqljson.Path("crs_account_id")))
		}).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	accounts, err := r.accountsToService(ctx, []*dbent.Account{m})
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, nil
	}
	return &accounts[0], nil
}

func (r *accountRepository) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, extra->>'crs_account_id'
		FROM accounts
		WHERE deleted_at IS NULL
			AND extra->>'crs_account_id' IS NOT NULL
			AND extra->>'crs_account_id' != ''
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int64)
	for rows.Next() {
		var id int64
		var crsID string
		if err := rows.Scan(&id, &crsID); err != nil {
			return nil, err
		}
		result[crsID] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *accountRepository) Update(ctx context.Context, account *service.Account) error {
	if account == nil {
		return nil
	}

	builder := applyAccountUpdateFields(r.client.Account.UpdateOneID(account.ID), account)

	updated, err := builder.Save(ctx)
	if err != nil {
		return translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	account.UpdatedAt = updated.UpdatedAt
	if err := r.syncAccountErrorSince(ctx, account.ID, account.Status); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account update failed: account=%d err=%v", account.ID, err)
	}
	if _, err := r.repairOpenAISharedPoolBindings(ctx, []int64{account.ID}); err != nil {
		return err
	}
	// 普通账号编辑（如 model_mapping / credentials）也需要立即刷新单账号快照，
	// 否则网关在 outbox worker 延迟或异常时仍可能读到旧配置。
	r.syncSchedulerAccountSnapshot(ctx, account.ID)
	return nil
}

func applyAccountUpdateFields(builder *dbent.AccountUpdateOne, account *service.Account) *dbent.AccountUpdateOne {
	builder.
		SetName(account.Name).
		SetNillableNotes(account.Notes).
		SetPlatform(account.Platform).
		SetAccountLevel(service.NormalizeAccountLevel(account.AccountLevel)).
		SetType(account.Type).
		SetCredentials(normalizeJSONMap(account.Credentials)).
		SetExtra(normalizeJSONMap(account.Extra)).
		SetShareMode(service.NormalizeAccountShareMode(account.ShareMode)).
		SetShareStatus(service.NormalizeAccountShareStatus(account.ShareStatus)).
		SetConcurrency(account.Concurrency).
		SetLoadFactorPaidCeiling(normalizeLoadFactorPaidCeiling(account.LoadFactorPaidCeiling)).
		SetPriority(account.Priority).
		SetStatus(account.Status).
		SetErrorMessage(account.ErrorMessage).
		SetSchedulable(account.Schedulable).
		SetAutoPauseOnExpired(account.AutoPauseOnExpired)

	if account.RateMultiplier != nil {
		builder.SetRateMultiplier(*account.RateMultiplier)
	}
	if account.PrivatePriority != nil {
		builder.SetPrivatePriority(*account.PrivatePriority)
	} else {
		builder.ClearPrivatePriority()
	}
	if account.LoadFactor != nil {
		builder.SetLoadFactor(*account.LoadFactor)
	} else {
		builder.ClearLoadFactor()
	}
	if account.OwnerUserID != nil {
		builder.SetOwnerUserID(*account.OwnerUserID)
	} else {
		builder.ClearOwnerUserID()
	}
	if account.SharePolicyID != nil {
		builder.SetSharePolicyID(*account.SharePolicyID)
	} else {
		builder.ClearSharePolicyID()
	}

	if account.ProxyID != nil {
		builder.SetProxyID(*account.ProxyID)
	} else {
		builder.ClearProxyID()
	}
	if account.LastUsedAt != nil {
		builder.SetLastUsedAt(*account.LastUsedAt)
	} else {
		builder.ClearLastUsedAt()
	}
	if account.ExpiresAt != nil {
		builder.SetExpiresAt(*account.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}
	if account.RateLimitedAt != nil {
		builder.SetRateLimitedAt(*account.RateLimitedAt)
	} else {
		builder.ClearRateLimitedAt()
	}
	if account.RateLimitResetAt != nil {
		builder.SetRateLimitResetAt(*account.RateLimitResetAt)
	} else {
		builder.ClearRateLimitResetAt()
	}
	if account.OverloadUntil != nil {
		builder.SetOverloadUntil(*account.OverloadUntil)
	} else {
		builder.ClearOverloadUntil()
	}
	if account.SessionWindowStart != nil {
		builder.SetSessionWindowStart(*account.SessionWindowStart)
	} else {
		builder.ClearSessionWindowStart()
	}
	if account.SessionWindowEnd != nil {
		builder.SetSessionWindowEnd(*account.SessionWindowEnd)
	} else {
		builder.ClearSessionWindowEnd()
	}
	if account.SessionWindowStatus != "" {
		builder.SetSessionWindowStatus(account.SessionWindowStatus)
	} else {
		builder.ClearSessionWindowStatus()
	}
	if account.Notes == nil {
		builder.ClearNotes()
	}
	return builder
}

func (r *accountRepository) UpdateOwnedAccountWithLoadFactorCredits(ctx context.Context, ownerUserID int64, account *service.Account) (*service.Account, error) {
	if account == nil {
		return nil, service.ErrAccountNilInput
	}
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if account.LoadFactor == nil || *account.LoadFactor <= 0 || *account.LoadFactor > service.AccountMaxLoadFactor {
		return nil, service.ErrOwnedAccountLoadFactorOutOfRange
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	exec := sqlExecutorFromEntClient(tx.Client())
	if exec == nil {
		return nil, fmt.Errorf("transaction sql executor is unavailable")
	}

	creditsBalance, creditsUsedTotal, err := lockUserLoadFactorCredits(txCtx, exec, ownerUserID)
	if err != nil {
		return nil, err
	}
	dbPaidCeiling, err := lockOwnedAccountLoadFactorCeiling(txCtx, exec, ownerUserID, account.ID)
	if err != nil {
		return nil, err
	}

	targetLoadFactor := *account.LoadFactor
	paidCeiling := normalizeLoadFactorPaidCeiling(dbPaidCeiling)
	charge := targetLoadFactor - paidCeiling
	if charge < 0 {
		charge = 0
	}
	if charge > creditsBalance {
		return nil, service.ErrOwnedAccountLoadFactorCreditsInsufficient.WithMetadata(map[string]string{
			"required": strconv.Itoa(charge),
			"balance":  strconv.Itoa(creditsBalance),
		})
	}

	nextPaidCeiling := paidCeiling
	if targetLoadFactor > nextPaidCeiling {
		nextPaidCeiling = targetLoadFactor
	}
	account.LoadFactorPaidCeiling = nextPaidCeiling

	if charge > 0 {
		if err := debitUserLoadFactorCredits(txCtx, exec, userLoadFactorCreditDebitInput{
			UserID:          ownerUserID,
			AccountID:       account.ID,
			Target:          targetLoadFactor,
			PreviousCeiling: paidCeiling,
			NextCeiling:     nextPaidCeiling,
			Amount:          charge,
			BalanceBefore:   creditsBalance,
			BalanceAfter:    creditsBalance - charge,
			UsedBefore:      creditsUsedTotal,
			UsedAfter:       creditsUsedTotal + charge,
		}); err != nil {
			return nil, err
		}
	}

	updated, err := applyAccountUpdateFields(tx.Client().Account.UpdateOneID(account.ID), account).Save(txCtx)
	if err != nil {
		return nil, translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}
	account.UpdatedAt = updated.UpdatedAt
	if err := r.syncAccountErrorSince(txCtx, account.ID, account.Status); err != nil {
		return nil, err
	}
	if err := enqueueSchedulerOutbox(txCtx, exec, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(account.GroupIDs)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	r.syncSchedulerAccountSnapshot(ctx, account.ID)
	return account, nil
}

type userLoadFactorCreditDebitInput struct {
	UserID          int64
	AccountID       int64
	Target          int
	PreviousCeiling int
	NextCeiling     int
	Amount          int
	BalanceBefore   int
	BalanceAfter    int
	UsedBefore      int
	UsedAfter       int
}

func lockUserLoadFactorCredits(ctx context.Context, exec sqlQueryExecutor, userID int64) (int, int, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT load_factor_credits_balance, load_factor_credits_used_total
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, userID)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, 0, service.ErrUserNotFound
	}
	var balance int
	var usedTotal int
	if err := rows.Scan(&balance, &usedTotal); err != nil {
		return 0, 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return balance, usedTotal, nil
}

func lockOwnedAccountLoadFactorCeiling(ctx context.Context, exec sqlQueryExecutor, ownerUserID, accountID int64) (int, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT load_factor_paid_ceiling
		FROM accounts
		WHERE id = $1
			AND owner_user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, accountID, ownerUserID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, service.ErrAccountNotFound
	}
	var paidCeiling int
	if err := rows.Scan(&paidCeiling); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return paidCeiling, nil
}

func debitUserLoadFactorCredits(ctx context.Context, exec sqlQueryExecutor, in userLoadFactorCreditDebitInput) error {
	if _, err := exec.ExecContext(ctx, `
		UPDATE users
		SET load_factor_credits_balance = $1,
			load_factor_credits_used_total = $2,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
	`, in.BalanceAfter, in.UsedAfter, in.UserID); err != nil {
		return err
	}

	metadata, err := json.Marshal(map[string]any{
		"account_id":       in.AccountID,
		"target":           in.Target,
		"previous_ceiling": in.PreviousCeiling,
		"next_ceiling":     in.NextCeiling,
	})
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO user_load_factor_ledger (
			user_id, account_id, direction, amount, reason,
			balance_before, balance_after, operator_user_id, metadata
		) VALUES (
			$1, $2, 'debit', $3, 'account_load_factor_increase',
			$4, $5, NULL, $6::jsonb
		)
	`, in.UserID, in.AccountID, in.Amount, in.BalanceBefore, in.BalanceAfter, string(metadata))
	return err
}

func (r *accountRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	_, err := r.client.Account.UpdateOneID(id).
		SetCredentials(normalizeJSONMap(credentials)).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if _, err := r.repairOpenAISharedPoolBindings(ctx, []int64{id}); err != nil {
		return err
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) Delete(ctx context.Context, id int64) error {
	groupIDs, err := r.loadAccountGroupIDs(ctx, id)
	if err != nil {
		return err
	}
	// 使用事务保证账号与关联分组的删除原子性
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	var txClient *dbent.Client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
	} else {
		// 已处于外部事务中（ErrTxStarted），复用当前 client
		txClient = r.client
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := txClient.ExecContext(ctx, "DELETE FROM scheduled_test_plans WHERE account_id = $1", id); err != nil {
		return err
	}
	if _, err := txClient.Account.Delete().Where(dbaccount.IDEQ(id)).Exec(ctx); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	r.deleteSchedulerAccountSnapshot(ctx, id)
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue account delete failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) DeleteStaleErrorAccounts(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE status = $1
			AND error_since IS NOT NULL
			AND error_since <= $2
			AND deleted_at IS NULL
		ORDER BY error_since ASC, id ASC
		LIMIT $3
	`, service.StatusError, cutoff, limit)
	if err != nil {
		return 0, err
	}

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var deleted int64
	for _, id := range ids {
		removed, err := r.deleteStaleErrorAccount(ctx, id, cutoff)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
		}
	}
	return deleted, nil
}

func (r *accountRepository) deleteStaleErrorAccount(ctx context.Context, id int64, cutoff time.Time) (bool, error) {
	groupIDs, err := r.loadAccountGroupIDs(ctx, id)
	if err != nil {
		return false, err
	}

	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND status = $2
			AND error_since IS NOT NULL
			AND error_since <= $3
			AND deleted_at IS NULL
	`, id, service.StatusError, cutoff)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if _, err := r.sql.ExecContext(ctx, "DELETE FROM scheduled_test_plans WHERE account_id = $1", id); err != nil {
		return false, err
	}

	r.deleteSchedulerAccountSnapshot(ctx, id)
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue stale error account delete failed: account=%d err=%v", id, err)
	}
	return true, nil
}

func (r *accountRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.Account, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "", "", "", 0, 0, "")
}

func (r *accountRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	return r.listWithFilters(ctx, params, nil, platform, accountType, status, search, ownerSearch, groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListOwnedWithFilters(ctx context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	if ownerUserID <= 0 {
		return nil, nil, service.ErrUserNotFound
	}
	if _, err := r.repairQuotaPoolOwnerOpenAISharedPoolBindings(ctx, ownerUserID); err != nil {
		return nil, nil, err
	}
	return r.listWithFilters(ctx, params, &ownerUserID, platform, accountType, status, search, "", groupID, proxyID, privacyMode)
}

func (r *accountRepository) ListQuotaPoolAccounts(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("account repository sql executor is unavailable")
	}

	if _, err := r.repairQuotaPoolVisibleOpenAISharedPoolBindings(ctx, ownerUserID); err != nil {
		return nil, err
	}

	accounts, err := r.listQuotaPoolAccountRows(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}
	if err := r.loadQuotaPoolAccountProxies(ctx, accounts); err != nil {
		return nil, err
	}
	if err := r.loadQuotaPoolAccountGroupRows(ctx, ownerUserID, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) RepairQuotaPoolOwnerOpenAISharedPoolBindings(ctx context.Context, ownerUserID int64) (bool, error) {
	return r.repairQuotaPoolOwnerOpenAISharedPoolBindings(ctx, ownerUserID)
}

func (r *accountRepository) RepairQuotaPoolVisibleOpenAISharedPoolBindings(ctx context.Context, ownerUserID int64) (bool, error) {
	return r.repairQuotaPoolVisibleOpenAISharedPoolBindings(ctx, ownerUserID)
}

func (r *accountRepository) RepairAllVisibleOpenAISharedPoolBindings(ctx context.Context) (bool, error) {
	return r.repairAllVisibleOpenAISharedPoolBindings(ctx)
}

func (r *accountRepository) EnsureOpenAIProSharedPoolForAccount(ctx context.Context, accountID int64) (bool, error) {
	if r == nil || r.sql == nil || accountID <= 0 {
		return false, nil
	}
	groupIDs, err := ensureOpenAIProSharedPoolForAccounts(ctx, r.sql, []int64{accountID}, true)
	if err != nil {
		return false, err
	}
	if len(groupIDs) == 0 {
		return false, nil
	}
	for _, groupID := range uniquePositiveInt64s(groupIDs) {
		gid := groupID
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &gid, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue pro shared pool ensure failed: group=%d err=%v", gid, err)
		}
	}
	return true, nil
}

func (r *accountRepository) repairQuotaPoolOwnerOpenAISharedPoolBindings(ctx context.Context, ownerUserID int64) (bool, error) {
	if r == nil || r.sql == nil || ownerUserID <= 0 {
		return false, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE deleted_at IS NULL
			AND owner_user_id = $1
			AND platform = 'openai'
			AND type = 'oauth'
			AND lower(btrim(COALESCE(share_mode, ''))) = 'public'
			AND lower(btrim(COALESCE(share_status, ''))) NOT IN ('pending', 'suspended')
	`, ownerUserID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	accountIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return false, err
		}
		accountIDs = append(accountIDs, id)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(accountIDs) == 0 {
		return false, nil
	}
	return r.repairOpenAISharedPoolBindings(ctx, accountIDs)
}

func (r *accountRepository) repairQuotaPoolVisibleOpenAISharedPoolBindings(ctx context.Context, ownerUserID int64) (bool, error) {
	if ownerUserID <= 0 {
		return false, nil
	}
	return r.repairAllVisibleOpenAISharedPoolBindings(ctx)
}

func (r *accountRepository) repairAllVisibleOpenAISharedPoolBindings(ctx context.Context) (bool, error) {
	if r == nil || r.sql == nil {
		return false, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT DISTINCT a.id
		FROM accounts a
		WHERE a.deleted_at IS NULL
			AND a.platform = 'openai'
			AND a.type = 'oauth'
			AND a.owner_user_id IS NOT NULL
			AND lower(btrim(COALESCE(a.share_mode, ''))) = 'public'
			AND lower(btrim(COALESCE(a.share_status, ''))) NOT IN ('pending', 'suspended')
	`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	accountIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return false, err
		}
		accountIDs = append(accountIDs, id)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(accountIDs) == 0 {
		return false, nil
	}
	return r.repairOpenAISharedPoolBindings(ctx, accountIDs)
}

func (r *accountRepository) listQuotaPoolAccountRows(ctx context.Context, ownerUserID int64) ([]service.Account, error) {
	rows, err := r.sql.QueryContext(ctx, fmt.Sprintf(`
		WITH quota_pool_account_ids AS (
			SELECT id
			FROM accounts
			WHERE deleted_at IS NULL
				AND owner_user_id = $1
			UNION
			SELECT a.id
			FROM account_groups ag
			JOIN groups g ON g.id = ag.group_id
			JOIN accounts a ON a.id = ag.account_id
			WHERE a.deleted_at IS NULL
				AND g.deleted_at IS NULL
				AND g.is_exclusive = false
				AND g.owner_user_id IS NULL
				AND lower(btrim(COALESCE(g.scope, ''))) = 'public'
				AND lower(btrim(COALESCE(g.subscription_type, ''))) IN ('', 'standard')
				AND (
					a.owner_user_id IS NULL
					OR (
						a.owner_user_id IS NOT NULL
						AND lower(btrim(COALESCE(a.share_mode, ''))) = 'public'
						AND lower(btrim(COALESCE(a.share_status, ''))) NOT IN ('pending', 'suspended')
					)
				)
		)
		SELECT
			a.id,
			a.name,
			a.platform,
			a.account_level,
			%s AS effective_plan_token,
			a.credentials->>'plan_type',
			a.credentials->>'chatgpt_plan_type',
			a.credentials->>'subscription_plan',
			a.extra->>'plan_type',
			a.extra->>'chatgpt_plan_type',
			a.extra->>'subscription_plan',
			a.type,
			a.extra->>'quota_limit',
			a.extra->>'quota_used',
			a.extra->>'quota_daily_limit',
			a.extra->>'quota_daily_used',
			a.extra->>'quota_daily_start',
			a.extra->>'quota_daily_reset_mode',
			a.extra->>'quota_daily_reset_at',
			a.extra->>'quota_weekly_limit',
			a.extra->>'quota_weekly_used',
			a.extra->>'quota_weekly_start',
			a.extra->>'quota_weekly_reset_mode',
			a.extra->>'quota_weekly_reset_at',
			a.extra->>'codex_5h_used_percent',
			a.extra->>'codex_5h_reset_after_seconds',
			a.extra->>'codex_5h_reset_at',
			a.extra->>'codex_5h_limit_percent',
			a.extra->>'codex_7d_used_percent',
			a.extra->>'codex_7d_reset_after_seconds',
			a.extra->>'codex_7d_reset_at',
			a.extra->>'codex_7d_limit_percent',
			a.extra->>'codex_usage_updated_at',
			a.extra->>'privacy_mode',
			a.proxy_id,
			a.owner_user_id,
			a.share_mode,
			a.share_status,
			a.concurrency,
			a.status,
			a.expires_at,
			a.auto_pause_on_expired,
			a.schedulable,
			a.rate_limit_reset_at,
			a.overload_until,
			a.temp_unschedulable_until,
			a.temp_unschedulable_reason
		FROM accounts a
		JOIN quota_pool_account_ids q ON q.id = a.id
		ORDER BY a.id
	`, openAIPlanTokenSQL("a.credentials", "a.extra")), ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]service.Account, 0)
	for rows.Next() {
		var account service.Account
		var ownerUserID sql.NullInt64
		var quotaLimit, quotaUsed, quotaDailyLimit, quotaDailyUsed sql.NullString
		var quotaDailyStart, quotaDailyResetMode, quotaDailyResetAt sql.NullString
		var quotaWeeklyLimit, quotaWeeklyUsed, quotaWeeklyStart sql.NullString
		var quotaWeeklyResetMode, quotaWeeklyResetAt sql.NullString
		var codex5hUsedPercent, codex5hResetAfterSeconds, codex5hResetAt, codex5hLimitPercent sql.NullString
		var codex7dUsedPercent, codex7dResetAfterSeconds, codex7dResetAt, codex7dLimitPercent sql.NullString
		var codexUsageUpdatedAt, privacyMode sql.NullString
		var effectivePlanToken sql.NullString
		var credentialPlanType, credentialChatGPTPlanType, credentialSubscriptionPlan sql.NullString
		var extraPlanType, extraChatGPTPlanType, extraSubscriptionPlan sql.NullString
		var tempUnschedulableReason sql.NullString
		var proxyID sql.NullInt64
		if err := rows.Scan(
			&account.ID,
			&account.Name,
			&account.Platform,
			&account.AccountLevel,
			&effectivePlanToken,
			&credentialPlanType,
			&credentialChatGPTPlanType,
			&credentialSubscriptionPlan,
			&extraPlanType,
			&extraChatGPTPlanType,
			&extraSubscriptionPlan,
			&account.Type,
			&quotaLimit,
			&quotaUsed,
			&quotaDailyLimit,
			&quotaDailyUsed,
			&quotaDailyStart,
			&quotaDailyResetMode,
			&quotaDailyResetAt,
			&quotaWeeklyLimit,
			&quotaWeeklyUsed,
			&quotaWeeklyStart,
			&quotaWeeklyResetMode,
			&quotaWeeklyResetAt,
			&codex5hUsedPercent,
			&codex5hResetAfterSeconds,
			&codex5hResetAt,
			&codex5hLimitPercent,
			&codex7dUsedPercent,
			&codex7dResetAfterSeconds,
			&codex7dResetAt,
			&codex7dLimitPercent,
			&codexUsageUpdatedAt,
			&privacyMode,
			&proxyID,
			&ownerUserID,
			&account.ShareMode,
			&account.ShareStatus,
			&account.Concurrency,
			&account.Status,
			&account.ExpiresAt,
			&account.AutoPauseOnExpired,
			&account.Schedulable,
			&account.RateLimitResetAt,
			&account.OverloadUntil,
			&account.TempUnschedulableUntil,
			&tempUnschedulableReason,
		); err != nil {
			return nil, err
		}
		if ownerUserID.Valid {
			account.OwnerUserID = &ownerUserID.Int64
		}
		if proxyID.Valid {
			account.ProxyID = &proxyID.Int64
		}
		account.ShareMode = service.NormalizeAccountShareMode(account.ShareMode)
		account.ShareStatus = service.NormalizeAccountShareStatus(account.ShareStatus)
		account.TempUnschedulableReason = tempUnschedulableReason.String
		account.Credentials = map[string]any{}
		account.Extra = map[string]any{}
		setNullStringExtra(account.Credentials, "plan_type", credentialPlanType)
		setNullStringExtra(account.Credentials, "chatgpt_plan_type", credentialChatGPTPlanType)
		setNullStringExtra(account.Credentials, "subscription_plan", credentialSubscriptionPlan)
		setNullStringExtra(account.Extra, "plan_type", extraPlanType)
		setNullStringExtra(account.Extra, "chatgpt_plan_type", extraChatGPTPlanType)
		setNullStringExtra(account.Extra, "subscription_plan", extraSubscriptionPlan)
		setNullStringExtra(account.Credentials, "plan_type", effectivePlanToken)
		account.AccountLevel = service.NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra)
		setNullStringExtra(account.Extra, "quota_limit", quotaLimit)
		setNullStringExtra(account.Extra, "quota_used", quotaUsed)
		setNullStringExtra(account.Extra, "quota_daily_limit", quotaDailyLimit)
		setNullStringExtra(account.Extra, "quota_daily_used", quotaDailyUsed)
		setNullStringExtra(account.Extra, "quota_daily_start", quotaDailyStart)
		setNullStringExtra(account.Extra, "quota_daily_reset_mode", quotaDailyResetMode)
		setNullStringExtra(account.Extra, "quota_daily_reset_at", quotaDailyResetAt)
		setNullStringExtra(account.Extra, "quota_weekly_limit", quotaWeeklyLimit)
		setNullStringExtra(account.Extra, "quota_weekly_used", quotaWeeklyUsed)
		setNullStringExtra(account.Extra, "quota_weekly_start", quotaWeeklyStart)
		setNullStringExtra(account.Extra, "quota_weekly_reset_mode", quotaWeeklyResetMode)
		setNullStringExtra(account.Extra, "quota_weekly_reset_at", quotaWeeklyResetAt)
		setNullStringExtra(account.Extra, "codex_5h_used_percent", codex5hUsedPercent)
		setNullStringExtra(account.Extra, "codex_5h_reset_after_seconds", codex5hResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_5h_reset_at", codex5hResetAt)
		setNullStringExtra(account.Extra, "codex_5h_limit_percent", codex5hLimitPercent)
		setNullStringExtra(account.Extra, "codex_7d_used_percent", codex7dUsedPercent)
		setNullStringExtra(account.Extra, "codex_7d_reset_after_seconds", codex7dResetAfterSeconds)
		setNullStringExtra(account.Extra, "codex_7d_reset_at", codex7dResetAt)
		setNullStringExtra(account.Extra, "codex_7d_limit_percent", codex7dLimitPercent)
		setNullStringExtra(account.Extra, "codex_usage_updated_at", codexUsageUpdatedAt)
		setNullStringExtra(account.Extra, "privacy_mode", privacyMode)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) loadQuotaPoolAccountProxies(ctx context.Context, accounts []service.Account) error {
	proxyIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		if accounts[i].ProxyID != nil {
			proxyIDs = append(proxyIDs, *accounts[i].ProxyID)
		}
	}
	proxies, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ProxyID == nil {
			continue
		}
		if proxy, ok := proxies[*accounts[i].ProxyID]; ok {
			accounts[i].Proxy = proxy
		}
	}
	return nil
}

func (r *accountRepository) loadQuotaPoolAccountGroupRows(ctx context.Context, ownerUserID int64, accounts []service.Account) error {
	byID := make(map[int64]*service.Account, len(accounts))
	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = &accounts[i]
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			ag.account_id,
			ag.group_id,
			ag.priority,
			ag.created_at,
			g.name,
			g.platform,
			g.rate_multiplier,
			g.is_exclusive,
			g.status,
			g.owner_user_id,
			g.scope,
			g.subscription_type,
			g.required_account_level,
			g.require_oauth_only,
			g.require_privacy_set
		FROM account_groups ag
		JOIN groups g ON g.id = ag.group_id
		WHERE ag.account_id = ANY($1)
			AND g.deleted_at IS NULL
		ORDER BY ag.account_id, ag.priority
	`, pq.Array(accountIDs))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var group service.Group
		var accountGroup service.AccountGroup
		var groupOwnerUserID sql.NullInt64
		if err := rows.Scan(
			&accountID,
			&group.ID,
			&accountGroup.Priority,
			&accountGroup.CreatedAt,
			&group.Name,
			&group.Platform,
			&group.RateMultiplier,
			&group.IsExclusive,
			&group.Status,
			&groupOwnerUserID,
			&group.Scope,
			&group.SubscriptionType,
			&group.RequiredAccountLevel,
			&group.RequireOAuthOnly,
			&group.RequirePrivacySet,
		); err != nil {
			return err
		}
		if groupOwnerUserID.Valid {
			group.OwnerUserID = &groupOwnerUserID.Int64
		}
		group.Hydrated = true
		group.Scope = service.NormalizeGroupScope(group.Scope)
		group.SubscriptionType = service.NormalizeSubscriptionType(group.SubscriptionType)
		group.RequiredAccountLevel = service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)

		account, ok := byID[accountID]
		if !ok {
			continue
		}
		accountGroup.AccountID = accountID
		accountGroup.GroupID = group.ID
		accountGroup.Group = &group
		account.AccountGroups = append(account.AccountGroups, accountGroup)
		account.GroupIDs = append(account.GroupIDs, group.ID)
		account.Groups = append(account.Groups, &group)
	}
	return rows.Err()
}

func setNullStringExtra(extra map[string]any, key string, value sql.NullString) {
	if !value.Valid || value.String == "" {
		return
	}
	extra[key] = value.String
}

const (
	accountRepoNumericTextPattern = `^\s*[+-]?(\d+(\.\d+)?|\.\d+)\s*$`
	accountRepoRFC3339TextPattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`
)

func accountTempUnschedulableInactivePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C(dbaccount.FieldTempUnschedulableUntil)
		s.Where(entsql.Or(
			accountIgnoresLocalSystemErrorStateSQL(s),
			accountOpenAIOAuthRelayPoolTempUnschedIgnoredSQL(s),
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func accountRateLimitInactivePredicate() dbpredicate.Account {
	return accountLocalSystemStateInactivePredicate(dbaccount.FieldRateLimitResetAt)
}

func accountOverloadInactivePredicate() dbpredicate.Account {
	return accountLocalSystemStateInactivePredicate(dbaccount.FieldOverloadUntil)
}

func accountRateLimitActivePredicate() dbpredicate.Account {
	return accountLocalSystemStateActivePredicate(dbaccount.FieldRateLimitResetAt)
}

func accountTempUnschedulableActivePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C(dbaccount.FieldTempUnschedulableUntil)
		s.Where(entsql.And(
			entsql.Not(accountIgnoresLocalSystemErrorStateSQL(s)),
			entsql.Not(accountOpenAIOAuthRelayPoolTempUnschedIgnoredSQL(s)),
			entsql.Not(entsql.IsNull(col)),
			entsql.GT(col, entsql.Expr("NOW()")),
		))
	})
}

func accountLocalSystemStateInactivePredicate(field string) dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C(field)
		s.Where(entsql.Or(
			accountIgnoresLocalSystemErrorStateSQL(s),
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func accountLocalSystemStateActivePredicate(field string) dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C(field)
		s.Where(entsql.And(
			entsql.Not(accountIgnoresLocalSystemErrorStateSQL(s)),
			entsql.Not(entsql.IsNull(col)),
			entsql.GT(col, entsql.Expr("NOW()")),
		))
	})
}

func accountIgnoresLocalSystemErrorStateSQL(s *entsql.Selector) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		typeCol := s.C(dbaccount.FieldType)
		credentialsCol := s.C(dbaccount.FieldCredentials)
		b.WriteString("(")
		b.Ident(typeCol).
			WriteString(" IN (").
			Arg(service.AccountTypeAPIKey).
			WriteString(", ").
			Arg(service.AccountTypeBedrock).
			WriteString(")")
		b.WriteString(" AND LOWER(COALESCE(")
		b.Ident(credentialsCol).WriteString(" ->> ").Arg("pool_mode")
		b.WriteString(", ").Arg("").WriteString(")) = ").Arg("true")
		b.WriteString(" AND NOT ")
		accountCustomErrorPolicyActiveEntSQL(b, credentialsCol)
		b.WriteString(")")
	})
}

func accountOpenAIOAuthRelayPoolTempUnschedIgnoredSQL(s *entsql.Selector) *entsql.Predicate {
	return entsql.P(func(b *entsql.Builder) {
		platformCol := s.C(dbaccount.FieldPlatform)
		typeCol := s.C(dbaccount.FieldType)
		reasonCol := s.C(dbaccount.FieldTempUnschedulableReason)
		b.WriteString("(")
		b.Ident(platformCol).WriteString(" = ").Arg(service.PlatformOpenAI)
		b.WriteString(" AND ")
		b.Ident(typeCol).WriteString(" = ").Arg(service.AccountTypeOAuth)
		b.WriteString(" AND (")
		b.WriteString("COALESCE(").Ident(reasonCol).WriteString(", ").Arg("").WriteString(") LIKE ").Arg(`%"matched_keyword":"` + service.TempUnschedKeywordUpstreamRelayPoolUnavailable + `"%`)
		b.WriteString(" OR ")
		b.WriteString("COALESCE(").Ident(reasonCol).WriteString(", ").Arg("").WriteString(") ILIKE ").Arg("%upstream relay pool unavailable%")
		b.WriteString("))")
	})
}

func openAISharedPoolEffectiveAccountLevelPredicate(requiredLevel string) dbpredicate.Account {
	required := service.NormalizeOpenAISharedPoolRequiredLevel(requiredLevel)
	return dbpredicate.Account(func(s *entsql.Selector) {
		if required == "" || service.OpenAISharedPoolLevelRank(required) == 0 {
			s.Where(entsql.False())
			return
		}
		accountLevelCol := s.C(dbaccount.FieldAccountLevel)
		credentialsCol := s.C(dbaccount.FieldCredentials)
		extraCol := s.C(dbaccount.FieldExtra)
		s.Where(entsql.P(func(b *entsql.Builder) {
			writeOpenAISharedPoolEffectiveAccountLevelEntSQL(b, accountLevelCol, credentialsCol, extraCol)
			b.WriteString(" = ").Arg(required)
		}))
	})
}

func writeOpenAISharedPoolEffectiveAccountLevelEntSQL(b *entsql.Builder, accountLevelCol, credentialsCol, extraCol string) {
	b.WriteString("(CASE WHEN (")
	b.Ident(accountLevelCol).WriteString(" = ").Arg(service.AccountLevelTeam)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" IN (").Arg(service.AccountLevelTeam).WriteString(", ").Arg("chatgptteam").WriteString(")")
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" LIKE ").Arg("team%")
	b.WriteString(") THEN ").Arg(service.AccountLevelTeam)
	b.WriteString(" WHEN (")
	b.Ident(accountLevelCol).WriteString(" = ").Arg(service.AccountLevelPro)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" = ").Arg(service.AccountLevelPro)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" = ").Arg("chatgptpro")
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" LIKE ").Arg("pro%")
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" LIKE ").Arg("chatgptpro%")
	b.WriteString(") THEN ").Arg(service.AccountLevelPro)
	b.WriteString(" WHEN (")
	b.Ident(accountLevelCol).WriteString(" = ").Arg(service.AccountLevelPlus)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" = ").Arg(service.AccountLevelPlus)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" = ").Arg("chatgptplus")
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" LIKE ").Arg("plus%")
	b.WriteString(") THEN ").Arg(service.AccountLevelPlus)
	b.WriteString(" WHEN (")
	b.Ident(accountLevelCol).WriteString(" = ").Arg(service.AccountLevelFree)
	b.WriteString(" OR ")
	writeOpenAIPlanTokenEntSQL(b, credentialsCol, extraCol)
	b.WriteString(" IN (").Arg(service.AccountLevelFree).WriteString(", ").Arg("chatgptfree").WriteString(")")
	b.WriteString(") THEN ").Arg(service.AccountLevelFree)
	b.WriteString(" ELSE ").Arg(service.AccountLevelFree).WriteString(" END)")
}

func writeOpenAIPlanTokenEntSQL(b *entsql.Builder, credentialsCol, extraCol string) {
	b.WriteString(openAIPlanTokenSQL(credentialsCol, extraCol))
}

func openAIPlanTokenSQL(credentialsExpr, extraExpr string) string {
	rows := openAIPlanCandidateValueRowsSQL(credentialsExpr, 1)
	rows = append(rows, openAIPlanCandidateValueRowsSQL(extraExpr, 101)...)
	return fmt.Sprintf(`(
	SELECT COALESCE((
		SELECT token
		FROM (
			SELECT
				token,
				CASE
					WHEN token IN ('team', 'chatgptteam') OR token LIKE 'team%%' THEN 4
					WHEN token = 'pro' OR token = 'chatgptpro' OR token LIKE 'pro%%' OR token LIKE 'chatgptpro%%' THEN 3
					WHEN token = 'plus' OR token = 'chatgptplus' OR token LIKE 'plus%%' THEN 2
					WHEN token IN ('free', 'chatgptfree') THEN 1
					ELSE 0
				END AS rank,
				ord
			FROM (VALUES
				%s
			) AS raw_plan(ord, raw_token)
			CROSS JOIN LATERAL (
				SELECT regexp_replace(lower(btrim(COALESCE(raw_token, ''))), '[[:space:]_-]+', '', 'g') AS token
			) normalized_plan
			WHERE token <> ''
		) ranked_plan
		ORDER BY CASE WHEN rank > 0 THEN 0 ELSE 1 END, rank DESC, ord
		LIMIT 1
	), '')
)`, strings.Join(rows, ",\n\t\t\t\t"))
}

func openAIPlanCandidateValueRowsSQL(jsonExpr string, startOrder int) []string {
	jsonValue := fmt.Sprintf("COALESCE(%s, '{}'::jsonb)", jsonExpr)
	selectedAccountID := fmt.Sprintf(
		"COALESCE(NULLIF((%[1]s)->>'chatgpt_account_id', ''), NULLIF((%[1]s)->>'organization_id', ''), NULLIF((%[1]s)->>'account_id', ''), '')",
		jsonValue,
	)
	accountInfoJSON := fmt.Sprintf("COALESCE((%s)->'account_info', '{}'::jsonb)", jsonValue)
	selectedAccountInfoID := fmt.Sprintf(
		"COALESCE(NULLIF((%[1]s)->>'chatgpt_account_id', ''), NULLIF((%[1]s)->>'organization_id', ''), NULLIF((%[1]s)->>'account_id', ''), %[2]s)",
		accountInfoJSON,
		selectedAccountID,
	)
	exprs := []string{
		fmt.Sprintf("(%s)->>'plan_type'", jsonValue),
		fmt.Sprintf("(%s)->>'chatgpt_plan_type'", jsonValue),
		fmt.Sprintf("(%s)->>'subscription_plan'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account,plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account,chatgpt_plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account,subscription_plan}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{entitlement,plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{entitlement,chatgpt_plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{entitlement,subscription_plan}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,chatgpt_plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,subscription_plan}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,account,plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,account,chatgpt_plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,account,subscription_plan}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,entitlement,plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,entitlement,chatgpt_plan_type}'", jsonValue),
		fmt.Sprintf("(%s) #>> '{account_info,entitlement,subscription_plan}'", jsonValue),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'plan_type')", jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'chatgpt_plan_type')", jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'subscription_plan')", jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'plan_type')", jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'chatgpt_plan_type')", jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'subscription_plan')", jsonValue, selectedAccountID),
		openAIAccountsFallbackPlanSQL(jsonValue, selectedAccountID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'plan_type')", accountInfoJSON, selectedAccountInfoID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'chatgpt_plan_type')", accountInfoJSON, selectedAccountInfoID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'account', 'subscription_plan')", accountInfoJSON, selectedAccountInfoID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'plan_type')", accountInfoJSON, selectedAccountInfoID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'chatgpt_plan_type')", accountInfoJSON, selectedAccountInfoID),
		fmt.Sprintf("jsonb_extract_path_text((%s)->'accounts', %s, 'entitlement', 'subscription_plan')", accountInfoJSON, selectedAccountInfoID),
		openAIAccountsFallbackPlanSQL(accountInfoJSON, selectedAccountInfoID),
	}
	rows := make([]string, 0, len(exprs))
	for i, expr := range exprs {
		rows = append(rows, fmt.Sprintf("(%d, %s)", startOrder+i, expr))
	}
	return rows
}

func openAIAccountsFallbackPlanSQL(jsonValue, selectedAccountID string) string {
	query := `(
	SELECT raw_token
	FROM (
		SELECT account_entry.value #>> '{account,plan_type}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
		UNION ALL
		SELECT account_entry.value #>> '{account,chatgpt_plan_type}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
		UNION ALL
		SELECT account_entry.value #>> '{account,subscription_plan}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
		UNION ALL
		SELECT account_entry.value #>> '{entitlement,plan_type}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
		UNION ALL
		SELECT account_entry.value #>> '{entitlement,chatgpt_plan_type}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
		UNION ALL
		SELECT account_entry.value #>> '{entitlement,subscription_plan}' AS raw_token,
			CASE WHEN lower(btrim(COALESCE(account_entry.value #>> '{account,is_default}', ''))) = 'true' THEN 0 ELSE 1 END AS account_ord
		FROM jsonb_each({{ACCOUNTS_JSON}}) AS account_entry(key, value)
	) account_plan
	CROSS JOIN LATERAL (
		SELECT regexp_replace(lower(btrim(COALESCE(raw_token, ''))), '[[:space:]_-]+', '', 'g') AS token
	) normalized_plan
	WHERE {{SELECTED_ACCOUNT_ID}} = ''
		AND token <> ''
	ORDER BY
		account_ord,
		CASE
			WHEN token IN ('team', 'chatgptteam') OR token LIKE 'team%' THEN 4
			WHEN token = 'pro' OR token = 'chatgptpro' OR token LIKE 'pro%' OR token LIKE 'chatgptpro%' THEN 3
			WHEN token = 'plus' OR token = 'chatgptplus' OR token LIKE 'plus%' THEN 2
			WHEN token IN ('free', 'chatgptfree') THEN 1
			ELSE 0
		END DESC
	LIMIT 1
)`
	accountsJSON := fmt.Sprintf("(CASE WHEN jsonb_typeof((%[1]s)->'accounts') = 'object' THEN (%[1]s)->'accounts' ELSE '{}'::jsonb END)", jsonValue)
	query = strings.ReplaceAll(query, "{{JSON_VALUE}}", jsonValue)
	query = strings.ReplaceAll(query, "{{ACCOUNTS_JSON}}", accountsJSON)
	query = strings.ReplaceAll(query, "{{SELECTED_ACCOUNT_ID}}", selectedAccountID)
	return query
}

func accountCustomErrorPolicyActiveEntSQL(b *entsql.Builder, credentialsCol string) {
	b.WriteString("(")
	b.WriteString("LOWER(COALESCE(")
	b.Ident(credentialsCol).WriteString(" ->> ").Arg("custom_error_codes_enabled")
	b.WriteString(", ").Arg("").WriteString(")) = ").Arg("true")
	b.WriteString(" AND ")
	accountCustomErrorPolicyValueActiveEntSQL(b, func(b *entsql.Builder) {
		b.Ident(credentialsCol).WriteString(" -> ").Arg("custom_error_codes")
	})
	b.WriteString(")")
}

type entSQLWriter func(*entsql.Builder)

func accountCustomErrorPolicyValueActiveEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	b.WriteString("CASE jsonb_typeof(")
	writeValue(b)
	b.WriteString(") WHEN ").Arg("array").WriteString(" THEN EXISTS (SELECT 1 FROM jsonb_array_elements(")
	writeValue(b)
	b.WriteString(") AS custom_error_code(value) WHERE ")
	accountCustomErrorPolicyScalarActiveEntSQL(b, func(b *entsql.Builder) {
		b.Ident("custom_error_code").WriteString(".").Ident("value")
	})
	b.WriteString(") WHEN ").Arg("string").WriteString(" THEN ")
	accountCustomErrorPolicyStringActiveEntSQL(b, writeValue)
	b.WriteString(" WHEN ").Arg("number").WriteString(" THEN ")
	accountCustomErrorPolicyNumberActiveEntSQL(b, writeValue)
	b.WriteString(" ELSE FALSE END")
}

func accountCustomErrorPolicyScalarActiveEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	b.WriteString("CASE jsonb_typeof(")
	writeValue(b)
	b.WriteString(") WHEN ").Arg("string").WriteString(" THEN ")
	accountCustomErrorPolicyStringActiveEntSQL(b, writeValue)
	b.WriteString(" WHEN ").Arg("number").WriteString(" THEN ")
	accountCustomErrorPolicyNumberActiveEntSQL(b, writeValue)
	b.WriteString(" ELSE FALSE END")
}

func accountCustomErrorPolicyNumberActiveEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	b.WriteString("(")
	writeCustomErrorPolicyJSONTextEntSQL(b, writeValue)
	b.WriteString(" ~ ").Arg(`^[0-9]+(\.[0-9]+)?$`)
	b.WriteString(" AND (")
	writeCustomErrorPolicyJSONTextEntSQL(b, writeValue)
	b.WriteString(")::numeric >= 100 AND (")
	writeCustomErrorPolicyJSONTextEntSQL(b, writeValue)
	b.WriteString(")::numeric < 600)")
}

func accountCustomErrorPolicyStringActiveEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	b.WriteString("EXISTS (SELECT 1 FROM regexp_split_to_table(")
	writeCustomErrorPolicyNormalizedStringEntSQL(b, writeValue)
	b.WriteString(", ").Arg(",").WriteString(") AS custom_error_token(token) WHERE ")
	accountCustomErrorPolicyStringTokenActiveEntSQL(b, func(b *entsql.Builder) {
		b.Ident("custom_error_token").WriteString(".").Ident("token")
	})
	b.WriteString(")")
}

func accountCustomErrorPolicyStringTokenActiveEntSQL(b *entsql.Builder, writeToken entSQLWriter) {
	b.WriteString("(")
	writeToken(b)
	b.WriteString(" <> ").Arg("")
	b.WriteString(" AND ((")
	writeToken(b)
	b.WriteString(" ~ ").Arg("^[0-9]+$")
	b.WriteString(" AND (")
	writeToken(b)
	b.WriteString(")::numeric BETWEEN 100 AND 599) OR (")
	writeToken(b)
	b.WriteString(" ~ ").Arg("^[0-9]+-[0-9]+$")
	b.WriteString(" AND (split_part(")
	writeToken(b)
	b.WriteString(", ").Arg("-").WriteString(", 1))::numeric BETWEEN 100 AND 599")
	b.WriteString(" AND (split_part(")
	writeToken(b)
	b.WriteString(", ").Arg("-").WriteString(", 2))::numeric BETWEEN 100 AND 599")
	b.WriteString(" AND (split_part(")
	writeToken(b)
	b.WriteString(", ").Arg("-").WriteString(", 1))::numeric <= (split_part(")
	writeToken(b)
	b.WriteString(", ").Arg("-").WriteString(", 2))::numeric)))")
}

func writeCustomErrorPolicyNormalizedStringEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	b.WriteString("replace(replace(COALESCE(")
	writeCustomErrorPolicyJSONTextEntSQL(b, writeValue)
	b.WriteString(", ").Arg("").WriteString("), ").Arg("，").WriteString(", ").Arg(",").WriteString("), ").Arg(" ").WriteString(", ").Arg("").WriteString(")")
}

func writeCustomErrorPolicyJSONTextEntSQL(b *entsql.Builder, writeValue entSQLWriter) {
	writeValue(b)
	b.WriteString(" #>> ").Arg("{}")
}

func accountCodexQuotaProtectedPredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		extraCol := s.C(dbaccount.FieldExtra)
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("(")
			writeCodexQuotaWindowProtectedCondition(b, extraCol, "codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent")
			b.WriteString(" OR ")
			writeCodexQuotaWindowProtectedCondition(b, extraCol, "codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_limit_percent")
			b.WriteString(")")
		}))
	})
}

func writeCodexQuotaWindowProtectedCondition(b *entsql.Builder, extraCol, usedKey, resetAtKey, limitKey string) {
	b.WriteString("(")
	writeNumericExtraOrDefault(b, extraCol, usedKey, "0")
	b.WriteString(" >= ")
	writeCodexQuotaLimitExpr(b, extraCol, limitKey)
	b.WriteString(" AND ")
	writeTimestampExtraExpr(b, extraCol, resetAtKey)
	b.WriteString(" > NOW())")
}

func writeNumericExtraOrDefault(b *entsql.Builder, extraCol, key, defaultValue string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoNumericTextPattern).WriteString(" THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric ELSE ").WriteString(defaultValue).WriteString(" END)")
}

func writeCodexQuotaLimitExpr(b *entsql.Builder, extraCol, key string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoNumericTextPattern).WriteString(" THEN CASE WHEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric BETWEEN 1 AND 100 THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::numeric ELSE 100 END ELSE 100 END)")
}

func writeTimestampExtraExpr(b *entsql.Builder, extraCol, key string) {
	b.WriteString("(CASE WHEN ")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(" ~ ").Arg(accountRepoRFC3339TextPattern).WriteString(" THEN (")
	writeExtraTextExpr(b, extraCol, key)
	b.WriteString(")::timestamptz ELSE NULL END)")
}

func writeExtraTextExpr(b *entsql.Builder, extraCol, key string) {
	b.Ident(extraCol).WriteString(" ->> ").Arg(key)
}

func (r *accountRepository) listWithFilters(ctx context.Context, params pagination.PaginationParams, ownerUserID *int64, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]service.Account, *pagination.PaginationResult, error) {
	q := r.client.Account.Query()

	if ownerUserID != nil {
		q = q.Where(dbaccount.OwnerUserIDEQ(*ownerUserID))
	}
	if platform != "" {
		q = q.Where(dbaccount.PlatformEQ(platform))
	}
	if accountType != "" {
		q = q.Where(dbaccount.TypeEQ(accountType))
	}
	if status != "" {
		switch status {
		case service.StatusActive:
			q = q.Where(
				dbaccount.StatusEQ(status),
				dbaccount.SchedulableEQ(true),
				accountRateLimitInactivePredicate(),
				accountOverloadInactivePredicate(),
				accountTempUnschedulableInactivePredicate(),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
			)
		case service.AccountListStatusRateLimited:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				accountRateLimitActivePredicate(),
				accountTempUnschedulableInactivePredicate(),
			)
		case service.AccountListStatusTempUnschedulable:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
				accountTempUnschedulableActivePredicate(),
			)
		case service.AccountListStatusCodexQuotaProtected:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.PlatformEQ(service.PlatformOpenAI),
				dbaccount.TypeEQ(service.AccountTypeOAuth),
				dbaccount.Or(
					dbaccount.RateLimitResetAtIsNil(),
					dbaccount.RateLimitResetAtLTE(time.Now()),
				),
				accountCodexQuotaProtectedPredicate(),
			)
		case service.AccountListStatusUnschedulable:
			q = q.Where(
				dbaccount.StatusEQ(service.StatusActive),
				dbaccount.SchedulableEQ(false),
				accountRateLimitInactivePredicate(),
				accountOverloadInactivePredicate(),
				accountTempUnschedulableInactivePredicate(),
				dbaccount.Not(accountCodexQuotaProtectedPredicate()),
			)
		default:
			q = q.Where(dbaccount.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(dbaccount.NameContainsFold(search))
	}
	ownerSearch = strings.TrimSpace(ownerSearch)
	if ownerSearch != "" {
		ownerMatches := []dbpredicate.User{
			dbuser.EmailContainsFold(ownerSearch),
			dbuser.UsernameContainsFold(ownerSearch),
		}
		if ownerID, err := strconv.ParseInt(ownerSearch, 10, 64); err == nil && ownerID > 0 {
			ownerMatches = append(ownerMatches, dbuser.IDEQ(ownerID))
		}
		q = q.Where(dbaccount.HasOwnerWith(
			dbuser.DeletedAtIsNil(),
			dbuser.Or(ownerMatches...),
		))
	}
	if groupID == service.AccountListGroupUngrouped {
		q = q.Where(accountHasNoNonPrivateGroups())
	} else if groupID > 0 {
		q = q.Where(dbaccount.HasAccountGroupsWith(dbaccountgroup.GroupIDEQ(groupID)))
	}
	if proxyID == service.AccountListProxyUnassigned {
		q = q.Where(dbaccount.ProxyIDIsNil())
	} else if proxyID > 0 {
		q = q.Where(dbaccount.ProxyIDEQ(proxyID))
	}
	if privacyMode != "" {
		q = q.Where(dbpredicate.Account(func(s *entsql.Selector) {
			path := sqljson.Path("privacy_mode")
			switch privacyMode {
			case service.AccountPrivacyModeUnsetFilter:
				s.Where(entsql.Or(
					entsql.Not(sqljson.HasKey(dbaccount.FieldExtra, path)),
					sqljson.ValueEQ(dbaccount.FieldExtra, "", path),
				))
			default:
				s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, privacyMode, path))
			}
		}))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	accountsQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range accountListOrder(params, ownerUserID != nil) {
		accountsQuery = accountsQuery.Order(order)
	}

	accounts, err := accountsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outAccounts, err := r.accountsToService(ctx, accounts)
	if err != nil {
		return nil, nil, err
	}
	return outAccounts, paginationResultFromTotal(int64(total), params), nil
}

func accountListOrder(params pagination.PaginationParams, ownedScope bool) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderAsc)

	field := dbaccount.FieldName
	defaultOrder := true
	switch sortBy {
	case "", "name":
		field = dbaccount.FieldName
	case "id":
		field = dbaccount.FieldID
		defaultOrder = false
	case "status":
		field = dbaccount.FieldStatus
		defaultOrder = false
	case "schedulable":
		field = dbaccount.FieldSchedulable
		defaultOrder = false
	case "priority":
		if ownedScope {
			return accountListOwnedPriorityOrder(sortOrder)
		}
		field = dbaccount.FieldPriority
		defaultOrder = false
	case "rate_multiplier":
		field = dbaccount.FieldRateMultiplier
		defaultOrder = false
	case "last_used_at":
		field = dbaccount.FieldLastUsedAt
		defaultOrder = false
	case "expires_at":
		field = dbaccount.FieldExpiresAt
		defaultOrder = false
	case "created_at":
		field = dbaccount.FieldCreatedAt
		defaultOrder = false
	}

	if sortOrder == pagination.SortOrderDesc {
		return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(dbaccount.FieldID)}
	}
	if defaultOrder {
		return []func(*entsql.Selector){dbent.Asc(dbaccount.FieldName), dbent.Asc(dbaccount.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(dbaccount.FieldID)}
}

func accountListOwnedPriorityOrder(sortOrder string) []func(*entsql.Selector) {
	orderExpr := func(direction string, tieOrder func(string) string) func(*entsql.Selector) {
		return func(s *entsql.Selector) {
			expr := fmt.Sprintf(
				"CASE WHEN %s > 0 THEN %s ELSE %s END %s",
				s.C(dbaccount.FieldPrivatePriority),
				s.C(dbaccount.FieldPrivatePriority),
				s.C(dbaccount.FieldPriority),
				direction,
			)
			s.OrderExpr(entsql.Expr(expr))
			s.OrderBy(tieOrder(s.C(dbaccount.FieldID)))
		}
	}
	if sortOrder == pagination.SortOrderDesc {
		return []func(*entsql.Selector){orderExpr("DESC", entsql.Desc)}
	}
	return []func(*entsql.Selector){orderExpr("ASC", entsql.Asc)}
}

func accountHasNoNonPrivateGroups() dbpredicate.Account {
	return dbaccount.Not(dbaccount.HasAccountGroupsWith(
		dbaccountgroup.HasGroupWith(
			dbgroup.DeletedAtIsNil(),
			dbgroup.ScopeNEQ(service.GroupScopeUserPrivate),
		),
	))
}

func (r *accountRepository) ListByGroup(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, err := r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status: service.StatusActive,
	})
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

func (r *accountRepository) ListActive(ctx context.Context) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(dbaccount.StatusEQ(service.StatusActive)).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListOAuthRefreshCandidates(ctx context.Context, refreshWindow time.Duration) ([]service.Account, error) {
	if r.sql == nil {
		return nil, errors.New("account repository SQL executor not configured")
	}
	query, args := buildOAuthRefreshCandidatesQuery(refreshWindow)
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0, 128)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []service.Account{}, nil
	}

	accounts, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil {
			out = append(out, *account)
		}
	}
	return out, nil
}

func buildOAuthRefreshCandidatesQuery(refreshWindow time.Duration) (string, []any) {
	refreshWindowSeconds := int64(refreshWindow / time.Second)
	if refreshWindowSeconds < 0 {
		refreshWindowSeconds = 0
	}
	return `
		WITH candidates AS (
			SELECT
				id,
				priority,
				platform,
				rate_limit_reset_at,
				NULLIF(btrim(credentials->>'expires_at'), '') AS expires_at_raw
			FROM accounts
			WHERE deleted_at IS NULL
				AND status = 'active'
				AND type = 'oauth'
				AND platform IN ('anthropic', 'openai', 'gemini', 'antigravity')
				AND credentials ? 'refresh_token'
				AND btrim(credentials->>'refresh_token') <> ''
				AND (
					temp_unschedulable_until > NOW()
					AND temp_unschedulable_reason LIKE 'token refresh retry exhausted:%'
				) IS NOT TRUE
		),
		parsed AS (
			SELECT
				id,
				priority,
				platform,
				rate_limit_reset_at,
				expires_at_raw,
				CASE
					WHEN expires_at_raw ~ '^[0-9]+$' THEN to_timestamp(expires_at_raw::double precision)
					ELSE NULL
				END AS credential_expires_at,
				(expires_at_raw IS NOT NULL AND expires_at_raw !~ '^[0-9]+$') AS needs_go_time_parse
			FROM candidates
		)
		SELECT id
		FROM parsed
		WHERE
			needs_go_time_parse
			OR (
				platform = 'openai'
				AND (
					(credential_expires_at IS NOT NULL AND credential_expires_at <= NOW() + ($1::bigint * INTERVAL '1 second'))
					OR (credential_expires_at IS NULL AND rate_limit_reset_at > NOW())
				)
			)
			OR (
				platform IN ('anthropic', 'gemini')
				AND credential_expires_at IS NOT NULL
				AND credential_expires_at <= NOW() + ($1::bigint * INTERVAL '1 second')
			)
			OR (
				platform = 'antigravity'
				AND credential_expires_at IS NOT NULL
				AND credential_expires_at <= NOW() + INTERVAL '15 minutes'
			)
		ORDER BY priority ASC, id ASC
	`, []any{refreshWindowSeconds}
}

func (r *accountRepository) ListByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) UpdateLastUsed(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetLastUsedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"last_used": map[string]int64{
			strconv.FormatInt(id, 10): now.Unix(),
		},
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, &id, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue last used failed: account=%d err=%v", id, err)
	}
	return nil
}

func (r *accountRepository) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(updates))
	args := make([]any, 0, len(updates)*2+1)
	caseSQL := "UPDATE accounts SET last_used_at = CASE id"

	idx := 1
	for id, ts := range updates {
		caseSQL += " WHEN $" + itoa(idx) + " THEN $" + itoa(idx+1) + "::timestamptz"
		args = append(args, id, ts)
		ids = append(ids, id)
		idx += 2
	}

	caseSQL += " END, updated_at = NOW() WHERE id = ANY($" + itoa(idx) + ") AND deleted_at IS NULL"
	args = append(args, pq.Array(ids))

	_, err := r.sql.ExecContext(ctx, caseSQL, args...)
	if err != nil {
		return err
	}
	lastUsedPayload := make(map[string]int64, len(updates))
	for id, ts := range updates {
		lastUsedPayload[strconv.FormatInt(id, 10)] = ts.Unix()
	}
	payload := map[string]any{"last_used": lastUsedPayload}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountLastUsed, nil, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue batch last used failed: err=%v", err)
	}
	return nil
}

func (r *accountRepository) SetError(ctx context.Context, id int64, errorMsg string) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET status = $2,
			error_message = $3,
			error_since = COALESCE(error_since, NOW()),
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, service.StatusError, errorMsg)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue set error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

// syncSchedulerAccountSnapshot 在账号状态变更时主动同步快照到调度器缓存。
// 当账号被设置为错误、禁用、不可调度或临时不可调度时调用，
// 确保调度器和粘性会话逻辑能及时感知账号的最新状态，避免继续使用不可用账号。
//
// syncSchedulerAccountSnapshot proactively syncs account snapshot to scheduler cache
// when account status changes. Called when account is set to error, disabled,
// unschedulable, or temporarily unschedulable, ensuring scheduler and sticky session
// logic can promptly detect the latest account state and avoid using unavailable accounts.
func (r *accountRepository) syncSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	account, err := r.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot read failed: id=%d err=%v", accountID, err)
		return
	}
	if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot write failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) deleteSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	if err := r.schedulerCache.DeleteAccount(ctx, accountID); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] delete account snapshot failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) syncSchedulerAccountSnapshots(ctx context.Context, accountIDs []int64) {
	if r == nil || r.schedulerCache == nil || len(accountIDs) == 0 {
		return
	}

	uniqueIDs := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return
	}

	accounts, err := r.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot read failed: count=%d err=%v", len(uniqueIDs), err)
		return
	}

	for _, account := range accounts {
		if account == nil {
			continue
		}
		if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot write failed: id=%d err=%v", account.ID, err)
		}
	}
}

func (r *accountRepository) syncSchedulerAccountAndGroupSnapshots(ctx context.Context, accountID int64, groupIDs []int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	account, err := r.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account/group snapshot read failed: id=%d err=%v", accountID, err)
		return
	}
	if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account/group snapshot write failed: id=%d err=%v", accountID, err)
	}
	r.syncSchedulerGroupSnapshotsForAccount(ctx, account, groupIDs)
}

func (r *accountRepository) syncSchedulerGroupSnapshotsForAccount(ctx context.Context, account *service.Account, groupIDs []int64) {
	if r == nil || r.schedulerCache == nil || account == nil || len(groupIDs) == 0 {
		return
	}
	platforms := []string{account.Platform}
	if account.Platform == service.PlatformAntigravity && account.IsMixedSchedulingEnabled() {
		platforms = append(platforms, service.PlatformAnthropic, service.PlatformGemini)
	}
	for _, platform := range platforms {
		r.syncSchedulerGroupPlatformSnapshots(ctx, groupIDs, platform)
	}
}

func (r *accountRepository) syncSchedulerGroupPlatformSnapshots(ctx context.Context, groupIDs []int64, platform string) {
	if r == nil || r.schedulerCache == nil || len(groupIDs) == 0 || platform == "" {
		return
	}
	for _, groupID := range uniquePositiveInt64s(groupIDs) {
		accounts, err := r.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
		if err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] sync group snapshot read failed: group=%d platform=%s err=%v", groupID, platform, err)
			continue
		}
		accounts = filterSchedulerSnapshotAccounts(accounts)
		for _, mode := range []string{service.SchedulerModeSingle, service.SchedulerModeForced} {
			bucket := service.SchedulerBucket{GroupID: groupID, Platform: platform, Mode: mode}
			if err := r.schedulerCache.SetSnapshot(ctx, bucket, accounts); err != nil {
				logger.LegacyPrintf("repository.account", "[Scheduler] sync group snapshot write failed: bucket=%s err=%v", bucket.String(), err)
			}
		}
		if platform == service.PlatformAnthropic || platform == service.PlatformGemini {
			mixedAccounts, err := r.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, []string{platform, service.PlatformAntigravity})
			if err != nil {
				logger.LegacyPrintf("repository.account", "[Scheduler] sync mixed group snapshot read failed: group=%d platform=%s err=%v", groupID, platform, err)
				continue
			}
			mixedAccounts = filterSchedulerMixedSnapshotAccounts(mixedAccounts)
			bucket := service.SchedulerBucket{GroupID: groupID, Platform: platform, Mode: service.SchedulerModeMixed}
			if err := r.schedulerCache.SetSnapshot(ctx, bucket, mixedAccounts); err != nil {
				logger.LegacyPrintf("repository.account", "[Scheduler] sync mixed group snapshot write failed: bucket=%s err=%v", bucket.String(), err)
			}
		}
	}
}

func filterSchedulerSnapshotAccounts(accounts []service.Account) []service.Account {
	if len(accounts) == 0 {
		return accounts
	}
	filtered := accounts[:0]
	for _, account := range accounts {
		if account.IsSchedulableWithoutCodexQuotaProtection() {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func filterSchedulerMixedSnapshotAccounts(accounts []service.Account) []service.Account {
	if len(accounts) == 0 {
		return accounts
	}
	filtered := accounts[:0]
	for _, account := range accounts {
		if !account.IsSchedulableWithoutCodexQuotaProtection() {
			continue
		}
		if account.Platform == service.PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
			continue
		}
		filtered = append(filtered, account)
	}
	return filtered
}

func (r *accountRepository) ClearError(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET status = $2,
			error_message = '',
			error_since = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id, service.StatusActive)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) syncAccountErrorSince(ctx context.Context, id int64, status string) error {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return fmt.Errorf("sql executor is unavailable")
	}
	if status == service.StatusError {
		_, err := exec.ExecContext(ctx, `
			UPDATE accounts
			SET error_since = COALESCE(error_since, NOW())
			WHERE id = $1
				AND status = $2
				AND deleted_at IS NULL
		`, id, service.StatusError)
		return err
	}

	_, err := exec.ExecContext(ctx, `
		UPDATE accounts
		SET error_since = NULL
		WHERE id = $1
			AND error_since IS NOT NULL
			AND deleted_at IS NULL
	`, id)
	return err
}

func (r *accountRepository) AddToGroup(ctx context.Context, accountID, groupID int64, priority int) error {
	_, err := r.client.AccountGroup.Create().
		SetAccountID(accountID).
		SetGroupID(groupID).
		SetPriority(priority).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue add to group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	r.syncSchedulerAccountAndGroupSnapshots(ctx, accountID, []int64{groupID})
	return nil
}

func (r *accountRepository) RemoveFromGroup(ctx context.Context, accountID, groupID int64) error {
	_, err := r.client.AccountGroup.Delete().
		Where(
			dbaccountgroup.AccountIDEQ(accountID),
			dbaccountgroup.GroupIDEQ(groupID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue remove from group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	r.syncSchedulerAccountAndGroupSnapshots(ctx, accountID, []int64{groupID})
	return nil
}

func (r *accountRepository) GetGroups(ctx context.Context, accountID int64) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(
			dbgroup.HasAccountsWith(dbaccount.IDEQ(accountID)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupEntityToService(groups[i]))
	}
	return outGroups, nil
}

func (r *accountRepository) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	existingGroupIDs, err := r.loadAccountGroupIDs(ctx, accountID)
	if err != nil {
		return err
	}
	// 使用事务保证删除旧绑定与创建新绑定的原子性
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	var txClient *dbent.Client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
	} else {
		// 已处于外部事务中（ErrTxStarted），复用当前 client
		txClient = r.client
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(accountID)).Exec(ctx); err != nil {
		return err
	}

	if len(groupIDs) > 0 {
		builders := make([]*dbent.AccountGroupCreate, 0, len(groupIDs))
		for i, groupID := range groupIDs {
			builders = append(builders, txClient.AccountGroup.Create().
				SetAccountID(accountID).
				SetGroupID(groupID).
				SetPriority(i+1),
			)
		}

		if _, err := txClient.AccountGroup.CreateBulk(builders...).Save(ctx); err != nil {
			return err
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	payload := buildSchedulerGroupPayload(mergeGroupIDs(existingGroupIDs, groupIDs))
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bind groups failed: account=%d err=%v", accountID, err)
	}
	r.syncSchedulerAccountAndGroupSnapshots(ctx, accountID, mergeGroupIDs(existingGroupIDs, groupIDs))
	return nil
}

func (r *accountRepository) ListSchedulable(ctx context.Context) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
	})
}

func (r *accountRepository) ListSchedulableByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	// 单平台查询复用多平台逻辑，保持过滤条件与排序策略一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   []string{platform},
	})
}

func (r *accountRepository) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 仅返回可调度的活跃账号，并过滤处于过载/限流窗口的账号。
	// 代理与分组信息统一在 accountsToService 中批量加载，避免 N+1 查询。
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]service.Account, error) {
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformEQ(platform),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	now := time.Now()
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.PlatformIn(platforms...),
			dbaccount.StatusEQ(service.StatusActive),
			dbaccount.SchedulableEQ(true),
			dbaccount.Not(dbaccount.HasAccountGroups()),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		).
		Order(dbent.Asc(dbaccount.FieldPriority)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	if len(platforms) == 0 {
		return nil, nil
	}
	// 复用按分组查询逻辑，保证分组优先级 + 账号优先级的排序与筛选一致。
	return r.queryAccountsByGroup(ctx, groupID, accountGroupQueryOptions{
		status:      service.StatusActive,
		schedulable: true,
		platforms:   platforms,
	})
}

func (r *accountRepository) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	now := time.Now()
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetRateLimitedAt(now).
		SetRateLimitResetAt(resetAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time) error {
	if scope == "" {
		return nil
	}
	now := time.Now().UTC()
	payload := map[string]string{
		"rate_limited_at":     now.Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		`UPDATE accounts SET 
			extra = jsonb_set(
				jsonb_set(COALESCE(extra, '{}'::jsonb), '{model_rate_limits}'::text[], COALESCE(extra->'model_rate_limits', '{}'::jsonb), true),
				ARRAY['model_rate_limits', $1]::text[],
				$2::jsonb,
				true
			),
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL`,
		scope,
		raw,
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue model rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetOverloadUntil(until).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue overload failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = $1,
			temp_unschedulable_reason = $2,
			updated_at = NOW()
		WHERE id = $3
			AND deleted_at IS NULL
			AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until < $1)
	`, until, reason, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected <= 0 {
		return nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue temp unschedulable failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearTempUnschedulable(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET temp_unschedulable_until = NULL,
			temp_unschedulable_reason = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear temp unschedulable failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearRateLimit(ctx context.Context, id int64) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		ClearRateLimitedAt().
		ClearRateLimitResetAt().
		ClearOverloadUntil().
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) - 'antigravity_quota_scopes', updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear quota scopes failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) ClearModelRateLimits(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) - 'model_rate_limits', updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear model rate limit failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	builder := r.client.Account.Update().
		Where(dbaccount.IDEQ(id))
	if status != "" {
		builder.SetSessionWindowStatus(status)
	}
	if start != nil {
		builder.SetSessionWindowStart(*start)
	}
	if end != nil {
		builder.SetSessionWindowEnd(*end)
	}
	_, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	// 触发调度器缓存更新。窗口时间和状态都会进入调度 metadata，
	// 需要立即刷新单账号快照，避免缓存路径继续按旧窗口限速。
	if start != nil || end != nil || status != "" {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue session window update failed: account=%d err=%v", id, err)
		}
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetSchedulable(schedulable).
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue schedulable change failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		UPDATE accounts
		SET schedulable = FALSE,
			updated_at = NOW()
		WHERE deleted_at IS NULL
			AND schedulable = TRUE
			AND auto_pause_on_expired = TRUE
			AND expires_at IS NOT NULL
			AND expires_at <= $1
		RETURNING id
	`, now)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventFullRebuild, nil, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue auto pause rebuild failed: err=%v", err)
		}
		r.syncSchedulerAccountSnapshots(ctx, ids)
	}
	return int64(len(ids)), nil
}

func (r *accountRepository) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// 使用 JSONB 合并操作实现原子更新，避免读-改-写的并发丢失更新问题
	payload, err := json.Marshal(updates)
	if err != nil {
		return err
	}

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		"UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL",
		string(payload), id,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) ||
		(hasCodexQuotaProtectionRelevantExtraUpdate(updates) && r.isCodexQuotaProtectionActive(ctx, id)) {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue extra update failed: account=%d err=%v", id, err)
		}
		r.syncSchedulerAccountSnapshot(ctx, id)
	} else {
		// 观测型 extra 字段不需要触发 bucket 重建，但仍同步单账号快照，
		// 让 sticky session / GetAccount 命中缓存时也能读到最新数据，
		// 同时避免缓存局部 patch 覆盖掉并发写入的其它账号字段。
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func (r *accountRepository) isCodexQuotaProtectionActive(ctx context.Context, accountID int64) bool {
	account, err := r.GetByID(ctx, accountID)
	if err != nil || account == nil {
		if err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] check codex quota protection failed: account=%d err=%v", accountID, err)
		}
		return false
	}
	return account.IsCodexQuotaProtectionActiveAt(time.Now())
}

func shouldEnqueueSchedulerOutboxForExtraUpdates(updates map[string]any) bool {
	if len(updates) == 0 {
		return false
	}
	for key := range updates {
		if _, ok := schedulerRelevantExtraKeys[strings.TrimSpace(key)]; ok {
			return true
		}
		if isCodexQuotaLimitExtraKey(key) {
			return true
		}
		if isSchedulerNeutralExtraKey(key) {
			continue
		}
		return true
	}
	return false
}

func isCodexQuotaLimitExtraKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "codex_5h_limit_percent", "codex_7d_limit_percent":
		return true
	default:
		return false
	}
}

func hasCodexQuotaProtectionRelevantExtraUpdate(updates map[string]any) bool {
	for key := range updates {
		switch strings.TrimSpace(key) {
		case "codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_reset_after_seconds",
			"codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_reset_after_seconds",
			"codex_5h_limit_percent", "codex_7d_limit_percent":
			return true
		}
	}
	return false
}

func isSchedulerNeutralExtraKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if _, ok := schedulerNeutralExtraKeys[key]; ok {
		return true
	}
	for _, prefix := range schedulerNeutralExtraKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (r *accountRepository) BulkUpdate(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	setClauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	idx := 1
	if updates.Name != nil {
		setClauses = append(setClauses, "name = $"+itoa(idx))
		args = append(args, *updates.Name)
		idx++
	}
	if updates.ProxyID != nil {
		// 0 表示清除代理（前端发送 0 而不是 null 来表达清除意图）
		if *updates.ProxyID == 0 {
			setClauses = append(setClauses, "proxy_id = NULL")
		} else {
			setClauses = append(setClauses, "proxy_id = $"+itoa(idx))
			args = append(args, *updates.ProxyID)
			idx++
		}
	}
	if updates.Concurrency != nil {
		setClauses = append(setClauses, "concurrency = $"+itoa(idx))
		args = append(args, *updates.Concurrency)
		idx++
	}
	if updates.Priority != nil {
		setClauses = append(setClauses, "priority = $"+itoa(idx))
		args = append(args, *updates.Priority)
		idx++
	}
	if updates.PrivatePriority != nil {
		setClauses = append(setClauses, "private_priority = $"+itoa(idx))
		args = append(args, *updates.PrivatePriority)
		idx++
	}
	if updates.RateMultiplier != nil {
		setClauses = append(setClauses, "rate_multiplier = $"+itoa(idx))
		args = append(args, *updates.RateMultiplier)
		idx++
	}
	if updates.LoadFactor != nil {
		if *updates.LoadFactor <= 0 {
			setClauses = append(setClauses, "load_factor = NULL")
		} else {
			setClauses = append(setClauses, "load_factor = $"+itoa(idx))
			args = append(args, *updates.LoadFactor)
			idx++
		}
	}
	if updates.Status != nil {
		setClauses = append(setClauses, "status = $"+itoa(idx))
		args = append(args, *updates.Status)
		idx++
		if *updates.Status == service.StatusError {
			setClauses = append(setClauses, "error_since = COALESCE(error_since, NOW())")
		} else {
			setClauses = append(setClauses, "error_since = NULL")
		}
	}
	if updates.Schedulable != nil {
		setClauses = append(setClauses, "schedulable = $"+itoa(idx))
		args = append(args, *updates.Schedulable)
		idx++
	}
	if updates.AccountLevel != nil {
		setClauses = append(setClauses, "account_level = $"+itoa(idx))
		args = append(args, service.NormalizeAccountLevel(*updates.AccountLevel))
		idx++
	}
	// JSONB 需要合并而非覆盖，使用 raw SQL 保持旧行为。
	if len(updates.Credentials) > 0 {
		payload, err := json.Marshal(updates.Credentials)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "credentials = COALESCE(credentials, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}
	if len(updates.Extra) > 0 {
		payload, err := json.Marshal(updates.Extra)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "extra = COALESCE(extra, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}

	if len(setClauses) == 0 {
		return 0, nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := "UPDATE accounts SET " + joinClauses(setClauses, ", ") + " WHERE id = ANY($" + itoa(idx) + ") AND deleted_at IS NULL"
	args = append(args, pq.Array(ids))

	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, translateAccountPersistenceError(err, nil)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		payload := map[string]any{"account_ids": ids}
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bulk update failed: err=%v", err)
		}
		if updates.AccountLevel != nil || len(updates.Credentials) > 0 || len(updates.Extra) > 0 {
			if _, err := r.repairOpenAISharedPoolBindings(ctx, ids); err != nil {
				return 0, err
			}
		}
		r.syncSchedulerAccountSnapshots(ctx, ids)
	}
	return rows, nil
}

func (r *accountRepository) repairOpenAISharedPoolBindings(ctx context.Context, accountIDs []int64) (bool, error) {
	if r == nil || r.sql == nil || len(accountIDs) == 0 {
		return false, nil
	}
	changedAccountIDs, affectedGroupIDs, err := repairOpenAISharedPoolBindingsForAccounts(ctx, r.sql, accountIDs)
	if err != nil {
		return false, fmt.Errorf("repair openai shared pool bindings: %w", err)
	}
	if len(changedAccountIDs) == 0 && len(affectedGroupIDs) == 0 {
		return false, nil
	}
	if len(changedAccountIDs) == 0 {
		for _, groupID := range uniquePositiveInt64s(affectedGroupIDs) {
			gid := groupID
			if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventGroupChanged, nil, &gid, nil); err != nil {
				logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue shared pool group repair failed: group=%d err=%v", gid, err)
			}
		}
		r.syncSchedulerOpenAIGroupSnapshots(ctx, affectedGroupIDs)
		return true, nil
	}
	payload := map[string]any{"account_ids": changedAccountIDs}
	if len(affectedGroupIDs) > 0 {
		payload["group_ids"] = affectedGroupIDs
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue shared pool binding repair failed: err=%v", err)
	}
	r.syncSchedulerAccountSnapshots(ctx, changedAccountIDs)
	r.syncSchedulerOpenAIGroupSnapshots(ctx, affectedGroupIDs)
	return true, nil
}

func (r *accountRepository) syncSchedulerOpenAIGroupSnapshots(ctx context.Context, groupIDs []int64) {
	r.syncSchedulerGroupPlatformSnapshots(ctx, groupIDs, service.PlatformOpenAI)
}

func repairOpenAISharedPoolBindingsForAccounts(ctx context.Context, exec sqlExecutor, accountIDs []int64) ([]int64, []int64, error) {
	if exec == nil || len(accountIDs) == 0 {
		return nil, nil, nil
	}
	ensuredGroupIDs, err := ensureOpenAIProSharedPoolForAccounts(ctx, exec, accountIDs, false)
	if err != nil {
		return nil, nil, err
	}
	var changedAccountIDs pq.Int64Array
	var affectedGroupIDs pq.Int64Array
	query := strings.ReplaceAll(`
		WITH candidate_accounts AS (
			SELECT
				a.id,
				CASE
					WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'team'
						OR plan.token IN ('team', 'chatgptteam')
						OR plan.token LIKE 'team%' THEN 'team'
					WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'pro'
						OR plan.token = 'pro'
						OR plan.token = 'chatgptpro'
						OR plan.token LIKE 'pro%'
						OR plan.token LIKE 'chatgptpro%' THEN 'pro'
					WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'plus'
						OR plan.token = 'plus'
						OR plan.token = 'chatgptplus'
						OR plan.token LIKE 'plus%' THEN 'plus'
					WHEN lower(btrim(COALESCE(a.account_level, ''))) = 'free'
						OR plan.token IN ('free', 'chatgptfree') THEN 'free'
					ELSE 'free'
				END AS effective_level
			FROM accounts a
			CROSS JOIN LATERAL (
				SELECT {{OPENAI_PLAN_TOKEN}} AS token
			) plan
			WHERE a.id = ANY($1)
				AND a.deleted_at IS NULL
				AND a.platform = 'openai'
				AND a.type = 'oauth'
				AND a.owner_user_id IS NOT NULL
				AND lower(btrim(COALESCE(a.share_mode, ''))) = 'public'
				AND lower(btrim(COALESCE(a.share_status, ''))) NOT IN ('pending', 'suspended')
		),
		candidate_target_groups AS (
			SELECT
				g.id,
				lower(btrim(COALESCE(g.required_account_level, ''))) AS required_level,
				g.name,
				g.sort_order
			FROM groups g
			WHERE g.deleted_at IS NULL
				AND g.platform = 'openai'
				AND g.status = 'active'
				AND g.owner_user_id IS NULL
				AND lower(btrim(COALESCE(g.scope, ''))) = 'public'
				AND g.is_exclusive = FALSE
				AND lower(btrim(COALESCE(g.subscription_type, ''))) IN ('', 'standard')
				AND lower(btrim(COALESCE(g.required_account_level, ''))) IN ('free', 'plus', 'pro', 'team')
		),
		target_groups AS (
			SELECT DISTINCT ON (required_level)
				required_level,
				id
			FROM candidate_target_groups
			ORDER BY
				required_level,
				CASE
					WHEN required_level = 'pro' AND name = 'PRO共享号池' THEN 0
					WHEN required_level = 'pro' AND name = 'OpenAI PRO共享号池' THEN 1
					WHEN required_level = 'pro' AND name = 'OpenAI PRO共享号池(公共)' THEN 2
					WHEN name LIKE '%共享号池%' THEN 3
					ELSE 4
				END,
				sort_order,
				id
		),
		matched_accounts AS (
			SELECT ca.id AS account_id, tg.id AS target_group_id
			FROM candidate_accounts ca
			JOIN target_groups tg ON tg.required_level = ca.effective_level
		),
		stale_public_bindings AS (
			DELETE FROM account_groups ag
			USING groups g, matched_accounts ma
			WHERE ag.account_id = ma.account_id
				AND ag.group_id = g.id
				AND g.deleted_at IS NULL
				AND g.platform = 'openai'
				AND g.owner_user_id IS NULL
				AND lower(btrim(COALESCE(g.scope, ''))) = 'public'
				AND g.is_exclusive = FALSE
				AND lower(btrim(COALESCE(g.subscription_type, ''))) IN ('', 'standard')
				AND g.id <> ma.target_group_id
			RETURNING ag.account_id, ag.group_id, ag.priority
		),
		inserted_bindings AS (
			INSERT INTO account_groups (account_id, group_id, priority, created_at)
			SELECT
				ma.account_id,
				ma.target_group_id,
				COALESCE(MIN(spb.priority), 50),
				NOW()
			FROM matched_accounts ma
			LEFT JOIN stale_public_bindings spb ON spb.account_id = ma.account_id
			GROUP BY ma.account_id, ma.target_group_id
			ON CONFLICT (account_id, group_id) DO NOTHING
			RETURNING account_id, group_id
		),
		changed_accounts AS (
			SELECT account_id FROM stale_public_bindings
			UNION
			SELECT account_id FROM inserted_bindings
		),
		affected_groups AS (
			SELECT group_id FROM stale_public_bindings
			UNION
			SELECT group_id FROM inserted_bindings
		)
		SELECT
			COALESCE((SELECT array_agg(account_id ORDER BY account_id) FROM changed_accounts), '{}'::bigint[]),
			COALESCE((SELECT array_agg(group_id ORDER BY group_id) FROM affected_groups), '{}'::bigint[])
	`, "{{OPENAI_PLAN_TOKEN}}", openAIPlanTokenSQL("a.credentials", "a.extra"))
	err = scanSingleRow(ctx, exec, query, []any{pq.Array(accountIDs)}, &changedAccountIDs, &affectedGroupIDs)
	if err != nil {
		return nil, nil, err
	}
	return append([]int64(nil), changedAccountIDs...), mergeInt64Slices(affectedGroupIDs, ensuredGroupIDs), nil
}

func ensureOpenAIProSharedPoolForAccounts(ctx context.Context, exec sqlExecutor, accountIDs []int64, includePending bool) ([]int64, error) {
	if exec == nil || len(accountIDs) == 0 {
		return nil, nil
	}
	var groupIDs pq.Int64Array
	query := strings.ReplaceAll(`
		WITH effective_pro_accounts AS (
			SELECT a.id
			FROM accounts a
			CROSS JOIN LATERAL (
				SELECT {{OPENAI_PLAN_TOKEN}} AS token
			) plan
			WHERE a.id = ANY($1)
				AND a.deleted_at IS NULL
				AND a.platform = 'openai'
				AND a.type = 'oauth'
				AND a.owner_user_id IS NOT NULL
				AND lower(btrim(COALESCE(a.share_mode, ''))) = 'public'
				AND lower(btrim(COALESCE(a.share_status, ''))) <> 'suspended'
				AND ($2::boolean OR lower(btrim(COALESCE(a.share_status, ''))) <> 'pending')
				AND NOT (
					lower(btrim(COALESCE(a.account_level, ''))) = 'team'
					OR plan.token IN ('team', 'chatgptteam')
					OR plan.token LIKE 'team%'
				)
				AND (
					lower(btrim(COALESCE(a.account_level, ''))) = 'pro'
					OR plan.token = 'pro'
					OR plan.token = 'chatgptpro'
					OR plan.token LIKE 'pro%'
					OR plan.token LIKE 'chatgptpro%'
				)
		),
		normalized_pro_pool AS (
			UPDATE groups
			SET platform = 'openai',
				scope = 'public',
				owner_user_id = NULL,
				subscription_type = 'standard',
				required_account_level = 'pro',
				is_exclusive = FALSE,
				updated_at = NOW()
			WHERE deleted_at IS NULL
				AND status = 'active'
				AND platform = 'openai'
				AND (owner_user_id IS NULL OR lower(btrim(COALESCE(scope, ''))) = 'public')
				AND (
					lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
					OR btrim(name) = 'PRO共享号池'
				)
				AND (
					lower(btrim(COALESCE(scope, ''))) <> 'public'
					OR owner_user_id IS NOT NULL
					OR lower(btrim(COALESCE(subscription_type, ''))) <> 'standard'
					OR lower(btrim(COALESCE(required_account_level, ''))) <> 'pro'
					OR is_exclusive = TRUE
				)
			RETURNING id
		),
		existing_pro_pool AS (
			SELECT id
			FROM groups
			WHERE deleted_at IS NULL
				AND platform = 'openai'
				AND status = 'active'
				AND owner_user_id IS NULL
				AND lower(btrim(COALESCE(scope, ''))) = 'public'
				AND is_exclusive = FALSE
				AND lower(btrim(COALESCE(subscription_type, ''))) IN ('', 'standard')
				AND lower(btrim(COALESCE(required_account_level, ''))) = 'pro'
			LIMIT 1
		),
		source_pool AS (
			SELECT
				description,
				rate_multiplier,
				default_validity_days,
				allow_image_generation,
				image_rate_independent,
				image_rate_multiplier,
				claude_code_only,
				COALESCE(model_routing, '{}'::jsonb) AS model_routing,
				model_routing_enabled,
				mcp_xml_inject,
				COALESCE(supported_model_scopes, '[]'::jsonb) AS supported_model_scopes,
				sort_order,
				allow_messages_dispatch,
				require_oauth_only,
				require_privacy_set,
				default_mapped_model,
				COALESCE(messages_dispatch_model_config, '{}'::jsonb) AS messages_dispatch_model_config,
				rpm_limit
			FROM groups
			WHERE deleted_at IS NULL
				AND platform = 'openai'
				AND status = 'active'
				AND owner_user_id IS NULL
				AND lower(btrim(COALESCE(scope, ''))) = 'public'
				AND is_exclusive = FALSE
				AND lower(btrim(COALESCE(subscription_type, ''))) IN ('', 'standard')
				AND lower(btrim(COALESCE(required_account_level, ''))) IN ('plus', 'free', '')
			ORDER BY CASE lower(btrim(COALESCE(required_account_level, '')))
				WHEN 'plus' THEN 0
				WHEN 'free' THEN 1
				ELSE 2
			END, sort_order, id
			LIMIT 1
		),
		template_pool AS (
			SELECT * FROM source_pool
			UNION ALL
			SELECT
				'OpenAI Pro public shared pool'::text,
				1.0::numeric,
				30::integer,
				FALSE,
				FALSE,
				1.0::numeric,
				FALSE,
				'{}'::jsonb,
				FALSE,
				TRUE,
				'[]'::jsonb,
				0::integer,
				FALSE,
				FALSE,
				FALSE,
				''::text,
				'{}'::jsonb,
				0::integer
			WHERE NOT EXISTS (SELECT 1 FROM source_pool)
			LIMIT 1
		),
		candidate_name AS (
			SELECT CASE
				WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'PRO共享号池')
					THEN 'PRO共享号池'
				WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'OpenAI PRO共享号池')
					THEN 'OpenAI PRO共享号池'
				WHEN NOT EXISTS (SELECT 1 FROM groups WHERE deleted_at IS NULL AND name = 'OpenAI PRO共享号池(公共)')
					THEN 'OpenAI PRO共享号池(公共)'
				ELSE 'OpenAI PRO共享号池-' || ((SELECT COALESCE(MAX(id), 0) FROM groups) + 1)::text
			END AS name
		),
		inserted_pro_pool AS (
			INSERT INTO groups (
				name,
				description,
				rate_multiplier,
				is_exclusive,
				status,
				owner_user_id,
				scope,
				platform,
				required_account_level,
				subscription_type,
				default_validity_days,
				allow_image_generation,
				image_rate_independent,
				image_rate_multiplier,
				claude_code_only,
				model_routing,
				model_routing_enabled,
				mcp_xml_inject,
				supported_model_scopes,
				sort_order,
				allow_messages_dispatch,
				require_oauth_only,
				require_privacy_set,
				default_mapped_model,
				messages_dispatch_model_config,
				rpm_limit,
				created_at,
				updated_at
			)
			SELECT
				candidate_name.name,
				COALESCE(NULLIF(template_pool.description, ''), 'OpenAI Pro public shared pool'),
				template_pool.rate_multiplier,
				FALSE,
				'active',
				NULL,
				'public',
				'openai',
				'pro',
				'standard',
				template_pool.default_validity_days,
				template_pool.allow_image_generation,
				template_pool.image_rate_independent,
				template_pool.image_rate_multiplier,
				template_pool.claude_code_only,
				template_pool.model_routing,
				template_pool.model_routing_enabled,
				template_pool.mcp_xml_inject,
				template_pool.supported_model_scopes,
				template_pool.sort_order + 1,
				template_pool.allow_messages_dispatch,
				template_pool.require_oauth_only,
				template_pool.require_privacy_set,
				template_pool.default_mapped_model,
				template_pool.messages_dispatch_model_config,
				template_pool.rpm_limit,
				NOW(),
				NOW()
			FROM candidate_name
			CROSS JOIN template_pool
			WHERE EXISTS (SELECT 1 FROM effective_pro_accounts)
				AND NOT EXISTS (SELECT 1 FROM existing_pro_pool)
			RETURNING id
		),
		changed_groups AS (
			SELECT id FROM normalized_pro_pool
			UNION
			SELECT id FROM inserted_pro_pool
		)
		SELECT COALESCE((SELECT array_agg(id ORDER BY id) FROM changed_groups), '{}'::bigint[])
	`, "{{OPENAI_PLAN_TOKEN}}", openAIPlanTokenSQL("a.credentials", "a.extra"))
	err := scanSingleRow(ctx, exec, query, []any{pq.Array(accountIDs), includePending}, &groupIDs)
	if err != nil {
		return nil, fmt.Errorf("ensure openai pro shared pool: %w", err)
	}
	return append([]int64(nil), groupIDs...), nil
}

func mergeInt64Slices(values ...[]int64) []int64 {
	seen := make(map[int64]struct{})
	out := make([]int64, 0)
	for _, slice := range values {
		for _, value := range slice {
			if value <= 0 {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

type accountGroupQueryOptions struct {
	status      string
	schedulable bool
	platforms   []string // 允许的多个平台，空切片表示不进行平台过滤
}

func (r *accountRepository) queryAccountsByGroup(ctx context.Context, groupID int64, opts accountGroupQueryOptions) ([]service.Account, error) {
	q := r.client.AccountGroup.Query().
		Where(dbaccountgroup.GroupIDEQ(groupID))

	// 通过 account_groups 中间表查询账号，并按需叠加状态/平台/调度能力过滤。
	preds := make([]dbpredicate.Account, 0, 6)
	preds = append(preds, dbaccount.DeletedAtIsNil())
	if opts.schedulable {
		group, err := r.client.Group.Query().
			Where(dbgroup.IDEQ(groupID), dbgroup.DeletedAtIsNil()).
			Only(ctx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return []service.Account{}, nil
			}
			return nil, err
		}
		requiredLevel := service.NormalizeRequiredAccountLevel(group.RequiredAccountLevel)
		if group.Platform == service.PlatformOpenAI && requiredLevel != "" {
			if service.OpenAISharedPoolLevelRank(requiredLevel) == 0 {
				return []service.Account{}, nil
			}
			preds = append(preds,
				dbaccount.PlatformEQ(service.PlatformOpenAI),
				dbaccount.Or(
					dbaccount.TypeEQ(service.AccountTypeAPIKey),
					openAISharedPoolEffectiveAccountLevelPredicate(requiredLevel),
				),
			)
		}
		if isPublicStandardSharedPoolGroupEntity(group) {
			preds = append(preds, sharedPoolSchedulableAccountVisibilityPredicate())
		}
		if group.RequireOauthOnly {
			preds = append(preds, dbaccount.TypeIn(service.AccountTypeOAuth, service.AccountTypeSetupToken))
		}
		if group.RequirePrivacySet {
			preds = append(preds, accountPrivacySetPredicate())
		}
	}
	if opts.status != "" {
		preds = append(preds, dbaccount.StatusEQ(opts.status))
	}
	if len(opts.platforms) > 0 {
		preds = append(preds, dbaccount.PlatformIn(opts.platforms...))
	}
	if opts.schedulable {
		now := time.Now()
		preds = append(preds,
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			accountOverloadInactivePredicate(),
			accountRateLimitInactivePredicate(),
		)
	}

	if len(preds) > 0 {
		q = q.Where(dbaccountgroup.HasAccountWith(preds...))
	}

	groups, err := q.
		Order(
			dbaccountgroup.ByPriority(),
			dbaccountgroup.ByAccountField(dbaccount.FieldPriority),
		).
		WithAccount().
		All(ctx)
	if err != nil {
		return nil, err
	}

	orderedIDs := make([]int64, 0, len(groups))
	accountMap := make(map[int64]*dbent.Account, len(groups))
	for _, ag := range groups {
		if ag.Edges.Account == nil {
			continue
		}
		if _, exists := accountMap[ag.AccountID]; exists {
			continue
		}
		accountMap[ag.AccountID] = ag.Edges.Account
		orderedIDs = append(orderedIDs, ag.AccountID)
	}

	accounts := make([]*dbent.Account, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if acc, ok := accountMap[id]; ok {
			accounts = append(accounts, acc)
		}
	}

	return r.accountsToService(ctx, accounts)
}

func isPublicStandardSharedPoolGroupEntity(group *dbent.Group) bool {
	if group == nil || group.OwnerUserID != nil || group.IsExclusive {
		return false
	}
	if service.NormalizeGroupScope(group.Scope) != service.GroupScopePublic {
		return false
	}
	return service.IsStandardSubscriptionType(group.SubscriptionType)
}

func sharedPoolSchedulableAccountVisibilityPredicate() dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.OwnerUserIDIsNil(),
		dbaccount.And(
			dbaccount.OwnerUserIDNotNil(),
			accountPublicShareApprovedPredicate(),
		),
	)
}

func accountPublicShareApprovedPredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		shareModeCol := s.C(dbaccount.FieldShareMode)
		shareStatusCol := s.C(dbaccount.FieldShareStatus)
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("LOWER(BTRIM(COALESCE(")
			b.Ident(shareModeCol).WriteString(", ").Arg("").WriteString("))) = ").Arg(service.AccountShareModePublic)
			b.WriteString(" AND LOWER(BTRIM(COALESCE(")
			b.Ident(shareStatusCol).WriteString(", ").Arg("").WriteString("))) NOT IN (")
			b.Arg(service.AccountShareStatusPending).WriteString(", ").Arg(service.AccountShareStatusSuspended).WriteString(")")
		}))
	})
}

func accountPrivacySetPredicate() dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.And(
			dbaccount.PlatformEQ(service.PlatformOpenAI),
			accountPrivacyModePredicate(service.PrivacyModeTrainingOff),
		),
		dbaccount.And(
			dbaccount.PlatformEQ(service.PlatformAntigravity),
			accountPrivacyModePredicate(service.AntigravityPrivacySet),
		),
		dbaccount.Not(dbaccount.PlatformIn(service.PlatformOpenAI, service.PlatformAntigravity)),
	)
}

func accountPrivacyModePredicate(mode string) dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, mode, sqljson.Path("privacy_mode")))
	})
}

func (r *accountRepository) accountsToService(ctx context.Context, accounts []*dbent.Account) ([]service.Account, error) {
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(accounts))
	proxyIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		accountIDs = append(accountIDs, acc.ID)
		if acc.ProxyID != nil {
			proxyIDs = append(proxyIDs, *acc.ProxyID)
		}
	}

	proxyMap, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	errorSinceByAccount, err := r.loadAccountErrorSince(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	listingIDsByAccount, err := r.loadAccountShareModeListingIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outAccounts := make([]service.Account, 0, len(accounts))
	for _, acc := range accounts {
		out := accountEntityToService(acc)
		if out == nil {
			continue
		}
		if errorSince, ok := errorSinceByAccount[acc.ID]; ok {
			out.ErrorSince = errorSince
		}
		if acc.ProxyID != nil {
			if proxy, ok := proxyMap[*acc.ProxyID]; ok {
				out.Proxy = proxy
			}
		}
		if groups, ok := groupsByAccount[acc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[acc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[acc.ID]; ok {
			out.AccountGroups = ags
		}
		if listingID, ok := listingIDsByAccount[acc.ID]; ok {
			id := listingID
			out.AccountShareModeListingID = &id
		}
		outAccounts = append(outAccounts, *out)
	}

	return outAccounts, nil
}

func (r *accountRepository) loadAccountShareModeListingIDs(ctx context.Context, accountIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64)
	if len(accountIDs) == 0 {
		return out, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT account_id, id
		FROM account_share_listings
		WHERE account_id = ANY($1)
			AND deleted_at IS NULL
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var listingID int64
		if err := rows.Scan(&accountID, &listingID); err != nil {
			return nil, err
		}
		out[accountID] = listingID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *accountRepository) loadAccountErrorSince(ctx context.Context, accountIDs []int64) (map[int64]*time.Time, error) {
	out := make(map[int64]*time.Time)
	if len(accountIDs) == 0 {
		return out, nil
	}

	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, error_since
		FROM accounts
		WHERE id = ANY($1)
			AND error_since IS NOT NULL
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id int64
		var errorSince time.Time
		if err := rows.Scan(&id, &errorSince); err != nil {
			return nil, err
		}
		value := errorSince
		out[id] = &value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func tempUnschedulablePredicate() dbpredicate.Account {
	return accountTempUnschedulableInactivePredicate()
}

func notExpiredPredicate(now time.Time) dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.ExpiresAtIsNil(),
		dbaccount.ExpiresAtGT(now),
		dbaccount.AutoPauseOnExpiredEQ(false),
	)
}

func (r *accountRepository) loadProxies(ctx context.Context, proxyIDs []int64) (map[int64]*service.Proxy, error) {
	proxyMap := make(map[int64]*service.Proxy)
	proxyIDs = uniquePositiveInt64s(proxyIDs)
	if len(proxyIDs) == 0 {
		return proxyMap, nil
	}

	for start := 0; start < len(proxyIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(proxyIDs) {
			end = len(proxyIDs)
		}
		proxies, err := r.client.Proxy.Query().Where(dbproxy.IDIn(proxyIDs[start:end]...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range proxies {
			proxyMap[p.ID] = proxyEntityToService(p)
		}
	}
	return proxyMap, nil
}

func (r *accountRepository) loadAccountGroups(ctx context.Context, accountIDs []int64) (map[int64][]*service.Group, map[int64][]int64, map[int64][]service.AccountGroup, error) {
	groupsByAccount := make(map[int64][]*service.Group)
	groupIDsByAccount := make(map[int64][]int64)
	accountGroupsByAccount := make(map[int64][]service.AccountGroup)

	accountIDs = uniquePositiveInt64s(accountIDs)
	if len(accountIDs) == 0 {
		return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
	}

	for start := 0; start < len(accountIDs); start += postgresParameterBatchSize {
		end := start + postgresParameterBatchSize
		if end > len(accountIDs) {
			end = len(accountIDs)
		}
		entries, err := r.client.AccountGroup.Query().
			Where(dbaccountgroup.AccountIDIn(accountIDs[start:end]...)).
			WithGroup().
			Order(dbaccountgroup.ByAccountID(), dbaccountgroup.ByPriority()).
			All(ctx)
		if err != nil {
			return nil, nil, nil, err
		}

		for _, ag := range entries {
			groupSvc := groupEntityToService(ag.Edges.Group)
			agSvc := service.AccountGroup{
				AccountID: ag.AccountID,
				GroupID:   ag.GroupID,
				Priority:  ag.Priority,
				CreatedAt: ag.CreatedAt,
				Group:     groupSvc,
			}
			accountGroupsByAccount[ag.AccountID] = append(accountGroupsByAccount[ag.AccountID], agSvc)
			groupIDsByAccount[ag.AccountID] = append(groupIDsByAccount[ag.AccountID], ag.GroupID)
			if groupSvc != nil {
				groupsByAccount[ag.AccountID] = append(groupsByAccount[ag.AccountID], groupSvc)
			}
		}
	}

	return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
}

func (r *accountRepository) loadAccountGroupIDs(ctx context.Context, accountID int64) ([]int64, error) {
	entries, err := r.client.AccountGroup.
		Query().
		Where(dbaccountgroup.AccountIDEQ(accountID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.GroupID)
	}
	return ids, nil
}

func mergeGroupIDs(a []int64, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range a {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range b {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// buildSchedulerGroupPayload must return an untyped nil for empty groups.
// enqueueSchedulerOutbox checks payload as an interface; a typed nil map would
// marshal as "null" and produce a different dedup key from literal nil.
func buildSchedulerGroupPayload(groupIDs []int64) any {
	if len(groupIDs) == 0 {
		return nil
	}
	return map[string]any{"group_ids": groupIDs}
}

func accountEntityToService(m *dbent.Account) *service.Account {
	if m == nil {
		return nil
	}

	rateMultiplier := m.RateMultiplier
	credentials := copyJSONMap(m.Credentials)
	extra := copyJSONMap(m.Extra)

	return &service.Account{
		ID:                      m.ID,
		Name:                    m.Name,
		Notes:                   m.Notes,
		Platform:                m.Platform,
		AccountLevel:            service.NormalizeOpenAIAccountLevel(m.Platform, m.AccountLevel, credentials, extra),
		Type:                    m.Type,
		Credentials:             credentials,
		Extra:                   extra,
		OwnerUserID:             m.OwnerUserID,
		ShareMode:               service.NormalizeAccountShareMode(m.ShareMode),
		ShareStatus:             service.NormalizeAccountShareStatus(m.ShareStatus),
		SharePolicyID:           m.SharePolicyID,
		ProxyID:                 m.ProxyID,
		Concurrency:             m.Concurrency,
		Priority:                m.Priority,
		PrivatePriority:         m.PrivatePriority,
		RateMultiplier:          &rateMultiplier,
		LoadFactor:              m.LoadFactor,
		LoadFactorPaidCeiling:   m.LoadFactorPaidCeiling,
		Status:                  m.Status,
		ErrorMessage:            derefString(m.ErrorMessage),
		LastUsedAt:              m.LastUsedAt,
		ExpiresAt:               m.ExpiresAt,
		AutoPauseOnExpired:      m.AutoPauseOnExpired,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		Schedulable:             m.Schedulable,
		RateLimitedAt:           m.RateLimitedAt,
		RateLimitResetAt:        m.RateLimitResetAt,
		OverloadUntil:           m.OverloadUntil,
		TempUnschedulableUntil:  m.TempUnschedulableUntil,
		TempUnschedulableReason: derefString(m.TempUnschedulableReason),
		SessionWindowStart:      m.SessionWindowStart,
		SessionWindowEnd:        m.SessionWindowEnd,
		SessionWindowStatus:     derefString(m.SessionWindowStatus),
	}
}

func normalizeJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func normalizeLoadFactorPaidCeiling(value int) int {
	if value < service.OwnedPersonalDefaultLoadFactor {
		return service.OwnedPersonalDefaultLoadFactor
	}
	return value
}

func copyJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func joinClauses(clauses []string, sep string) string {
	if len(clauses) == 0 {
		return ""
	}
	out := clauses[0]
	for i := 1; i < len(clauses); i++ {
		out += sep + clauses[i]
	}
	return out
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// FindByExtraField 根据 extra 字段中的键值对查找账号。
// 使用 PostgreSQL JSONB @> 操作符进行高效查询（需要 GIN 索引支持）。
//
// FindByExtraField finds accounts by key-value pairs in the extra field.
// Uses PostgreSQL JSONB @> operator for efficient queries (requires GIN index).
func (r *accountRepository) FindByExtraField(ctx context.Context, key string, value any) ([]service.Account, error) {
	accounts, err := r.client.Account.Query().
		Where(
			dbaccount.DeletedAtIsNil(),
			func(s *entsql.Selector) {
				path := sqljson.Path(key)
				switch v := value.(type) {
				case string:
					preds := []*entsql.Predicate{sqljson.ValueEQ(dbaccount.FieldExtra, v, path)}
					if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
						preds = append(preds, sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path))
					}
					if len(preds) == 1 {
						s.Where(preds[0])
					} else {
						s.Where(entsql.Or(preds...))
					}
				case int:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.Itoa(v), path),
					))
				case int64:
					s.Where(entsql.Or(
						sqljson.ValueEQ(dbaccount.FieldExtra, v, path),
						sqljson.ValueEQ(dbaccount.FieldExtra, strconv.FormatInt(v, 10), path),
					))
				case json.Number:
					if parsed, err := v.Int64(); err == nil {
						s.Where(entsql.Or(
							sqljson.ValueEQ(dbaccount.FieldExtra, parsed, path),
							sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path),
						))
					} else {
						s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, v.String(), path))
					}
				default:
					s.Where(sqljson.ValueEQ(dbaccount.FieldExtra, value, path))
				}
			},
		).
		All(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}

	return r.accountsToService(ctx, accounts)
}

// nowUTC is a SQL expression to generate a UTC RFC3339 timestamp string.
const nowUTC = `to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`

// dailyExpiredExpr is a SQL expression that evaluates to TRUE when daily quota period has expired.
// Supports both rolling (24h from start) and fixed (pre-computed reset_at) modes.
const dailyExpiredExpr = `(
	CASE WHEN COALESCE(extra->>'quota_daily_reset_mode', 'rolling') = 'fixed'
	THEN NOW() >= COALESCE((extra->>'quota_daily_reset_at')::timestamptz, '1970-01-01'::timestamptz)
	ELSE COALESCE((extra->>'quota_daily_start')::timestamptz, '1970-01-01'::timestamptz)
		+ '24 hours'::interval <= NOW()
	END
)`

// weeklyExpiredExpr is a SQL expression that evaluates to TRUE when weekly quota period has expired.
const weeklyExpiredExpr = `(
	CASE WHEN COALESCE(extra->>'quota_weekly_reset_mode', 'rolling') = 'fixed'
	THEN NOW() >= COALESCE((extra->>'quota_weekly_reset_at')::timestamptz, '1970-01-01'::timestamptz)
	ELSE COALESCE((extra->>'quota_weekly_start')::timestamptz, '1970-01-01'::timestamptz)
		+ '168 hours'::interval <= NOW()
	END
)`

// nextDailyResetAtExpr is a SQL expression to compute the next daily reset_at when a reset occurs.
// For fixed mode: computes the next future reset time based on NOW(), timezone, and configured hour.
// This correctly handles long-inactive accounts by jumping directly to the next valid reset point.
const nextDailyResetAtExpr = `(
	CASE WHEN COALESCE(extra->>'quota_daily_reset_mode', 'rolling') = 'fixed'
	THEN to_char((
		-- Compute today's reset point in the configured timezone, then pick next future one
		CASE WHEN NOW() >= (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- NOW() is at or past today's reset point → next reset is tomorrow
		THEN (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
			+ '1 day'::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- NOW() is before today's reset point → next reset is today
		ELSE (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_daily_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		END
	) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	ELSE NULL END
)`

// nextWeeklyResetAtExpr is a SQL expression to compute the next weekly reset_at when a reset occurs.
// For fixed mode: computes the next future reset time based on NOW(), timezone, configured day and hour.
// This correctly handles long-inactive accounts by jumping directly to the next valid reset point.
const nextWeeklyResetAtExpr = `(
	CASE WHEN COALESCE(extra->>'quota_weekly_reset_mode', 'rolling') = 'fixed'
	THEN to_char((
		-- Compute this week's reset point in the configured timezone
		-- Step 1: get today's date at reset hour in configured tz
		-- Step 2: compute days forward to target weekday
		-- Step 3: if same day but past reset hour, advance 7 days
		CASE
		WHEN (
			-- days_forward = (target_day - current_day + 7) % 7
			(COALESCE((extra->>'quota_weekly_reset_day')::int, 1)
			 - EXTRACT(DOW FROM NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))::int
			 + 7) % 7
		) = 0 AND NOW() >= (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		-- Same weekday and past reset hour → next week
		THEN (
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
			+ '7 days'::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		ELSE (
			-- Advance to target weekday this week (or next if days_forward > 0)
			date_trunc('day', NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))
			+ (COALESCE((extra->>'quota_weekly_reset_hour')::int, 0) || ' hours')::interval
			+ ((
				(COALESCE((extra->>'quota_weekly_reset_day')::int, 1)
				 - EXTRACT(DOW FROM NOW() AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC'))::int
				 + 7) % 7
			) || ' days')::interval
		) AT TIME ZONE COALESCE(extra->>'quota_reset_timezone', 'UTC')
		END
	) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	ELSE NULL END
)`

// IncrementQuotaUsed 原子递增账号的配额用量（总/日/周三个维度）
// 日/周额度在周期过期时自动重置为 0 再递增。
// 支持滚动窗口（rolling）和固定时间（fixed）两种重置模式。
func (r *accountRepository) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	rows, err := r.sql.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			-- 总额度：始终递增
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			-- 日额度：仅在 quota_daily_limit > 0 时处理
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				-- 固定模式重置时更新下次重置时间
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			-- 周额度：仅在 quota_weekly_limit > 0 时处理
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				-- 固定模式重置时更新下次重置时间
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, id)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	var newUsed, limit, dailyUsed, dailyLimit, weeklyUsed, weeklyLimit float64
	if rows.Next() {
		if err := rows.Scan(&newUsed, &limit, &dailyUsed, &dailyLimit, &weeklyUsed, &weeklyLimit); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 任一维度配额刚超限时触发调度快照刷新
	crossedTotal := limit > 0 && newUsed >= limit && (newUsed-amount) < limit
	crossedDaily := dailyLimit > 0 && dailyUsed >= dailyLimit && (dailyUsed-amount) < dailyLimit
	crossedWeekly := weeklyLimit > 0 && weeklyUsed >= weeklyLimit && (weeklyUsed-amount) < weeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", id, err)
		}
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

// ResetQuotaUsed 重置账号所有维度的配额用量为 0
// 保留固定重置模式的配置字段（quota_daily_reset_mode 等），仅清零用量和窗口起始时间
func (r *accountRepository) ResetQuotaUsed(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| '{"quota_used": 0, "quota_daily_used": 0, "quota_weekly_used": 0}'::jsonb
		) - 'quota_daily_start' - 'quota_weekly_start' - 'quota_daily_reset_at' - 'quota_weekly_reset_at', updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`,
		id)
	if err != nil {
		return err
	}
	// 重置配额后触发调度快照刷新，使账号重新参与调度
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue quota reset failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}
