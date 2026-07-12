package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulerSnapshotPlatformsIncludeGrok(t *testing.T) {
	require.Contains(t, schedulerSnapshotPlatforms(), PlatformGrok)
}

func TestSchedulerSnapshotDefaultBucketsIncludeGrok(t *testing.T) {
	snapshotService := &SchedulerSnapshotService{}
	buckets, err := snapshotService.defaultBuckets(context.Background())
	require.NoError(t, err)
	require.Contains(t, buckets, SchedulerBucket{GroupID: 0, Platform: PlatformGrok, Mode: SchedulerModeSingle})
	require.Contains(t, buckets, SchedulerBucket{GroupID: 0, Platform: PlatformGrok, Mode: SchedulerModeForced})
}
