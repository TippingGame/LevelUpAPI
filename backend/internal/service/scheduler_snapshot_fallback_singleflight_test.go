package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type fallbackSingleflightCache struct {
	SchedulerCache

	expectedReads int32
	reads         atomic.Int32
	allRead       chan struct{}
	readOnce      sync.Once
	captures      atomic.Int32
	writes        atomic.Int32
}

func (c *fallbackSingleflightCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	if c.expectedReads > 0 && c.reads.Add(1) == c.expectedReads {
		c.readOnce.Do(func() { close(c.allRead) })
	}
	if c.expectedReads > 0 {
		<-c.allRead
	}
	return nil, false, nil
}

func (c *fallbackSingleflightCache) CaptureBucketWriteToken(_ context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error) {
	c.captures.Add(1)
	return SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (c *fallbackSingleflightCache) SetSnapshot(_ context.Context, bucket SchedulerBucket, token SchedulerBucketWriteToken, _ []Account) error {
	if !token.ValidFor(bucket) {
		return fmt.Errorf("invalid scheduler write token")
	}
	c.writes.Add(1)
	return nil
}

type fallbackSingleflightAccountRepo struct {
	AccountRepository

	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	dbCalls   atomic.Int32
	accounts  []Account
}

func (r *fallbackSingleflightAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	r.dbCalls.Add(1)
	r.startOnce.Do(func() { close(r.started) })
	<-r.release
	return append([]Account(nil), r.accounts...), nil
}

func TestSchedulerFallbackCoalescesConcurrentBucketMisses(t *testing.T) {
	const callers = 16
	bucket := SchedulerBucket{GroupID: 701, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
	cache := &fallbackSingleflightCache{
		expectedReads: callers,
		allRead:       make(chan struct{}),
	}
	repo := &fallbackSingleflightAccountRepo{
		started: make(chan struct{}),
		release: make(chan struct{}),
		accounts: []Account{{
			ID:          70101,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
		}},
	}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, &config.Config{
		RunMode: config.RunModeStandard,
		Gateway: config.GatewayConfig{Scheduling: config.GatewaySchedulingConfig{
			DbFallbackEnabled: true,
		}},
	})

	type result struct {
		accounts []Account
		err      error
	}
	results := make(chan result, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			groupID := bucket.GroupID
			accounts, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, bucket.Platform, false)
			results <- result{accounts: accounts, err: err}
		}()
	}

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler fallback did not reach the repository")
	}
	require.Eventually(t, func() bool { return repo.dbCalls.Load() == 1 }, time.Second, time.Millisecond)
	// Keep the leader in the repository long enough for every caller that
	// passed the synchronized cache miss to register as a singleflight waiter.
	time.Sleep(50 * time.Millisecond)
	close(repo.release)
	wg.Wait()
	close(results)

	for got := range results {
		require.NoError(t, got.err)
		require.Len(t, got.accounts, 1)
		require.Equal(t, int64(70101), got.accounts[0].ID)
	}
	require.EqualValues(t, 1, repo.dbCalls.Load())
	require.EqualValues(t, 1, cache.captures.Load())
	require.EqualValues(t, 1, cache.writes.Load())
}

func TestSchedulerEmptyFallbackIsBrieflyReused(t *testing.T) {
	bucket := SchedulerBucket{GroupID: 702, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
	cache := &fallbackSingleflightCache{}
	repo := &fallbackSingleflightAccountRepo{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	close(repo.release)
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, &config.Config{
		RunMode: config.RunModeStandard,
		Gateway: config.GatewayConfig{Scheduling: config.GatewaySchedulingConfig{
			DbFallbackEnabled: true,
		}},
	})

	groupID := bucket.GroupID
	first, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, bucket.Platform, false)
	require.NoError(t, err)
	require.Empty(t, first)
	second, _, err := svc.ListSchedulableAccounts(context.Background(), &groupID, bucket.Platform, false)
	require.NoError(t, err)
	require.Empty(t, second)
	require.EqualValues(t, 1, repo.dbCalls.Load(), "an empty bucket should not query PostgreSQL for every request")
}
