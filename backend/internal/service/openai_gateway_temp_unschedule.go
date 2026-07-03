package service

import (
	"context"
	"net/http"
)

// TempUnscheduleRetryableError persists temporary unscheduling for OpenAI
// pool-mode retry errors once same-account retries are exhausted.
func (s *OpenAIGatewayService) TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return
	}
	account, ok := retryableFailoverTempUnscheduleAccount(ctx, s.accountRepo, accountID, failoverErr)
	if !ok {
		return
	}
	var tempUnschedCache TempUnschedCache
	if s.rateLimitService != nil {
		tempUnschedCache = s.rateLimitService.tempUnschedCache
	}
	switch failoverErr.StatusCode {
	case http.StatusBadRequest:
		tempUnscheduleGoogleConfigError(ctx, s.accountRepo, tempUnschedCache, account, "[openai_handler]")
	case http.StatusBadGateway:
		tempUnscheduleEmptyResponse(ctx, s.accountRepo, tempUnschedCache, account, "[openai_handler]")
	default:
		tempUnscheduleRetryableStatusError(ctx, s.accountRepo, tempUnschedCache, account, failoverErr.StatusCode, failoverErr.ResponseBody, "[openai_handler]")
	}
}
