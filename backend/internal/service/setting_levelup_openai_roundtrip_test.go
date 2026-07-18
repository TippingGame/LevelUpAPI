package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type levelUpOpenAISettingRepoStub struct {
	values map[string]string
}

func (s *levelUpOpenAISettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *levelUpOpenAISettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *levelUpOpenAISettingRepoStub) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *levelUpOpenAISettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *levelUpOpenAISettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *levelUpOpenAISettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *levelUpOpenAISettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func TestSettingServiceParsesLevelUpOpenAISettings(t *testing.T) {
	svc := NewSettingService(&levelUpOpenAISettingRepoStub{}, &config.Config{})

	got := svc.parseSettings(map[string]string{
		SettingKeyOpenAICleanRelayEnabled:                   "true",
		SettingKeyOpenAIFreeAccountRepairEnabled:            "true",
		SettingKeyOpenAIFreeAccountRepairWeeklyThresholdUSD: "72.5",
	})
	require.True(t, got.OpenAICleanRelayEnabled)
	require.True(t, got.OpenAIFreeAccountRepairEnabled)
	require.Equal(t, 72.5, got.OpenAIFreeAccountRepairWeeklyThresholdUSD)

	defaults := svc.parseSettings(map[string]string{})
	require.False(t, defaults.OpenAICleanRelayEnabled)
	require.False(t, defaults.OpenAIFreeAccountRepairEnabled)
	require.Equal(t, 60.0, defaults.OpenAIFreeAccountRepairWeeklyThresholdUSD)

	invalid := svc.parseSettings(map[string]string{
		SettingKeyOpenAIFreeAccountRepairWeeklyThresholdUSD: "NaN",
	})
	require.Equal(t, 60.0, invalid.OpenAIFreeAccountRepairWeeklyThresholdUSD)
}

func TestSettingServicePersistsLevelUpOpenAISettingsAndRefreshesCleanRelayCache(t *testing.T) {
	repo := &levelUpOpenAISettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	settings := &SystemSettings{
		OpenAICleanRelayEnabled:                   true,
		OpenAIFreeAccountRepairEnabled:            true,
		OpenAIFreeAccountRepairWeeklyThresholdUSD: 85.25,
	}

	require.NoError(t, svc.UpdateSettings(context.Background(), settings))
	require.Equal(t, "true", repo.values[SettingKeyOpenAICleanRelayEnabled])
	require.Equal(t, "true", repo.values[SettingKeyOpenAIFreeAccountRepairEnabled])
	require.Equal(t, "85.25", repo.values[SettingKeyOpenAIFreeAccountRepairWeeklyThresholdUSD])

	cached, ok := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
	require.True(t, ok)
	require.True(t, cached.openAICleanRelay)
}

func TestSettingServiceNormalizesOrRejectsInvalidOpenAIFreeAccountRepairThreshold(t *testing.T) {
	t.Run("zero uses runtime default", func(t *testing.T) {
		repo := &levelUpOpenAISettingRepoStub{values: map[string]string{}}
		svc := NewSettingService(repo, &config.Config{})
		settings := &SystemSettings{}

		require.NoError(t, svc.UpdateSettings(context.Background(), settings))
		require.Equal(t, "60", repo.values[SettingKeyOpenAIFreeAccountRepairWeeklyThresholdUSD])
		require.Equal(t, 60.0, settings.OpenAIFreeAccountRepairWeeklyThresholdUSD)
	})

	for _, threshold := range []float64{-1, math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			svc := NewSettingService(&levelUpOpenAISettingRepoStub{values: map[string]string{}}, &config.Config{})
			err := svc.UpdateSettings(context.Background(), &SystemSettings{
				OpenAIFreeAccountRepairWeeklyThresholdUSD: threshold,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "finite positive number")
		})
	}
}
