package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

var (
	ErrRegistrationDisabled   = infraerrors.Forbidden("REGISTRATION_DISABLED", "registration is currently disabled")
	ErrSettingNotFound        = infraerrors.NotFound("SETTING_NOT_FOUND", "setting not found")
	ErrDefaultSubGroupInvalid = infraerrors.BadRequest(
		"DEFAULT_SUBSCRIPTION_GROUP_INVALID",
		"default subscription group must exist and be subscription type",
	)
	ErrDefaultSubGroupDuplicate = infraerrors.BadRequest(
		"DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE",
		"default subscription group cannot be duplicated",
	)
)

type SettingRepository interface {
	Get(ctx context.Context, key string) (*Setting, error)
	GetValue(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	GetMultiple(ctx context.Context, keys []string) (map[string]string, error)
	SetMultiple(ctx context.Context, settings map[string]string) error
	GetAll(ctx context.Context) (map[string]string, error)
	Delete(ctx context.Context, key string) error
}

// *cachedVersionBounds

// *cachedBackendMode
// *cachedGatewayForwardingSettings
var cyberSessionBlockRuntimeCache atomic.Value // *cachedCyberSessionBlockRuntime
var cyberSessionBlockRuntimeSF singleflight.Group

// DefaultSubscriptionGroupReader validates group references used by default subscriptions.
type DefaultSubscriptionGroupReader interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
}

// WebSearchManagerBuilder creates a websearch.Manager from config (injected by infra layer).
// proxyURLs maps proxy ID to resolved URL for provider-level proxy support.
type WebSearchManagerBuilder func(cfg *WebSearchEmulationConfig, proxyURLs map[int64]string)

// SettingService 系统设置服务
type SettingService struct {
	settingRepo                 SettingRepository
	defaultSubGroupReader       DefaultSubscriptionGroupReader
	proxyRepo                   ProxyRepository // for resolving websearch provider proxy URLs
	cfg                         *config.Config
	onUpdate                    func() // Callback when settings are updated (for cache invalidation)
	version                     string // Application version
	webSearchManagerBuilder     WebSearchManagerBuilder
	antigravityUAVersionCache   atomic.Value
	antigravityUAVersionSF      singleflight.Group
	openAICodexUACache          atomic.Value
	openAICodexUASF             singleflight.Group
	codexRestrictionPolicyCache atomic.Value
	codexRestrictionPolicySF    singleflight.Group

	cyberSessionBlockRuntimeCache atomic.Value
	cyberSessionBlockRuntimeSF    singleflight.Group

	openAIQuotaAutoPauseSettingsCache atomic.Value
	openAIQuotaAutoPauseSettingsSF    singleflight.Group

	securitySwitchesCache atomic.Value
	securitySwitchesMu    sync.Mutex

	adminComplianceCache sync.Map
	adminComplianceSF    singleflight.Group
}

type DefaultPlatformQuotaSetting struct {
	DailyLimitUSD   *float64 `json:"daily"`
	WeeklyLimitUSD  *float64 `json:"weekly"`
	MonthlyLimitUSD *float64 `json:"monthly"`
}

type ProviderDefaultGrantSettings struct {
	Balance          float64
	Concurrency      int
	Subscriptions    []DefaultSubscriptionSetting
	GrantOnSignup    bool
	GrantOnFirstBind bool
	PlatformQuotas   map[string]*DefaultPlatformQuotaSetting
}

type AuthSourceDefaultSettings struct {
	Email                        ProviderDefaultGrantSettings
	LinuxDo                      ProviderDefaultGrantSettings
	OIDC                         ProviderDefaultGrantSettings
	WeChat                       ProviderDefaultGrantSettings
	GitHub                       ProviderDefaultGrantSettings
	Google                       ProviderDefaultGrantSettings
	DingTalk                     ProviderDefaultGrantSettings
	ForceEmailOnThirdPartySignup bool
}

type authSourceDefaultKeySet struct {
	source           string
	balance          string
	concurrency      string
	subscriptions    string
	grantOnSignup    string
	grantOnFirstBind string
	platformQuotas   string
}

var (
	emailAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "email",
		balance:          SettingKeyAuthSourceDefaultEmailBalance,
		concurrency:      SettingKeyAuthSourceDefaultEmailConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultEmailSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultEmailGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultEmailGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("email"),
	}
	linuxDoAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "linuxdo",
		balance:          SettingKeyAuthSourceDefaultLinuxDoBalance,
		concurrency:      SettingKeyAuthSourceDefaultLinuxDoConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultLinuxDoSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultLinuxDoGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultLinuxDoGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("linuxdo"),
	}
	oidcAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "oidc",
		balance:          SettingKeyAuthSourceDefaultOIDCBalance,
		concurrency:      SettingKeyAuthSourceDefaultOIDCConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultOIDCSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultOIDCGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultOIDCGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("oidc"),
	}
	weChatAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "wechat",
		balance:          SettingKeyAuthSourceDefaultWeChatBalance,
		concurrency:      SettingKeyAuthSourceDefaultWeChatConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultWeChatSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultWeChatGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultWeChatGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("wechat"),
	}
	gitHubAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "github",
		balance:          SettingKeyAuthSourceDefaultGitHubBalance,
		concurrency:      SettingKeyAuthSourceDefaultGitHubConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultGitHubSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultGitHubGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultGitHubGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("github"),
	}
	googleAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "google",
		balance:          SettingKeyAuthSourceDefaultGoogleBalance,
		concurrency:      SettingKeyAuthSourceDefaultGoogleConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultGoogleSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultGoogleGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultGoogleGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("google"),
	}
	dingTalkAuthSourceDefaultKeys = authSourceDefaultKeySet{
		source:           "dingtalk",
		balance:          SettingKeyAuthSourceDefaultDingTalkBalance,
		concurrency:      SettingKeyAuthSourceDefaultDingTalkConcurrency,
		subscriptions:    SettingKeyAuthSourceDefaultDingTalkSubscriptions,
		grantOnSignup:    SettingKeyAuthSourceDefaultDingTalkGrantOnSignup,
		grantOnFirstBind: SettingKeyAuthSourceDefaultDingTalkGrantOnFirstBind,
		platformQuotas:   SettingKeyAuthSourcePlatformQuotas("dingtalk"),
	}
)

const (
	defaultAuthSourceBalance     = 0
	defaultAuthSourceConcurrency = 5
	defaultWeChatConnectMode     = "open"
	defaultWeChatConnectScopes   = "snsapi_login"
	defaultWeChatConnectFrontend = "/auth/wechat/callback"
	defaultGitHubOAuthAuthorize  = "https://github.com/login/oauth/authorize"
	defaultGitHubOAuthToken      = "https://github.com/login/oauth/access_token"
	defaultGitHubOAuthUserInfo   = "https://api.github.com/user"
	defaultGitHubOAuthEmails     = "https://api.github.com/user/emails"
	defaultGitHubOAuthScopes     = "read:user user:email"
	defaultGitHubOAuthFrontend   = "/auth/oauth/callback"
	defaultGoogleOAuthAuthorize  = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultGoogleOAuthToken      = "https://oauth2.googleapis.com/token"
	defaultGoogleOAuthUserInfo   = "https://openidconnect.googleapis.com/v1/userinfo"
	defaultGoogleOAuthScopes     = "openid email profile"
	defaultGoogleOAuthFrontend   = "/auth/oauth/callback"
	defaultLoginAgreementMode    = "modal"
	defaultLoginAgreementDate    = "2026-03-31"
)

// NewSettingService 创建系统设置服务实例
func NewSettingService(settingRepo SettingRepository, cfg *config.Config) *SettingService {
	return &SettingService{
		settingRepo: settingRepo,
		cfg:         cfg,
	}
}

// SetDefaultSubscriptionGroupReader injects an optional group reader for default subscription validation.
func (s *SettingService) SetDefaultSubscriptionGroupReader(reader DefaultSubscriptionGroupReader) {
	s.defaultSubGroupReader = reader
}

// SetProxyRepository injects a proxy repo for resolving websearch provider proxy URLs.
func (s *SettingService) SetProxyRepository(repo ProxyRepository) {
	s.proxyRepo = repo
}

// GetAllSettings 获取所有系统设置
func (s *SettingService) GetAllSettings(ctx context.Context) (*SystemSettings, error) {
	settings, err := s.settingRepo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}

	return s.parseSettings(settings), nil
}

// GetUpstreamURLAllowlistHosts returns config-defined upstream hosts plus DB-managed additions.
// If the allowlist switch is disabled in config, callers should use format-only URL validation.
func (s *SettingService) GetUpstreamURLAllowlistHosts(ctx context.Context) ([]string, error) {
	if s == nil || s.cfg == nil {
		return nil, nil
	}
	hosts := mergeStringSlices(nil, s.cfg.Security.URLAllowlist.UpstreamHosts)
	if s.settingRepo == nil {
		return hosts, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyUpstreamURLAllowlistExtraHosts)
	if errors.Is(err, ErrSettingNotFound) {
		return hosts, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get upstream url allowlist extra hosts: %w", err)
	}
	extra := ParseUpstreamURLAllowlistExtraHosts(raw)
	return mergeStringSlices(hosts, extra), nil
}

func parseUserAccountImportLimit(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return DefaultUserAccountCredentialImportLimit
	}
	return NormalizeUserAccountCredentialImportLimit(v)
}

func (s *SettingService) GetUserAccountImportLimit(ctx context.Context) (int, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyUserAccountImportLimit)
	if errors.Is(err, ErrSettingNotFound) {
		return DefaultUserAccountCredentialImportLimit, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get user account import limit: %w", err)
	}
	return parseUserAccountImportLimit(raw), nil
}

// SetOnUpdateCallback sets a callback function to be called when settings are updated
// This is used for cache invalidation (e.g., HTML cache in frontend server)
func (s *SettingService) SetOnUpdateCallback(callback func()) {
	s.onUpdate = callback
}

// SetVersion sets the application version for injection into public settings
func (s *SettingService) SetVersion(version string) {
	s.version = version
}

func ParseUpstreamURLAllowlistExtraHosts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var hosts []string
	if err := json.Unmarshal([]byte(raw), &hosts); err != nil {
		return []string{}
	}
	normalized, err := NormalizeUpstreamURLAllowlistExtraHosts(hosts)
	if err != nil {
		return []string{}
	}
	return normalized
}

func NormalizeUpstreamURLAllowlistExtraHosts(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeUpstreamURLAllowlistHost(value)
		if err != nil {
			return nil, err
		}
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeUpstreamURLAllowlistHost(value string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, "/\\?#@ \t\r\n") {
		return "", fmt.Errorf("invalid upstream allowlist host: %s", value)
	}
	if strings.HasPrefix(raw, "*.") {
		suffix := strings.TrimPrefix(raw, "*.")
		if suffix == "" || strings.ContainsAny(suffix, "*:") {
			return "", fmt.Errorf("invalid upstream allowlist wildcard host: %s", value)
		}
		raw = "*." + suffix
	} else if strings.Contains(raw, "*") {
		return "", fmt.Errorf("invalid upstream allowlist wildcard host: %s", value)
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String(), nil
	}
	if strings.Contains(raw, ":") {
		host, port, err := net.SplitHostPort(raw)
		if err != nil {
			return "", fmt.Errorf("invalid upstream allowlist host: %s", value)
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil {
			return "", fmt.Errorf("invalid upstream allowlist host: %s", value)
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber <= 0 || portNumber > 65535 {
			return "", fmt.Errorf("invalid upstream allowlist host: %s", value)
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(portNumber)), nil
	}
	if strings.HasPrefix(raw, ".") || strings.HasSuffix(raw, ".") || strings.Contains(raw, "..") {
		return "", fmt.Errorf("invalid upstream allowlist host: %s", value)
	}
	return raw, nil
}

func mergeStringSlices(base []string, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	for _, value := range append(base, extra...) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, trimmed)
	}
	return merged
}

// IsOpenAICleanRelayEnabled 检查是否启用 OpenAI 洁净中继模式。
func (s *SettingService) IsOpenAICleanRelayEnabled(ctx context.Context) bool {
	return s.getGatewayForwardingSettingsCached(ctx).cleanRelay
}

// IsInvoiceManagementEnabled checks whether invoice management is enabled.
func (s *SettingService) IsInvoiceManagementEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyInvoiceManagementEnabled)
	if err != nil {
		return false
	}
	return value == "true"
}

// IsWithdrawalManagementEnabled checks whether withdrawal management is enabled.
func (s *SettingService) IsWithdrawalManagementEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyWithdrawalManagementEnabled)
	if err != nil {
		return true
	}
	return !isFalseSettingValue(value)
}

// GetDefaultAffiliateInviteCodePolicy returns the invite-code policy used when
// a user affiliate profile is created for the first time.
func (s *SettingService) GetDefaultAffiliateInviteCodePolicy(ctx context.Context) (int, bool) {
	if s == nil || s.settingRepo == nil {
		return AffiliateCodeWeeklyLimitDefault, AffiliateCodeAutoRotateDefault
	}
	weeklyLimit := AffiliateCodeWeeklyLimitDefault
	if value, err := s.settingRepo.GetValue(ctx, SettingKeyDefaultAffiliateWeeklyLimit); err == nil {
		if v, parseErr := strconv.Atoi(value); parseErr == nil && v >= 0 {
			if v > AffiliateCodeWeeklyLimitMax {
				v = AffiliateCodeWeeklyLimitMax
			}
			weeklyLimit = v
		}
	}
	autoRotate := AffiliateCodeAutoRotateDefault
	if value, err := s.settingRepo.GetValue(ctx, SettingKeyDefaultAffiliateCodeAutoRotate); err == nil {
		autoRotate = strings.EqualFold(strings.TrimSpace(value), "true")
	}
	return weeklyLimit, autoRotate
}

// GetUserPrivateGroupTemplate returns the default quota template for newly provisioned user-private groups.
func (s *SettingService) GetUserPrivateGroupTemplate(ctx context.Context) (*UserPrivateGroupTemplate, error) {
	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &UserPrivateGroupTemplate{
		DailyLimitUSD:   settings.UserPrivateGroupDailyLimitUSD,
		WeeklyLimitUSD:  settings.UserPrivateGroupWeeklyLimitUSD,
		MonthlyLimitUSD: settings.UserPrivateGroupMonthlyLimitUSD,
		RateMultiplier:  settings.UserPrivateGroupRateMultiplier,
		RPMLimit:        settings.UserPrivateGroupRPMLimit,
		CommissionRate:  settings.UserPrivateGroupCommissionRate,
	}, nil
}

func (s *SettingService) GetOpenAIFreeAccountRepairSettings(ctx context.Context) (enabled bool, weeklyThresholdUSD float64) {
	if s == nil || s.settingRepo == nil {
		return false, 0
	}
	rawEnabled, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIFreeAccountRepairEnabled)
	if err != nil || !strings.EqualFold(strings.TrimSpace(rawEnabled), "true") {
		return false, 0
	}

	threshold := 60.0
	rawThreshold, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIFreeAccountRepairWeeklyThresholdUSD)
	if err == nil && strings.TrimSpace(rawThreshold) != "" {
		parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(rawThreshold), 64)
		if parseErr != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return false, 0
		}
		threshold = parsed
	} else if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return false, 0
	}

	return true, threshold
}

func formatPositiveOptionalFloat(value *float64) string {
	if value == nil || *value <= 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return "0"
	}
	return strconv.FormatFloat(*value, 'f', 8, 64)
}

func parsePositiveOptionalFloat(value string) *float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

// getStringOrDefault 获取字符串值或默认值
func (s *SettingService) getStringOrDefault(settings map[string]string, key, defaultValue string) string {
	if value, ok := settings[key]; ok && value != "" {
		return value
	}
	return defaultValue
}
