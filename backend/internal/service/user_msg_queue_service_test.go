//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cleanupWorkerUserMsgQueueCache struct {
	scanCalls         atomic.Int64
	forceReleaseCalls atomic.Int64
	maxCount          atomic.Int64
}

var _ UserMsgQueueCache = (*cleanupWorkerUserMsgQueueCache)(nil)

func (c *cleanupWorkerUserMsgQueueCache) AcquireLock(context.Context, int64, string, int) (bool, error) {
	return true, nil
}

func (c *cleanupWorkerUserMsgQueueCache) ReleaseLock(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (c *cleanupWorkerUserMsgQueueCache) GetLastCompletedMs(context.Context, int64) (int64, error) {
	return 0, nil
}

func (c *cleanupWorkerUserMsgQueueCache) GetCurrentTimeMs(context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

func (c *cleanupWorkerUserMsgQueueCache) ScanLockKeys(_ context.Context, maxCount int) ([]int64, error) {
	c.scanCalls.Add(1)
	c.maxCount.Store(int64(maxCount))
	return []int64{42}, nil
}

func (c *cleanupWorkerUserMsgQueueCache) ForceReleaseLock(context.Context, int64) error {
	c.forceReleaseCalls.Add(1)
	return nil
}

func TestStartCleanupWorker_ScansAndReleasesOrphanedLocks(t *testing.T) {
	cache := &cleanupWorkerUserMsgQueueCache{}
	svc := NewUserMessageQueueService(cache, nil, nil)
	defer svc.Stop()

	svc.StartCleanupWorker(time.Millisecond)

	require.Eventually(t, func() bool {
		return cache.scanCalls.Load() > 0 && cache.forceReleaseCalls.Load() > 0
	}, time.Second, 10*time.Millisecond)
	require.EqualValues(t, 1000, cache.maxCount.Load())
}
