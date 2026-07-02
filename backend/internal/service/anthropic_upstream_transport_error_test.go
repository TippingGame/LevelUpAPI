package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type anthropicTransportAccountRepoStub struct {
	AccountRepository

	tempCalls      int
	lastAccountID  int64
	lastTempUntil  time.Time
	lastTempReason string
}

func (r *anthropicTransportAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastAccountID = id
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func TestClassifyAnthropicTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "proxy authentication required", err: errors.New("proxy authentication required"), want: true},
		{name: "connection refused", err: errors.New("dial tcp: connect: connection refused"), want: true},
		{name: "dns not found", err: errors.New("lookup proxy.local: no such host"), want: true},
		{name: "timeout remains transient", err: errors.New("i/o timeout"), want: false},
		{name: "deadline remains transient", err: context.DeadlineExceeded, want: false},
		{name: "canceled remains transient", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyAnthropicTransportError(tt.err).Persistent)
		})
	}
}

func TestMaybeTempUnscheduleAnthropicTransportError(t *testing.T) {
	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}

	account := &Account{
		ID:       42,
		Name:     "claude-oauth",
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), account, errors.New("proxy authentication required"), "proxy authentication required")

	require.Equal(t, 1, repo.tempCalls)
	require.Equal(t, int64(42), repo.lastAccountID)
	require.Contains(t, repo.lastTempReason, "proxy/network")
	require.Contains(t, repo.lastTempReason, "proxy authentication required")
	require.WithinDuration(t, time.Now().Add(anthropicTransportErrorTempUnschedDuration), repo.lastTempUntil, 2*time.Second)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempReason, account.TempUnschedulableReason)
}

func TestMaybeTempUnscheduleAnthropicTransportErrorSkipsNonPersistentAndAPIKey(t *testing.T) {
	repo := &anthropicTransportAccountRepoStub{}
	svc := &GatewayService{accountRepo: repo}

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), &Account{
		ID:       43,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}, errors.New("i/o timeout"), "i/o timeout")

	svc.maybeTempUnscheduleAnthropicTransportError(context.Background(), &Account{
		ID:       44,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
	}, errors.New("proxy authentication required"), "proxy authentication required")

	require.Zero(t, repo.tempCalls)
}
