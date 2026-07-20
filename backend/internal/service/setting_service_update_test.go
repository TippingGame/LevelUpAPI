//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type settingUpdateRepoStub struct {
	updates        map[string]string
	setMultipleErr error
}

func (s *settingUpdateRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *settingUpdateRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingUpdateRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingUpdateRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *settingUpdateRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	s.updates = make(map[string]string, len(settings))
	for k, v := range settings {
		s.updates[k] = v
	}
	return s.setMultipleErr
}

func (s *settingUpdateRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingUpdateRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

type settingValueRepoStub struct {
	values map[string]string
}

type settingGetAllRepoStub = settingValueRepoStub

func (s *settingValueRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	if value, ok := s.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (s *settingValueRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *settingValueRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *settingValueRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *settingValueRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *settingValueRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *settingValueRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type defaultSubGroupReaderStub struct {
	byID  map[int64]*Group
	errBy map[int64]error
	calls []int64
}

func TestSettingService_AffiliateAdminRechargeSetting(t *testing.T) {
	t.Run("missing value defaults to disabled", func(t *testing.T) {
		svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{}}, &config.Config{})

		settings, err := svc.GetAllSettings(context.Background())
		require.NoError(t, err)
		require.False(t, settings.AdminRechargeRebateEnabled)
	})

	t.Run("explicit value is parsed", func(t *testing.T) {
		svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{
			SettingKeyAffiliateAdminRechargeEnabled: "true",
		}}, &config.Config{})

		settings, err := svc.GetAllSettings(context.Background())
		require.NoError(t, err)
		require.True(t, settings.AdminRechargeRebateEnabled)
	})

	t.Run("value is persisted", func(t *testing.T) {
		repo := &settingUpdateRepoStub{}
		svc := NewSettingService(repo, &config.Config{})

		err := svc.UpdateSettings(context.Background(), &SystemSettings{
			AdminRechargeRebateEnabled: true,
		})
		require.NoError(t, err)
		require.Equal(t, "true", repo.updates[SettingKeyAffiliateAdminRechargeEnabled])
	})
}

func (s *defaultSubGroupReaderStub) GetByID(ctx context.Context, id int64) (*Group, error) {
	s.calls = append(s.calls, id)
	if err, ok := s.errBy[id]; ok {
		return nil, err
	}
	if g, ok := s.byID[id]; ok {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_ValidGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &defaultSubGroupReaderStub{
		byID: map[int64]*Group{
			11: {ID: 11, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetDefaultSubscriptionGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{11}, groupReader.calls)

	raw, ok := repo.updates[SettingKeyDefaultSubscriptions]
	require.True(t, ok)

	var got []DefaultSubscriptionSetting
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	require.Equal(t, []DefaultSubscriptionSetting{
		{GroupID: 11, ValidityDays: 30},
	}, got)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsNonSubscriptionGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &defaultSubGroupReaderStub{
		byID: map[int64]*Group{
			12: {ID: 12, SubscriptionType: SubscriptionTypeStandard},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetDefaultSubscriptionGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 12, ValidityDays: 7},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_INVALID", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsNotFoundGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &defaultSubGroupReaderStub{
		errBy: map[int64]error{
			13: ErrGroupNotFound,
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetDefaultSubscriptionGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 13, ValidityDays: 7},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_INVALID", infraerrors.Reason(err))
	require.Equal(t, "13", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsDuplicateGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &defaultSubGroupReaderStub{
		byID: map[int64]*Group{
			11: {ID: 11, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetDefaultSubscriptionGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
			{GroupID: 11, ValidityDays: 60},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE", infraerrors.Reason(err))
	require.Equal(t, "11", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsDuplicateGroupWithoutGroupReader(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
			{GroupID: 11, ValidityDays: 60},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE", infraerrors.Reason(err))
	require.Equal(t, "11", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_RegistrationEmailSuffixWhitelist_Normalized(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		RegistrationEmailSuffixWhitelist: []string{"example.com", "@EXAMPLE.com", " @foo.bar "},
	})
	require.NoError(t, err)
	require.Equal(t, `["@example.com","@foo.bar"]`, repo.updates[SettingKeyRegistrationEmailSuffixWhitelist])
}

func TestSettingService_UpdateSettings_RegistrationEmailSuffixWhitelist_Invalid(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		RegistrationEmailSuffixWhitelist: []string{"@invalid_domain"},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_REGISTRATION_EMAIL_SUFFIX_WHITELIST", infraerrors.Reason(err))
}

func TestSettingService_UpdateSettings_UpstreamAllowlistExtraHosts_Normalized(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		UpstreamURLAllowlistExtraHosts: []string{" NAICCC.com ", "*.Example.com", "naiccc.com", "203.0.113.10:8080"},
	})
	require.NoError(t, err)
	require.Equal(t, `["naiccc.com","*.example.com","203.0.113.10:8080"]`, repo.updates[SettingKeyUpstreamURLAllowlistExtraHosts])
}

func TestSettingService_ParseSettings_UpstreamAllowlistExtraHosts(t *testing.T) {
	svc := NewSettingService(&settingValueRepoStub{}, &config.Config{})

	settings := svc.parseSettings(map[string]string{
		SettingKeyUpstreamURLAllowlistExtraHosts: `[" Relay.Example.com ","*.Example.com","203.0.113.10:8080"]`,
	})

	require.Equal(t, []string{"relay.example.com", "*.example.com", "203.0.113.10:8080"}, settings.UpstreamURLAllowlistExtraHosts)
}

func TestSettingService_UpdateSettings_UpstreamAllowlistExtraHosts_Invalid(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		UpstreamURLAllowlistExtraHosts: []string{"https://naiccc.com"},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_UPSTREAM_URL_ALLOWLIST_EXTRA_HOSTS", infraerrors.Reason(err))
}

func TestSettingService_GetUpstreamURLAllowlistHosts_MergesConfigAndDB(t *testing.T) {
	repo := &settingValueRepoStub{
		values: map[string]string{
			SettingKeyUpstreamURLAllowlistExtraHosts: `["naiccc.com","*.naiccc.com"]`,
		},
	}
	svc := NewSettingService(repo, &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				UpstreamHosts: []string{"api.anthropic.com", "naiccc.com"},
			},
		},
	})

	hosts, err := svc.GetUpstreamURLAllowlistHosts(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"api.anthropic.com", "naiccc.com", "*.naiccc.com"}, hosts)
}

func TestParseDefaultSubscriptions_NormalizesValues(t *testing.T) {
	got := parseDefaultSubscriptions(`[{"group_id":11,"validity_days":30},{"group_id":11,"validity_days":60},{"group_id":0,"validity_days":10},{"group_id":12,"validity_days":99999}]`)
	require.Equal(t, []DefaultSubscriptionSetting{
		{GroupID: 11, ValidityDays: 30},
		{GroupID: 11, ValidityDays: 60},
		{GroupID: 12, ValidityDays: MaxValidityDays},
	}, got)
}

func TestSettingService_UpdateSettings_TablePreferences(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		TableDefaultPageSize: 50,
		TablePageSizeOptions: []int{20, 50, 100},
	})
	require.NoError(t, err)
	require.Equal(t, "50", repo.updates[SettingKeyTableDefaultPageSize])
	require.Equal(t, "[20,50,100]", repo.updates[SettingKeyTablePageSizeOptions])

	err = svc.UpdateSettings(context.Background(), &SystemSettings{
		TableDefaultPageSize: 1000,
		TablePageSizeOptions: []int{20, 100},
	})
	require.NoError(t, err)
	require.Equal(t, "1000", repo.updates[SettingKeyTableDefaultPageSize])
	require.Equal(t, "[20,100]", repo.updates[SettingKeyTablePageSizeOptions])
}

func TestSettingService_UpdateSettings_PaymentVisibleMethodsAndAdvancedScheduler(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		PaymentVisibleMethodAlipaySource:                   "alipay",
		PaymentVisibleMethodWxpaySource:                    "easypay",
		PaymentVisibleMethodAlipayEnabled:                  true,
		PaymentVisibleMethodWxpayEnabled:                   false,
		OpenAILowUpstreamRatePriorityEnabled:               true,
		OpenAIOAuthSchedulingRateMultiplier:                0.05,
		OpenAIAdvancedSchedulerEnabled:                     true,
		OpenAIAdvancedSchedulerStickyWeightedEnabled:       true,
		OpenAIAdvancedSchedulerSubscriptionPriorityEnabled: true,
		OpenAIAdvancedSchedulerLBTopK:                      " 3 ",
		OpenAIAdvancedSchedulerWeightPriority:              "2.50",
		OpenAIAdvancedSchedulerWeightLoad:                  "0",
		OpenAIAdvancedSchedulerWeightQueue:                 "0.75",
		OpenAIAdvancedSchedulerWeightErrorRate:             "1.25",
		OpenAIAdvancedSchedulerWeightTTFT:                  "0.5",
		OpenAIAdvancedSchedulerWeightReset:                 "",
		OpenAIAdvancedSchedulerWeightQuotaHeadroom:         "0.2",
		OpenAIAdvancedSchedulerWeightUpstreamCost:          "1.5",
		OpenAIAdvancedSchedulerWeightPreviousResponse:      "8",
		OpenAIAdvancedSchedulerWeightSessionSticky:         "4",
	})
	require.NoError(t, err)
	require.Equal(t, VisibleMethodSourceOfficialAlipay, repo.updates[SettingPaymentVisibleMethodAlipaySource])
	require.Equal(t, VisibleMethodSourceEasyPayWechat, repo.updates[SettingPaymentVisibleMethodWxpaySource])
	require.Equal(t, "true", repo.updates[SettingPaymentVisibleMethodAlipayEnabled])
	require.Equal(t, "false", repo.updates[SettingPaymentVisibleMethodWxpayEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyOpenAILowUpstreamRatePriorityEnabled])
	require.Equal(t, "0.05", repo.updates[SettingKeyOpenAIOAuthSchedulingRateMultiplier])
	require.Equal(t, "true", repo.updates[openAIAdvancedSchedulerSettingKey])
	require.Equal(t, "true", repo.updates[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled])
	require.Equal(t, "3", repo.updates[SettingKeyOpenAIAdvancedSchedulerLBTopK])
	require.Equal(t, "2.5", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightPriority])
	require.Equal(t, "0", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightLoad])
	require.Equal(t, "0.75", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightQueue])
	require.Equal(t, "1.25", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightErrorRate])
	require.Equal(t, "0.5", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightTTFT])
	require.Equal(t, "", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightReset])
	require.Equal(t, "0.2", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom])
	require.Equal(t, "1.5", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost])
	require.Equal(t, "8", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse])
	require.Equal(t, "4", repo.updates[SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky])
}

func TestSettingService_UpdateSettingsRejectsInvalidOpenAIOAuthSchedulingRateMultiplier(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	for _, rate := range []float64{-0.01, math.NaN(), math.Inf(1)} {
		err := svc.UpdateSettings(context.Background(), &SystemSettings{OpenAIOAuthSchedulingRateMultiplier: rate})
		require.Error(t, err)
	}
}

func TestSettingService_UpdateSettings_OpenAIAdvancedSchedulerWeightSums(t *testing.T) {
	maxFloat := strconv.FormatFloat(math.MaxFloat64, 'g', -1, 64)
	tests := []struct {
		name    string
		weights SystemSettings
		wantErr bool
	}{
		{
			name: "reset only base is valid",
			weights: SystemSettings{
				OpenAIAdvancedSchedulerWeightPriority:         "0",
				OpenAIAdvancedSchedulerWeightLoad:             "0",
				OpenAIAdvancedSchedulerWeightQueue:            "0",
				OpenAIAdvancedSchedulerWeightErrorRate:        "0",
				OpenAIAdvancedSchedulerWeightTTFT:             "0",
				OpenAIAdvancedSchedulerWeightReset:            "1",
				OpenAIAdvancedSchedulerWeightQuotaHeadroom:    "0",
				OpenAIAdvancedSchedulerWeightUpstreamCost:     "0",
				OpenAIAdvancedSchedulerWeightPreviousResponse: "0",
				OpenAIAdvancedSchedulerWeightSessionSticky:    "0",
			},
		},
		{
			name: "base sum overflow is rejected",
			weights: SystemSettings{
				OpenAIAdvancedSchedulerWeightPriority: maxFloat,
				OpenAIAdvancedSchedulerWeightLoad:     maxFloat,
			},
			wantErr: true,
		},
		{
			name: "sticky total sum overflow is rejected",
			weights: SystemSettings{
				OpenAIAdvancedSchedulerWeightPriority:         maxFloat,
				OpenAIAdvancedSchedulerWeightPreviousResponse: maxFloat,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewSettingService(&settingUpdateRepoStub{}, &config.Config{})
			err := svc.UpdateSettings(context.Background(), &tt.weights)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSettingService_ParseSettingsDefaultsOpenAIOAuthSchedulingRateMultiplier(t *testing.T) {
	svc := NewSettingService(&settingUpdateRepoStub{}, &config.Config{})

	require.Equal(t, 1.0, svc.parseSettings(map[string]string{}).OpenAIOAuthSchedulingRateMultiplier)
	require.Equal(t, 0.05, svc.parseSettings(map[string]string{SettingKeyOpenAIOAuthSchedulingRateMultiplier: "0.05"}).OpenAIOAuthSchedulingRateMultiplier)
}

func TestSettingService_UpdateSettings_RejectsInvalidPaymentVisibleMethodSource(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		PaymentVisibleMethodAlipaySource: "not-a-provider",
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
}
