package service

import (
	"context"
	"log/slog"
)

func shouldApplyRetryableFailoverTempUnschedule(ctx context.Context, repo AccountRepository, accountID int64, failoverErr *UpstreamFailoverError) bool {
	if repo == nil || accountID <= 0 || failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return false
	}
	readCtx, cancel := retryableErrorStateContext(ctx)
	defer cancel()
	account, err := repo.GetByID(readCtx, accountID)
	if err != nil || account == nil {
		slog.Warn("retryable_failover_temp_unschedule_account_load_failed", "account_id", accountID, "error", err)
		return true
	}
	if failoverErr.StatusCode > 0 {
		if !shouldApplyLocalErrorState(account, failoverErr.StatusCode) {
			slog.Info("retryable_failover_temp_unschedule_skipped", "account_id", accountID, "status_code", failoverErr.StatusCode)
			return false
		}
		return true
	}
	if !shouldApplyLocalSystemErrorState(account) {
		slog.Info("retryable_failover_temp_unschedule_skipped", "account_id", accountID, "status_code", failoverErr.StatusCode)
		return false
	}
	return true
}
