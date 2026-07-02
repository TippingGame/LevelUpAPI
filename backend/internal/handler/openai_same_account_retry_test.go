package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type openAITempUnschedulerMock struct {
	calls []openAITempUnscheduleCall
}

type openAITempUnscheduleCall struct {
	accountID   int64
	failoverErr *service.UpstreamFailoverError
}

func (m *openAITempUnschedulerMock) TempUnscheduleRetryableError(_ context.Context, accountID int64, failoverErr *service.UpstreamFailoverError) {
	m.calls = append(m.calls, openAITempUnscheduleCall{
		accountID:   accountID,
		failoverErr: failoverErr,
	})
}

func TestHandleOpenAISameAccountRetry_RetriesBeforeLimit(t *testing.T) {
	retryCounts := map[int64]int{}
	mock := &openAITempUnschedulerMock{}
	failoverErr := &service.UpstreamFailoverError{
		StatusCode:             http.StatusTooManyRequests,
		RetryableOnSameAccount: true,
	}

	action := handleOpenAISameAccountRetry(context.Background(), mock, 100, 2, failoverErr, retryCounts, 0, zap.NewNop(), "test.openai.retry")

	require.Equal(t, openAISameAccountRetryContinue, action)
	require.Equal(t, 1, retryCounts[int64(100)])
	require.Empty(t, mock.calls)
}

func TestHandleOpenAISameAccountRetry_TempUnschedulesWhenLimitExhausted(t *testing.T) {
	retryCounts := map[int64]int{100: 2}
	mock := &openAITempUnschedulerMock{}
	failoverErr := &service.UpstreamFailoverError{
		StatusCode:             http.StatusTooManyRequests,
		ResponseBody:           []byte(`{"error":"rate_limit"}`),
		RetryableOnSameAccount: true,
	}

	action := handleOpenAISameAccountRetry(context.Background(), mock, 100, 2, failoverErr, retryCounts, 0, zap.NewNop(), "test.openai.retry")

	require.Equal(t, openAISameAccountRetryNoop, action)
	require.Equal(t, 2, retryCounts[int64(100)])
	require.Len(t, mock.calls, 1)
	require.Equal(t, int64(100), mock.calls[0].accountID)
	require.Same(t, failoverErr, mock.calls[0].failoverErr)
}

func TestHandleOpenAISameAccountRetry_IgnoresNonRetryableError(t *testing.T) {
	retryCounts := map[int64]int{}
	mock := &openAITempUnschedulerMock{}
	failoverErr := &service.UpstreamFailoverError{StatusCode: http.StatusInternalServerError}

	action := handleOpenAISameAccountRetry(context.Background(), mock, 100, 2, failoverErr, retryCounts, 0, zap.NewNop(), "test.openai.retry")

	require.Equal(t, openAISameAccountRetryNoop, action)
	require.Empty(t, retryCounts)
	require.Empty(t, mock.calls)
}

func TestHandleOpenAISameAccountRetry_ReturnsCanceledWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retryCounts := map[int64]int{}
	mock := &openAITempUnschedulerMock{}
	failoverErr := &service.UpstreamFailoverError{
		StatusCode:             http.StatusTooManyRequests,
		RetryableOnSameAccount: true,
	}

	action := handleOpenAISameAccountRetry(ctx, mock, 100, 2, failoverErr, retryCounts, time.Second, zap.NewNop(), "test.openai.retry")

	require.Equal(t, openAISameAccountRetryCanceled, action)
	require.Equal(t, 1, retryCounts[int64(100)])
	require.Empty(t, mock.calls)
}
