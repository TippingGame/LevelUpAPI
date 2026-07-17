package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type adminVideoGroupRepoStub struct {
	GroupRepository
	existing *Group
	created  *Group
	updated  *Group
}

func (r *adminVideoGroupRepoStub) Create(_ context.Context, group *Group) error {
	r.created = group
	return nil
}

func (r *adminVideoGroupRepoStub) GetByID(_ context.Context, _ int64) (*Group, error) {
	return r.existing, nil
}

func (r *adminVideoGroupRepoStub) Update(_ context.Context, group *Group) error {
	r.updated = group
	return nil
}

func TestAdminServiceCreateGroupWithVideoPricing(t *testing.T) {
	repo := &adminVideoGroupRepoStub{}
	svc := &adminServiceImpl{groupRepo: repo}
	multiplier := 1.25
	price480P := 0.05
	price720P := 0.07
	price1080P := 0.25

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                 "grok-video",
		Platform:             PlatformGrok,
		RateMultiplier:       1,
		VideoRateIndependent: true,
		VideoRateMultiplier:  &multiplier,
		VideoPrice480P:       &price480P,
		VideoPrice720P:       &price720P,
		VideoPrice1080P:      &price1080P,
	})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.created)
	require.True(t, repo.created.VideoRateIndependent)
	require.InDelta(t, multiplier, repo.created.VideoRateMultiplier, 1e-12)
	require.InDelta(t, price480P, *repo.created.VideoPrice480P, 1e-12)
	require.InDelta(t, price720P, *repo.created.VideoPrice720P, 1e-12)
	require.InDelta(t, price1080P, *repo.created.VideoPrice1080P, 1e-12)
}

func TestAdminServiceUpdateGroupPartialVideoPricing(t *testing.T) {
	price480P := 0.05
	price720P := 0.07
	price1080P := 0.25
	existing := &Group{
		ID:                   1,
		Name:                 "grok-video",
		Platform:             PlatformGrok,
		Status:               StatusActive,
		VideoRateIndependent: true,
		VideoRateMultiplier:  1.25,
		VideoPrice480P:       &price480P,
		VideoPrice720P:       &price720P,
		VideoPrice1080P:      &price1080P,
	}
	repo := &adminVideoGroupRepoStub{existing: existing}
	svc := &adminServiceImpl{groupRepo: repo}
	independent := false
	multiplier := 0.8
	newPrice720P := 0.09

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		VideoRateIndependent: &independent,
		VideoRateMultiplier:  &multiplier,
		VideoPrice720P:       &newPrice720P,
	})

	require.NoError(t, err)
	require.NotNil(t, group)
	require.NotNil(t, repo.updated)
	require.False(t, repo.updated.VideoRateIndependent)
	require.InDelta(t, multiplier, repo.updated.VideoRateMultiplier, 1e-12)
	require.InDelta(t, price480P, *repo.updated.VideoPrice480P, 1e-12)
	require.InDelta(t, newPrice720P, *repo.updated.VideoPrice720P, 1e-12)
	require.InDelta(t, price1080P, *repo.updated.VideoPrice1080P, 1e-12)
}

func TestAdminServiceUpdateGroupRejectsNegativeVideoMultiplier(t *testing.T) {
	existing := &Group{ID: 1, Name: "grok-video", Platform: PlatformGrok, Status: StatusActive}
	repo := &adminVideoGroupRepoStub{existing: existing}
	svc := &adminServiceImpl{groupRepo: repo}
	multiplier := -0.1

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		VideoRateMultiplier: &multiplier,
	})

	require.ErrorContains(t, err, "video_rate_multiplier must be >= 0")
	require.Nil(t, group)
	require.Nil(t, repo.updated)
}
