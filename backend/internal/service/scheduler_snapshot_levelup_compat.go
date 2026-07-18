package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func (s *SchedulerSnapshotService) captureBucketWriteToken(ctx context.Context, bucket SchedulerBucket) (token SchedulerBucketWriteToken, err error) {
	if s == nil || s.cache == nil {
		return SchedulerBucketWriteToken{}, ErrSchedulerCacheNotReady
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			token = SchedulerBucketWriteToken{}
			err = fmt.Errorf("capture scheduler bucket write token: %v", recovered)
		}
	}()
	return s.cache.CaptureBucketWriteToken(ctx, bucket)
}

func filterSchedulableAccounts(accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.IsSchedulable() {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func (s *SchedulerSnapshotService) candidateIndexLimit() int {
	if s == nil || s.cfg == nil || s.cfg.Gateway.Scheduling.IndexedCandidateLimit <= 0 {
		return 256
	}
	return s.cfg.Gateway.Scheduling.IndexedCandidateLimit
}

func (s *SchedulerSnapshotService) filterCachedSchedulableAccounts(ctx context.Context, bucket SchedulerBucket, cached []*Account, source string) ([]Account, bool) {
	accounts := derefAccounts(cached)
	accounts, stale := s.repairCachedAccountsMissingRequiredProxy(ctx, accounts)
	filtered := filterSchedulableAccounts(accounts)
	if stale {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] cached account metadata missing required proxy hydration: bucket=%s source=%s", bucket.String(), source)
	}
	return filtered, stale
}

func (s *SchedulerSnapshotService) repairCachedAccountsMissingRequiredProxy(ctx context.Context, accounts []Account) ([]Account, bool) {
	if len(accounts) == 0 || s == nil || s.cache == nil {
		return accounts, false
	}
	stale := false
	for i := range accounts {
		if !accountMissingRequiredProxyHydration(&accounts[i]) {
			continue
		}
		full, err := s.cache.GetAccount(ctx, accounts[i].ID)
		if err != nil || full == nil || accountMissingRequiredProxyHydration(full) {
			stale = true
			continue
		}
		accounts[i] = *full
		if err := s.cache.SetAccount(ctx, full); err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] refresh account metadata after proxy hydration failed: account=%d err=%v", full.ID, err)
		}
	}
	return accounts, stale
}

func accountMissingRequiredProxyHydration(account *Account) bool {
	if account == nil || !account.RequiresProxyForScheduling() {
		return false
	}
	return !account.HasCompleteRequiredProxyForScheduling()
}

func filterSchedulableAccountsForSnapshot(accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.IsSchedulableWithoutCodexQuotaProtection() {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func (s *SchedulerSnapshotService) defaultBuckets(ctx context.Context) ([]SchedulerBucket, error) {
	buckets := make([]SchedulerBucket, 0)
	for _, platform := range schedulerSnapshotPlatforms() {
		buckets = append(buckets,
			SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeSingle},
			SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeForced},
		)
		if platform == PlatformAnthropic || platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeMixed})
		}
	}
	if s.isRunModeSimple() || s.groupRepo == nil {
		return dedupeBuckets(buckets), nil
	}
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return dedupeBuckets(buckets), nil
	}
	for _, group := range groups {
		if group.Platform == "" {
			continue
		}
		buckets = append(buckets,
			SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeSingle},
			SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeForced},
		)
		if group.Platform == PlatformAnthropic || group.Platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeMixed})
		}
	}
	return dedupeBuckets(buckets), nil
}
