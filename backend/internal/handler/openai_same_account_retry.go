package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

type openAISameAccountRetryAction int

const (
	openAISameAccountRetryNoop openAISameAccountRetryAction = iota
	openAISameAccountRetryContinue
	openAISameAccountRetryCanceled
)

func handleOpenAISameAccountRetry(
	ctx context.Context,
	tempUnscheduler TempUnscheduler,
	accountID int64,
	retryLimit int,
	failoverErr *service.UpstreamFailoverError,
	retryCounts map[int64]int,
	retryDelay time.Duration,
	reqLog *zap.Logger,
	retryLogMessage string,
) openAISameAccountRetryAction {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return openAISameAccountRetryNoop
	}
	if retryLimit < 0 {
		retryLimit = 0
	}
	retryCount := 0
	if retryCounts != nil {
		retryCount = retryCounts[accountID]
	}
	if retryCount < retryLimit {
		retryCount++
		if retryCounts != nil {
			retryCounts[accountID] = retryCount
		}
		if reqLog != nil {
			reqLog.Warn(retryLogMessage,
				zap.Int64("account_id", accountID),
				zap.Int("upstream_status", failoverErr.StatusCode),
				zap.Int("retry_limit", retryLimit),
				zap.Int("retry_count", retryCount),
			)
		}
		if !sleepWithContext(ctx, retryDelay) {
			return openAISameAccountRetryCanceled
		}
		return openAISameAccountRetryContinue
	}

	if tempUnscheduler != nil {
		tempUnscheduler.TempUnscheduleRetryableError(ctx, accountID, failoverErr)
	}
	if reqLog != nil {
		reqLog.Warn(retryLogMessage+"_exhausted",
			zap.Int64("account_id", accountID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("retry_limit", retryLimit),
			zap.Int("retry_count", retryCount),
		)
	}
	return openAISameAccountRetryNoop
}
