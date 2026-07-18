package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokBillingEligibilityAccountRepo struct {
	AccountRepository
	account *Account
	updates map[string]any
}

func (r *grokBillingEligibilityAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account != nil && r.account.ID == id {
		return r.account, nil
	}
	return nil, nil
}

func (r *grokBillingEligibilityAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

type grokBillingEligibilityUpstream struct {
	HTTPUpstream
	weeklyStatus  int
	monthlyStatus int
}

func (u *grokBillingEligibilityUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	status := u.monthlyStatus
	payload := `{"config":{"billingPeriodStart":"2026-07-01T00:00:00Z","billingPeriodEnd":"2026-08-01T00:00:00Z","monthlyLimit":{"val":15000}}}`
	if req.URL.RawQuery == "format=credits" {
		status = u.weeklyStatus
		payload = `{"config":{"currentPeriod":{"type":"WEEKLY","start":"2026-07-09T00:00:00Z","end":"2026-07-16T00:00:00Z"},"creditUsagePercent":25}}`
	}
	if status == 0 {
		status = http.StatusOK
	}
	if status != http.StatusOK {
		payload = `{"error":{"message":"billing unavailable"}}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(payload)),
	}, nil
}

func newGrokBillingEligibilityService(accountID int64, weeklyStatus, monthlyStatus int) (*GrokQuotaService, *grokBillingEligibilityAccountRepo, *Account) {
	account := &Account{
		ID:          accountID,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(2 * grokTokenRefreshSkew).UTC().Format(time.RFC3339),
		},
	}
	repo := &grokBillingEligibilityAccountRepo{account: account}
	upstream := &grokBillingEligibilityUpstream{weeklyStatus: weeklyStatus, monthlyStatus: monthlyStatus}
	return NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream), repo, account
}

func TestGrokQuotaFailedBillingProbePersistsMediaEligibilitySignal(t *testing.T) {
	svc, repo, account := newGrokBillingEligibilityService(58, http.StatusForbidden, http.StatusForbidden)

	result, err := svc.ProbeBilling(context.Background(), account.ID)

	require.Error(t, err)
	require.Nil(t, result)
	raw, ok := repo.updates[grokBillingExtraKey]
	require.True(t, ok)
	billing, ok := raw.(*xai.BillingSummary)
	require.True(t, ok)
	require.Equal(t, http.StatusForbidden, billing.StatusCode)
	require.Equal(t, http.StatusForbidden, billing.WeeklyStatusCode)
	require.Equal(t, http.StatusForbidden, billing.MonthlyStatusCode)
	require.True(t, billing.Partial)
	require.ElementsMatch(t, []string{"weekly", "monthly"}, billing.FailedWindows)

	account.Extra = map[string]any{grokBillingExtraKey: billing}
	eligible, reason := account.GrokMediaGenerationEligibility()
	require.False(t, eligible)
	require.Equal(t, "billing_forbidden", reason)
}

func TestGrokQuotaPartialBilling403PersistsMediaEligibilitySignal(t *testing.T) {
	svc, repo, account := newGrokBillingEligibilityService(59, http.StatusForbidden, http.StatusOK)

	result, err := svc.ProbeBilling(context.Background(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Billing)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, http.StatusForbidden, result.Billing.WeeklyStatusCode)
	require.Equal(t, http.StatusOK, result.Billing.MonthlyStatusCode)
	require.True(t, result.Billing.Partial)
	require.Contains(t, result.Billing.FailedWindows, "weekly")
	require.Contains(t, repo.updates, grokBillingExtraKey)

	account.Extra = map[string]any{grokBillingExtraKey: result.Billing}
	eligible, reason := account.GrokMediaGenerationEligibility()
	require.False(t, eligible)
	require.Equal(t, "billing_forbidden", reason)
}

func TestPreferBillingObservationStatus_MediaEligibility(t *testing.T) {
	tests := []struct {
		name          string
		weeklyStatus  int
		monthlyStatus int
		want          int
	}{
		{name: "weekly forbidden wins", weeklyStatus: http.StatusForbidden, monthlyStatus: http.StatusBadGateway, want: http.StatusForbidden},
		{name: "monthly forbidden wins", weeklyStatus: http.StatusBadGateway, monthlyStatus: http.StatusForbidden, want: http.StatusForbidden},
		{name: "weekly observation otherwise wins", weeklyStatus: http.StatusTooManyRequests, monthlyStatus: http.StatusBadGateway, want: http.StatusTooManyRequests},
		{name: "monthly observation is fallback", monthlyStatus: http.StatusBadGateway, want: http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, preferBillingObservationStatus(tt.weeklyStatus, tt.monthlyStatus))
		})
	}
}
