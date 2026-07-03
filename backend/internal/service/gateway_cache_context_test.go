package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type gatewayCacheContextRecorder struct {
	accountBindings map[string]int64
	stringBindings  map[string]string

	setAccountContextErr     error
	deleteAccountContextErr  error
	refreshAccountContextErr error
	setStringContextErr      error
	deleteStringContextErr   error
}

func (c *gatewayCacheContextRecorder) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if id, ok := c.accountBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (c *gatewayCacheContextRecorder) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.setAccountContextErr = ctx.Err()
	if c.accountBindings == nil {
		c.accountBindings = make(map[string]int64)
	}
	c.accountBindings[sessionHash] = accountID
	return nil
}

func (c *gatewayCacheContextRecorder) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	c.refreshAccountContextErr = ctx.Err()
	return nil
}

func (c *gatewayCacheContextRecorder) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	c.deleteAccountContextErr = ctx.Err()
	delete(c.accountBindings, sessionHash)
	return nil
}

func (c *gatewayCacheContextRecorder) GetSessionString(ctx context.Context, groupID int64, sessionHash string) (string, error) {
	if value, ok := c.stringBindings[sessionHash]; ok {
		return value, nil
	}
	return "", ErrGatewaySessionStringNotFound
}

func (c *gatewayCacheContextRecorder) SetSessionString(ctx context.Context, groupID int64, sessionHash string, value string, ttl time.Duration) error {
	c.setStringContextErr = ctx.Err()
	if c.stringBindings == nil {
		c.stringBindings = make(map[string]string)
	}
	c.stringBindings[sessionHash] = value
	return nil
}

func (c *gatewayCacheContextRecorder) DeleteSessionString(ctx context.Context, groupID int64, sessionHash string) error {
	c.deleteStringContextErr = ctx.Err()
	delete(c.stringBindings, sessionHash)
	return nil
}

func canceledContextForCacheTest() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestGatewayServiceStickyCacheWritesSurviveCanceledContext(t *testing.T) {
	cache := &gatewayCacheContextRecorder{accountBindings: map[string]int64{"sticky": 7}}
	svc := &GatewayService{cache: cache}
	ctx := canceledContextForCacheTest()

	require.NoError(t, svc.BindStickySession(ctx, nil, "sticky", 7))
	require.NoError(t, cache.setAccountContextErr)
	require.Equal(t, int64(7), cache.accountBindings["sticky"])

	require.NoError(t, svc.refreshStickySessionTTL(ctx, nil, "sticky", time.Minute))
	require.NoError(t, cache.refreshAccountContextErr)

	cleared, err := svc.ClearStickySessionIfBoundTo(ctx, nil, "sticky", 7)
	require.NoError(t, err)
	require.True(t, cleared)
	require.NoError(t, cache.deleteAccountContextErr)
	require.NotContains(t, cache.accountBindings, "sticky")
}

func TestGatewayServiceAffinityCacheWritesSurviveCanceledContext(t *testing.T) {
	cache := &gatewayCacheContextRecorder{}
	svc := &GatewayService{cache: cache}
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	ctx := canceledContextForCacheTest()

	key := svc.buildClientAffinityKey("account:acct-1:device:dev-1", 9)
	svc.bindClientAffinityAccount(ctx, nil, key, account)
	require.NoError(t, cache.setStringContextErr)
	require.Equal(t, "42", cache.stringBindings[key])

	cleared, err := svc.ClearClientAffinityIfBoundTo(ctx, nil, "account:acct-1:device:dev-1", 9, 42)
	require.NoError(t, err)
	require.True(t, cleared)
	require.NoError(t, cache.deleteStringContextErr)
	require.NotContains(t, cache.stringBindings, key)
}

func TestOpenAIStickyCacheWritesSurviveCanceledContext(t *testing.T) {
	cache := &gatewayCacheContextRecorder{accountBindings: map[string]int64{}}
	svc := &OpenAIGatewayService{cache: cache}
	ctx := canceledContextForCacheTest()

	require.NoError(t, svc.setStickySessionAccountID(ctx, nil, "openai-sticky", 99, time.Minute))
	require.NoError(t, cache.setAccountContextErr)

	require.NoError(t, svc.refreshStickySessionTTL(ctx, nil, "openai-sticky", time.Minute))
	require.NoError(t, cache.refreshAccountContextErr)

	require.NoError(t, svc.deleteStickySessionAccountID(ctx, nil, "openai-sticky"))
	require.NoError(t, cache.deleteAccountContextErr)
}
