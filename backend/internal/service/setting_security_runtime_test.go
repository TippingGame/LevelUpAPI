package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type securitySettingsRepoStub struct {
	mu               sync.Mutex
	values           map[string]string
	getValueCalls    int
	getMultipleCalls int
	delay            time.Duration
}

func (r *securitySettingsRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *securitySettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getValueCalls++
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *securitySettingsRepoStub) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *securitySettingsRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	r.getMultipleCalls++
	delay := r.delay
	r.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *securitySettingsRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *securitySettingsRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]string, len(r.values))
	for key, value := range r.values {
		result[key] = value
	}
	return result, nil
}

func (r *securitySettingsRepoStub) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

func (r *securitySettingsRepoStub) calls() (getValue, getMultiple int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getValueCalls, r.getMultipleCalls
}

func TestSecuritySwitchesShareHotPathCache(t *testing.T) {
	repo := &securitySettingsRepoStub{values: map[string]string{
		SettingKeySessionBindingEnabled: "true",
		SettingKeyStepUpEnabled:         "false",
	}}
	svc := NewSettingService(repo, &config.Config{})

	require.True(t, svc.IsSessionBindingEnabled(context.Background()))
	require.False(t, svc.IsStepUpEnabled(context.Background()))
	require.True(t, svc.IsSessionBindingEnabled(context.Background()))

	getValueCalls, getMultipleCalls := repo.calls()
	require.Zero(t, getValueCalls)
	require.Equal(t, 1, getMultipleCalls)
}

func TestSecuritySwitchesCollapseConcurrentCacheMisses(t *testing.T) {
	repo := &securitySettingsRepoStub{
		values: map[string]string{
			SettingKeySessionBindingEnabled: "true",
			SettingKeyStepUpEnabled:         "true",
		},
		delay: 25 * time.Millisecond,
	}
	svc := NewSettingService(repo, &config.Config{})

	const readers = 32
	start := make(chan struct{})
	results := make(chan bool, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			if index%2 == 0 {
				results <- svc.IsSessionBindingEnabled(context.Background())
				return
			}
			results <- svc.IsStepUpEnabled(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	for result := range results {
		require.True(t, result)
	}
	_, getMultipleCalls := repo.calls()
	require.Equal(t, 1, getMultipleCalls)
}

func TestSecuritySwitchCacheRefreshUsesSavedValuesImmediately(t *testing.T) {
	repo := &securitySettingsRepoStub{values: map[string]string{
		SettingKeySessionBindingEnabled: "false",
		SettingKeyStepUpEnabled:         "false",
	}}
	svc := NewSettingService(repo, &config.Config{})

	require.False(t, svc.IsSessionBindingEnabled(context.Background()))
	svc.refreshSecuritySwitchesCache(true, true)
	require.True(t, svc.IsSessionBindingEnabled(context.Background()))
	require.True(t, svc.IsStepUpEnabled(context.Background()))

	_, getMultipleCalls := repo.calls()
	require.Equal(t, 1, getMultipleCalls)
}
