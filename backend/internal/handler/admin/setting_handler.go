package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// semverPattern 预编译 semver 格式校验正则
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// menuItemIDPattern validates custom menu item IDs: alphanumeric, hyphens, underscores only.
var menuItemIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// generateMenuItemID generates a short random hex ID for a custom menu item.
func generateMenuItemID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate menu item ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func scopesContainOpenID(scopes string) bool {
	for _, scope := range strings.Fields(strings.ToLower(strings.TrimSpace(scopes))) {
		if scope == "openid" {
			return true
		}
	}
	return false
}

func rechargeCenterItemsToDTO(items []service.RechargeCenterItem) []dto.PaymentRechargeCenterItem {
	result := make([]dto.PaymentRechargeCenterItem, 0, len(items))
	for _, item := range items {
		result = append(result, dto.PaymentRechargeCenterItem{
			Name:        item.Name,
			Description: item.Description,
			URL:         item.URL,
		})
	}
	return result
}

func rechargeCenterItemsFromDTO(items []dto.PaymentRechargeCenterItem) []service.RechargeCenterItem {
	if items == nil {
		return nil
	}
	result := make([]service.RechargeCenterItem, 0, len(items))
	for _, item := range items {
		result = append(result, service.RechargeCenterItem{
			Name:        item.Name,
			Description: item.Description,
			URL:         item.URL,
		})
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// SettingHandler 系统设置处理器
type SettingHandler struct {
	settingService           *service.SettingService
	emailService             *service.EmailService
	turnstileService         *service.TurnstileService
	opsService               *service.OpsService
	paymentConfigService     *service.PaymentConfigService
	paymentService           *service.PaymentService
	userAttributeService     *service.UserAttributeService
	notificationEmailService *service.NotificationEmailService
	totpService              *service.TotpService
	userService              *service.UserService
}

// NewSettingHandler 创建系统设置处理器
func NewSettingHandler(settingService *service.SettingService, emailService *service.EmailService, turnstileService *service.TurnstileService, opsService *service.OpsService, paymentConfigService *service.PaymentConfigService, paymentService *service.PaymentService, userAttributeServices ...*service.UserAttributeService) *SettingHandler {
	h := &SettingHandler{
		settingService:       settingService,
		emailService:         emailService,
		turnstileService:     turnstileService,
		opsService:           opsService,
		paymentConfigService: paymentConfigService,
		paymentService:       paymentService,
	}
	if len(userAttributeServices) > 0 {
		h.userAttributeService = userAttributeServices[0]
	}
	return h
}

func (h *SettingHandler) SetNotificationEmailService(notificationEmailService *service.NotificationEmailService) {
	if h != nil {
		h.notificationEmailService = notificationEmailService
	}
}

// SetStepUpDeps attaches the services backing the step-up switch preconditions
// (enable requires the acting admin to have TOTP enabled; disable is itself a
// step-up gated operation), without changing the constructor signature used by
// existing unit tests.
func (h *SettingHandler) SetStepUpDeps(totpService *service.TotpService, userService *service.UserService) {
	h.totpService = totpService
	h.userService = userService
}

// GetSettings 获取所有系统设置
// GET /api/v1/admin/settings
func (h *SettingHandler) GetSettings(c *gin.Context) {
	settings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	authSourceDefaults, err := h.settingService.GetAuthSourceDefaultSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Check if ops monitoring is enabled (respects config.ops.enabled)
	opsEnabled := h.opsService != nil && h.opsService.IsMonitoringEnabled(c.Request.Context())
	defaultSubscriptions := make([]dto.DefaultSubscriptionSetting, 0, len(settings.DefaultSubscriptions))
	for _, sub := range settings.DefaultSubscriptions {
		defaultSubscriptions = append(defaultSubscriptions, dto.DefaultSubscriptionSetting{
			GroupID:      sub.GroupID,
			ValidityDays: sub.ValidityDays,
		})
	}

	// Load payment config
	var paymentCfg *service.PaymentConfig
	if h.paymentConfigService != nil {
		paymentCfg, _ = h.paymentConfigService.GetPaymentConfig(c.Request.Context())
	}
	if paymentCfg == nil {
		paymentCfg = &service.PaymentConfig{}
	}

	payload := dto.SystemSettings{
		RegistrationEnabled:                                    settings.RegistrationEnabled,
		EmailVerifyEnabled:                                     settings.EmailVerifyEnabled,
		RegistrationEmailSuffixWhitelist:                       settings.RegistrationEmailSuffixWhitelist,
		UpstreamURLAllowlistExtraHosts:                         settings.UpstreamURLAllowlistExtraHosts,
		PromoCodeEnabled:                                       settings.PromoCodeEnabled,
		PasswordResetEnabled:                                   settings.PasswordResetEnabled,
		FrontendURL:                                            settings.FrontendURL,
		InvitationCodeEnabled:                                  settings.InvitationCodeEnabled,
		TotpEnabled:                                            settings.TotpEnabled,
		TotpEncryptionKeyConfigured:                            h.settingService.IsTotpEncryptionKeyConfigured(),
		SessionBindingEnabled:                                  settings.SessionBindingEnabled,
		StepUpEnabled:                                          settings.StepUpEnabled,
		AuditLogRetentionDays:                                  settings.AuditLogRetentionDays,
		LoginAgreementEnabled:                                  settings.LoginAgreementEnabled,
		LoginAgreementMode:                                     settings.LoginAgreementMode,
		LoginAgreementUpdatedAt:                                settings.LoginAgreementUpdatedAt,
		LoginAgreementDocuments:                                loginAgreementDocumentsToDTO(settings.LoginAgreementDocuments),
		SMTPHost:                                               settings.SMTPHost,
		SMTPPort:                                               settings.SMTPPort,
		SMTPUsername:                                           settings.SMTPUsername,
		SMTPPasswordConfigured:                                 settings.SMTPPasswordConfigured,
		SMTPFrom:                                               settings.SMTPFrom,
		SMTPFromName:                                           settings.SMTPFromName,
		SMTPUseTLS:                                             settings.SMTPUseTLS,
		TurnstileEnabled:                                       settings.TurnstileEnabled,
		TurnstileSiteKey:                                       settings.TurnstileSiteKey,
		TurnstileSecretKeyConfigured:                           settings.TurnstileSecretKeyConfigured,
		APIKeyACLTrustForwardedIP:                              settings.APIKeyACLTrustForwardedIP,
		LinuxDoConnectEnabled:                                  settings.LinuxDoConnectEnabled,
		LinuxDoConnectClientID:                                 settings.LinuxDoConnectClientID,
		LinuxDoConnectClientSecretConfigured:                   settings.LinuxDoConnectClientSecretConfigured,
		LinuxDoConnectRedirectURL:                              settings.LinuxDoConnectRedirectURL,
		DingTalkConnectEnabled:                                 settings.DingTalkConnectEnabled,
		DingTalkConnectClientID:                                settings.DingTalkConnectClientID,
		DingTalkConnectClientSecretConfigured:                  settings.DingTalkConnectClientSecretConfigured,
		DingTalkConnectRedirectURL:                             settings.DingTalkConnectRedirectURL,
		DingTalkConnectCorpRestrictionPolicy:                   settings.DingTalkConnectCorpRestrictionPolicy,
		DingTalkConnectInternalCorpID:                          settings.DingTalkConnectInternalCorpID,
		DingTalkConnectBypassRegistration:                      settings.DingTalkConnectBypassRegistration,
		DingTalkConnectSyncCorpEmail:                           settings.DingTalkConnectSyncCorpEmail,
		DingTalkConnectSyncDisplayName:                         settings.DingTalkConnectSyncDisplayName,
		DingTalkConnectSyncDept:                                settings.DingTalkConnectSyncDept,
		DingTalkConnectSyncCorpEmailAttrKey:                    settings.DingTalkConnectSyncCorpEmailAttrKey,
		DingTalkConnectSyncDisplayNameAttrKey:                  settings.DingTalkConnectSyncDisplayNameAttrKey,
		DingTalkConnectSyncDeptAttrKey:                         settings.DingTalkConnectSyncDeptAttrKey,
		DingTalkConnectSyncCorpEmailAttrName:                   settings.DingTalkConnectSyncCorpEmailAttrName,
		DingTalkConnectSyncDisplayNameAttrName:                 settings.DingTalkConnectSyncDisplayNameAttrName,
		DingTalkConnectSyncDeptAttrName:                        settings.DingTalkConnectSyncDeptAttrName,
		WeChatConnectEnabled:                                   settings.WeChatConnectEnabled,
		WeChatConnectAppID:                                     settings.WeChatConnectAppID,
		WeChatConnectAppSecretConfigured:                       settings.WeChatConnectAppSecretConfigured,
		WeChatConnectOpenAppID:                                 settings.WeChatConnectOpenAppID,
		WeChatConnectOpenAppSecretConfigured:                   settings.WeChatConnectOpenAppSecretConfigured,
		WeChatConnectMPAppID:                                   settings.WeChatConnectMPAppID,
		WeChatConnectMPAppSecretConfigured:                     settings.WeChatConnectMPAppSecretConfigured,
		WeChatConnectMobileAppID:                               settings.WeChatConnectMobileAppID,
		WeChatConnectMobileAppSecretConfigured:                 settings.WeChatConnectMobileAppSecretConfigured,
		WeChatConnectOpenEnabled:                               settings.WeChatConnectOpenEnabled,
		WeChatConnectMPEnabled:                                 settings.WeChatConnectMPEnabled,
		WeChatConnectMobileEnabled:                             settings.WeChatConnectMobileEnabled,
		WeChatConnectMode:                                      settings.WeChatConnectMode,
		WeChatConnectScopes:                                    settings.WeChatConnectScopes,
		WeChatConnectRedirectURL:                               settings.WeChatConnectRedirectURL,
		WeChatConnectFrontendRedirectURL:                       settings.WeChatConnectFrontendRedirectURL,
		OIDCConnectEnabled:                                     settings.OIDCConnectEnabled,
		OIDCConnectProviderName:                                settings.OIDCConnectProviderName,
		OIDCConnectClientID:                                    settings.OIDCConnectClientID,
		OIDCConnectClientSecretConfigured:                      settings.OIDCConnectClientSecretConfigured,
		OIDCConnectIssuerURL:                                   settings.OIDCConnectIssuerURL,
		OIDCConnectDiscoveryURL:                                settings.OIDCConnectDiscoveryURL,
		OIDCConnectAuthorizeURL:                                settings.OIDCConnectAuthorizeURL,
		OIDCConnectTokenURL:                                    settings.OIDCConnectTokenURL,
		OIDCConnectUserInfoURL:                                 settings.OIDCConnectUserInfoURL,
		OIDCConnectJWKSURL:                                     settings.OIDCConnectJWKSURL,
		OIDCConnectScopes:                                      settings.OIDCConnectScopes,
		OIDCConnectRedirectURL:                                 settings.OIDCConnectRedirectURL,
		OIDCConnectFrontendRedirectURL:                         settings.OIDCConnectFrontendRedirectURL,
		OIDCConnectTokenAuthMethod:                             settings.OIDCConnectTokenAuthMethod,
		OIDCConnectUsePKCE:                                     settings.OIDCConnectUsePKCE,
		OIDCConnectValidateIDToken:                             settings.OIDCConnectValidateIDToken,
		OIDCConnectAllowedSigningAlgs:                          settings.OIDCConnectAllowedSigningAlgs,
		OIDCConnectClockSkewSeconds:                            settings.OIDCConnectClockSkewSeconds,
		OIDCConnectRequireEmailVerified:                        settings.OIDCConnectRequireEmailVerified,
		OIDCConnectUserInfoEmailPath:                           settings.OIDCConnectUserInfoEmailPath,
		OIDCConnectUserInfoIDPath:                              settings.OIDCConnectUserInfoIDPath,
		OIDCConnectUserInfoUsernamePath:                        settings.OIDCConnectUserInfoUsernamePath,
		GitHubOAuthEnabled:                                     settings.GitHubOAuthEnabled,
		GitHubOAuthClientID:                                    settings.GitHubOAuthClientID,
		GitHubOAuthClientSecretConfigured:                      settings.GitHubOAuthClientSecretConfigured,
		GitHubOAuthRedirectURL:                                 settings.GitHubOAuthRedirectURL,
		GitHubOAuthFrontendRedirectURL:                         settings.GitHubOAuthFrontendRedirectURL,
		GoogleOAuthEnabled:                                     settings.GoogleOAuthEnabled,
		GoogleOAuthClientID:                                    settings.GoogleOAuthClientID,
		GoogleOAuthClientSecretConfigured:                      settings.GoogleOAuthClientSecretConfigured,
		GoogleOAuthRedirectURL:                                 settings.GoogleOAuthRedirectURL,
		GoogleOAuthFrontendRedirectURL:                         settings.GoogleOAuthFrontendRedirectURL,
		SiteName:                                               settings.SiteName,
		SiteLogo:                                               settings.SiteLogo,
		SiteSubtitle:                                           settings.SiteSubtitle,
		APIBaseURL:                                             settings.APIBaseURL,
		ContactInfo:                                            settings.ContactInfo,
		DocURL:                                                 settings.DocURL,
		HomeContent:                                            settings.HomeContent,
		HideCcsImportButton:                                    settings.HideCcsImportButton,
		PurchaseSubscriptionEnabled:                            settings.PurchaseSubscriptionEnabled,
		PurchaseSubscriptionURL:                                settings.PurchaseSubscriptionURL,
		TableDefaultPageSize:                                   settings.TableDefaultPageSize,
		TablePageSizeOptions:                                   settings.TablePageSizeOptions,
		CustomMenuItems:                                        dto.ParseCustomMenuItems(settings.CustomMenuItems),
		CustomEndpoints:                                        dto.ParseCustomEndpoints(settings.CustomEndpoints),
		DefaultConcurrency:                                     settings.DefaultConcurrency,
		DefaultBalance:                                         settings.DefaultBalance,
		RiskControlEnabled:                                     settings.RiskControlEnabled,
		InvoiceManagementEnabled:                               settings.InvoiceManagementEnabled,
		WithdrawalManagementEnabled:                            settings.WithdrawalManagementEnabled,
		CyberSessionBlockEnabled:                               settings.CyberSessionBlockEnabled,
		CyberSessionBlockTTLSeconds:                            settings.CyberSessionBlockTTLSeconds,
		AffiliateRebateRate:                                    settings.AffiliateRebateRate,
		AffiliateRebateFreezeHours:                             settings.AffiliateRebateFreezeHours,
		AffiliateRebateDurationDays:                            settings.AffiliateRebateDurationDays,
		AffiliateRebatePerInviteeCap:                           settings.AffiliateRebatePerInviteeCap,
		AdminRechargeRebateEnabled:                             settings.AdminRechargeRebateEnabled,
		DefaultUserRPMLimit:                                    settings.DefaultUserRPMLimit,
		DefaultAffiliateWeeklyLimit:                            settings.DefaultAffiliateWeeklyLimit,
		DefaultAffiliateCodeAutoRotate:                         settings.DefaultAffiliateCodeAutoRotate,
		UserPrivateGroupDailyLimitUSD:                          settings.UserPrivateGroupDailyLimitUSD,
		UserPrivateGroupWeeklyLimitUSD:                         settings.UserPrivateGroupWeeklyLimitUSD,
		UserPrivateGroupMonthlyLimitUSD:                        settings.UserPrivateGroupMonthlyLimitUSD,
		UserPrivateGroupRateMultiplier:                         settings.UserPrivateGroupRateMultiplier,
		UserPrivateGroupRPMLimit:                               settings.UserPrivateGroupRPMLimit,
		UserPrivateGroupCommissionRate:                         settings.UserPrivateGroupCommissionRate,
		DefaultSubscriptions:                                   defaultSubscriptions,
		EnableModelFallback:                                    settings.EnableModelFallback,
		FallbackModelAnthropic:                                 settings.FallbackModelAnthropic,
		FallbackModelOpenAI:                                    settings.FallbackModelOpenAI,
		FallbackModelGemini:                                    settings.FallbackModelGemini,
		FallbackModelAntigravity:                               settings.FallbackModelAntigravity,
		EnableIdentityPatch:                                    settings.EnableIdentityPatch,
		IdentityPatchPrompt:                                    settings.IdentityPatchPrompt,
		OpsMonitoringEnabled:                                   opsEnabled && settings.OpsMonitoringEnabled,
		OpsRealtimeMonitoringEnabled:                           settings.OpsRealtimeMonitoringEnabled,
		OpsQueryModeDefault:                                    settings.OpsQueryModeDefault,
		OpsMetricsIntervalSeconds:                              settings.OpsMetricsIntervalSeconds,
		MinClaudeCodeVersion:                                   settings.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:                                   settings.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling:                            settings.AllowUngroupedKeyScheduling,
		BackendModeEnabled:                                     settings.BackendModeEnabled,
		EnableFingerprintUnification:                           settings.EnableFingerprintUnification,
		EnableMetadataPassthrough:                              settings.EnableMetadataPassthrough,
		EnableCCHSigning:                                       settings.EnableCCHSigning,
		EnableClaudeOAuthSystemPromptInjection:                 settings.EnableClaudeOAuthSystemPromptInjection,
		ClaudeOAuthSystemPrompt:                                settings.ClaudeOAuthSystemPrompt,
		ClaudeOAuthSystemPromptBlocks:                          settings.ClaudeOAuthSystemPromptBlocks,
		OpenAICleanRelayEnabled:                                settings.OpenAICleanRelayEnabled,
		EnableAnthropicCacheTTL1hInjection:                     settings.EnableAnthropicCacheTTL1hInjection,
		RewriteMessageCacheControl:                             settings.RewriteMessageCacheControl,
		EnableClientDatelineNormalization:                      settings.EnableClientDatelineNormalization,
		AntigravityUserAgentVersion:                            settings.AntigravityUserAgentVersion,
		OpenAICodexUserAgent:                                   settings.OpenAICodexUserAgent,
		MinCodexVersion:                                        settings.MinCodexVersion,
		MaxCodexVersion:                                        settings.MaxCodexVersion,
		CodexCLIOnlyBlacklist:                                  settings.CodexCLIOnlyBlacklist,
		CodexCLIOnlyWhitelist:                                  settings.CodexCLIOnlyWhitelist,
		CodexCLIOnlyAllowAppServerClients:                      settings.CodexCLIOnlyAllowAppServerClients,
		CodexCLIOnlyEngineFingerprintSignals:                   settings.CodexCLIOnlyEngineFingerprintSignals,
		WebSearchEmulationEnabled:                              settings.WebSearchEmulationEnabled,
		PaymentVisibleMethodAlipaySource:                       settings.PaymentVisibleMethodAlipaySource,
		PaymentVisibleMethodWxpaySource:                        settings.PaymentVisibleMethodWxpaySource,
		PaymentVisibleMethodAlipayEnabled:                      settings.PaymentVisibleMethodAlipayEnabled,
		PaymentVisibleMethodWxpayEnabled:                       settings.PaymentVisibleMethodWxpayEnabled,
		OpenAILowUpstreamRatePriorityEnabled:                   settings.OpenAILowUpstreamRatePriorityEnabled,
		OpenAIOAuthSchedulingRateMultiplier:                    settings.OpenAIOAuthSchedulingRateMultiplier,
		OpenAIAdvancedSchedulerEnabled:                         settings.OpenAIAdvancedSchedulerEnabled,
		OpenAIAdvancedSchedulerStickyWeightedEnabled:           settings.OpenAIAdvancedSchedulerStickyWeightedEnabled,
		OpenAIAdvancedSchedulerSubscriptionPriorityEnabled:     settings.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled,
		OpenAIAdvancedSchedulerLBTopK:                          settings.OpenAIAdvancedSchedulerLBTopK,
		OpenAIAdvancedSchedulerWeightPriority:                  settings.OpenAIAdvancedSchedulerWeightPriority,
		OpenAIAdvancedSchedulerWeightLoad:                      settings.OpenAIAdvancedSchedulerWeightLoad,
		OpenAIAdvancedSchedulerWeightQueue:                     settings.OpenAIAdvancedSchedulerWeightQueue,
		OpenAIAdvancedSchedulerWeightErrorRate:                 settings.OpenAIAdvancedSchedulerWeightErrorRate,
		OpenAIAdvancedSchedulerWeightTTFT:                      settings.OpenAIAdvancedSchedulerWeightTTFT,
		OpenAIAdvancedSchedulerWeightReset:                     settings.OpenAIAdvancedSchedulerWeightReset,
		OpenAIAdvancedSchedulerWeightQuotaHeadroom:             settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom,
		OpenAIAdvancedSchedulerWeightUpstreamCost:              settings.OpenAIAdvancedSchedulerWeightUpstreamCost,
		OpenAIAdvancedSchedulerWeightPreviousResponse:          settings.OpenAIAdvancedSchedulerWeightPreviousResponse,
		OpenAIAdvancedSchedulerWeightSessionSticky:             settings.OpenAIAdvancedSchedulerWeightSessionSticky,
		OpenAIAdvancedSchedulerEffectiveLBTopK:                 settings.OpenAIAdvancedSchedulerEffectiveLBTopK,
		OpenAIAdvancedSchedulerEffectiveWeightPriority:         settings.OpenAIAdvancedSchedulerEffectiveWeightPriority,
		OpenAIAdvancedSchedulerEffectiveWeightLoad:             settings.OpenAIAdvancedSchedulerEffectiveWeightLoad,
		OpenAIAdvancedSchedulerEffectiveWeightQueue:            settings.OpenAIAdvancedSchedulerEffectiveWeightQueue,
		OpenAIAdvancedSchedulerEffectiveWeightErrorRate:        settings.OpenAIAdvancedSchedulerEffectiveWeightErrorRate,
		OpenAIAdvancedSchedulerEffectiveWeightTTFT:             settings.OpenAIAdvancedSchedulerEffectiveWeightTTFT,
		OpenAIAdvancedSchedulerEffectiveWeightReset:            settings.OpenAIAdvancedSchedulerEffectiveWeightReset,
		OpenAIAdvancedSchedulerEffectiveWeightQuotaHeadroom:    settings.OpenAIAdvancedSchedulerEffectiveWeightQuotaHeadroom,
		OpenAIAdvancedSchedulerEffectiveWeightUpstreamCost:     settings.OpenAIAdvancedSchedulerEffectiveWeightUpstreamCost,
		OpenAIAdvancedSchedulerEffectiveWeightPreviousResponse: settings.OpenAIAdvancedSchedulerEffectiveWeightPreviousResponse,
		OpenAIAdvancedSchedulerEffectiveWeightSessionSticky:    settings.OpenAIAdvancedSchedulerEffectiveWeightSessionSticky,
		OpenAIFreeAccountRepairEnabled:                         settings.OpenAIFreeAccountRepairEnabled,
		OpenAIFreeAccountRepairWeeklyThresholdUSD:              settings.OpenAIFreeAccountRepairWeeklyThresholdUSD,
		BalanceLowNotifyEnabled:                                settings.BalanceLowNotifyEnabled,
		BalanceLowNotifyThreshold:                              settings.BalanceLowNotifyThreshold,
		BalanceLowNotifyRechargeURL:                            settings.BalanceLowNotifyRechargeURL,
		SubscriptionExpiryNotifyEnabled:                        settings.SubscriptionExpiryNotifyEnabled,
		AccountQuotaNotifyEnabled:                              settings.AccountQuotaNotifyEnabled,
		AccountQuotaNotifyEmails:                               dto.NotifyEmailEntriesFromService(settings.AccountQuotaNotifyEmails),
		PaymentEnabled:                                         paymentCfg.Enabled,
		PaymentMinAmount:                                       paymentCfg.MinAmount,
		PaymentMaxAmount:                                       paymentCfg.MaxAmount,
		PaymentDailyLimit:                                      paymentCfg.DailyLimit,
		PaymentOrderTimeoutMin:                                 paymentCfg.OrderTimeoutMin,
		PaymentMaxPendingOrders:                                paymentCfg.MaxPendingOrders,
		PaymentEnabledTypes:                                    paymentCfg.EnabledTypes,
		PaymentBalanceDisabled:                                 paymentCfg.BalanceDisabled,
		PaymentBalanceRechargeMultiplier:                       paymentCfg.BalanceRechargeMultiplier,
		PaymentSubscriptionUSDToCNYRate:                        paymentCfg.SubscriptionUSDToCNYRate,
		PaymentRechargeFeeRate:                                 paymentCfg.RechargeFeeRate,
		PaymentLoadBalanceStrat:                                paymentCfg.LoadBalanceStrategy,
		PaymentProductNamePrefix:                               paymentCfg.ProductNamePrefix,
		PaymentProductNameSuffix:                               paymentCfg.ProductNameSuffix,
		PaymentAnnouncementText:                                paymentCfg.AnnouncementText,
		PaymentRechargeCenterItems:                             rechargeCenterItemsToDTO(paymentCfg.RechargeCenterItems),
		PaymentRechargeCenterTabEnabled:                        paymentCfg.RechargeCenterTabEnabled,
		PaymentRechargeTabEnabled:                              paymentCfg.RechargeTabEnabled,
		PaymentSubscriptionTabEnabled:                          paymentCfg.SubscriptionTabEnabled,
		PaymentHelpImageURL:                                    paymentCfg.HelpImageURL,
		PaymentHelpText:                                        paymentCfg.HelpText,
		PaymentReceiptCodeOSSEnabled:                           paymentCfg.ReceiptCodeOSS.Enabled,
		PaymentReceiptCodeOSSEndpoint:                          paymentCfg.ReceiptCodeOSS.Endpoint,
		PaymentReceiptCodeOSSRegion:                            paymentCfg.ReceiptCodeOSS.Region,
		PaymentReceiptCodeOSSBucket:                            paymentCfg.ReceiptCodeOSS.Bucket,
		PaymentReceiptCodeOSSAccessKeyID:                       paymentCfg.ReceiptCodeOSS.AccessKeyID,
		PaymentReceiptCodeOSSSecretConfigured:                  paymentCfg.ReceiptCodeOSS.SecretAccessKeyConfigured,
		PaymentReceiptCodeOSSPrefix:                            paymentCfg.ReceiptCodeOSS.Prefix,
		PaymentReceiptCodeOSSPublicBaseURL:                     paymentCfg.ReceiptCodeOSS.PublicBaseURL,
		PaymentReceiptCodeOSSForcePathStyle:                    paymentCfg.ReceiptCodeOSS.ForcePathStyle,
		PaymentReceiptCodeOSSMaxSizeBytes:                      paymentCfg.ReceiptCodeOSS.MaxSizeBytes,
		PaymentReceiptCodeOSSPresignExpireSeconds:              paymentCfg.ReceiptCodeOSS.PresignExpireSeconds,
		PaymentCancelRateLimitEnabled:                          paymentCfg.CancelRateLimitEnabled,
		PaymentCancelRateLimitMax:                              paymentCfg.CancelRateLimitMax,
		PaymentCancelRateLimitWindow:                           paymentCfg.CancelRateLimitWindow,
		PaymentCancelRateLimitUnit:                             paymentCfg.CancelRateLimitUnit,
		PaymentCancelRateLimitMode:                             paymentCfg.CancelRateLimitMode,
		PaymentAlipayForceQRCode:                               paymentCfg.AlipayForceQRCode,

		ChannelMonitorEnabled:                settings.ChannelMonitorEnabled,
		ChannelMonitorDefaultIntervalSeconds: settings.ChannelMonitorDefaultIntervalSeconds,

		AvailableChannelsEnabled: settings.AvailableChannelsEnabled,

		UserAccountImportLimit: settings.UserAccountImportLimit,

		AffiliateEnabled: settings.AffiliateEnabled,

		AllowUserViewErrorRequests: settings.AllowUserViewErrorRequests,
	}

	// OpenAI fast policy (stored under a dedicated setting key)
	if fastPolicy, err := h.settingService.GetOpenAIFastPolicySettings(c.Request.Context()); err != nil {
		slog.Error("openai_fast_policy_settings_get_failed", "error", err)
	} else if fastPolicy != nil {
		payload.OpenAIFastPolicySettings = openaiFastPolicySettingsToDTO(fastPolicy)
	}

	if platformQuotas, err := h.settingService.GetDefaultPlatformQuotas(c.Request.Context()); err != nil {
		slog.Error("default_platform_quotas_get_failed", "error", err)
	} else {
		payload.DefaultPlatformQuotas = platformQuotas
	}

	response.Success(c, systemSettingsResponseData(payload, authSourceDefaults))
}

// openaiFastPolicySettingsToDTO converts service -> dto for OpenAI fast policy.
func openaiFastPolicySettingsToDTO(s *service.OpenAIFastPolicySettings) *dto.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]dto.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = dto.OpenAIFastPolicyRule{
			ServiceTier:          r.ServiceTier,
			Action:               r.Action,
			Scope:                r.Scope,
			UserIDs:              append([]int64(nil), r.UserIDs...),
			ErrorMessage:         r.ErrorMessage,
			ModelWhitelist:       append([]string(nil), r.ModelWhitelist...),
			FallbackAction:       r.FallbackAction,
			FallbackErrorMessage: r.FallbackErrorMessage,
		}
	}
	return &dto.OpenAIFastPolicySettings{Rules: rules}
}

// openaiFastPolicySettingsFromDTO converts dto -> service for OpenAI fast policy.
//
// 规范化 ServiceTier：在 DTO 进入 service 层之前统一把空字符串归一为
// service.OpenAIFastTierAny ("all")，避免管理员保存时空串与 "all" 同时
// 表达"匹配任意 tier"造成数据库取值的二义性。其它非空值原样透传，由
// service.SetOpenAIFastPolicySettings 负责合法值校验。
func openaiFastPolicySettingsFromDTO(s *dto.OpenAIFastPolicySettings) *service.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]service.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = service.OpenAIFastPolicyRule{
			ServiceTier:          r.ServiceTier,
			Action:               r.Action,
			Scope:                r.Scope,
			UserIDs:              append([]int64(nil), r.UserIDs...),
			ErrorMessage:         r.ErrorMessage,
			ModelWhitelist:       append([]string(nil), r.ModelWhitelist...),
			FallbackAction:       r.FallbackAction,
			FallbackErrorMessage: r.FallbackErrorMessage,
		}
		tier := strings.ToLower(strings.TrimSpace(rules[i].ServiceTier))
		if tier == "" {
			tier = service.OpenAIFastTierAny
		}
		rules[i].ServiceTier = tier
	}
	return &service.OpenAIFastPolicySettings{Rules: rules}
}

func loginAgreementDocumentsToDTO(docs []service.LoginAgreementDocument) []dto.LoginAgreementDocument {
	out := make([]dto.LoginAgreementDocument, 0, len(docs))
	for _, doc := range docs {
		out = append(out, dto.LoginAgreementDocument{
			ID:        doc.ID,
			Title:     doc.Title,
			ContentMD: doc.ContentMD,
		})
	}
	return out
}

func loginAgreementDocumentsToService(docs []dto.LoginAgreementDocument) []service.LoginAgreementDocument {
	out := make([]service.LoginAgreementDocument, 0, len(docs))
	for _, doc := range docs {
		out = append(out, service.LoginAgreementDocument{
			ID:        doc.ID,
			Title:     doc.Title,
			ContentMD: doc.ContentMD,
		})
	}
	return out
}

func decodeProvidedJSONFields(rawBody []byte) (map[string]struct{}, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &fields); err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(fields))
	for key := range fields {
		out[key] = struct{}{}
	}
	return out, nil
}

func fieldProvided(fields map[string]struct{}, key string) bool {
	_, ok := fields[key]
	return ok
}

func dtoDefaultSubscriptionsFromService(input []service.DefaultSubscriptionSetting) []dto.DefaultSubscriptionSetting {
	if input == nil {
		return nil
	}
	out := make([]dto.DefaultSubscriptionSetting, 0, len(input))
	for _, item := range input {
		out = append(out, dto.DefaultSubscriptionSetting{
			GroupID:      item.GroupID,
			ValidityDays: item.ValidityDays,
		})
	}
	return out
}

func optionalFloat64Value(input *float64) float64 {
	if input == nil {
		return 0
	}
	return *input
}

func preserveOmittedUpdateSettingsFields(req *UpdateSettingsRequest, previous *service.SystemSettings, fields map[string]struct{}) {
	if req == nil || previous == nil {
		return
	}

	if !fieldProvided(fields, "registration_enabled") {
		req.RegistrationEnabled = previous.RegistrationEnabled
	}
	if !fieldProvided(fields, "email_verify_enabled") {
		req.EmailVerifyEnabled = previous.EmailVerifyEnabled
	}
	if !fieldProvided(fields, "registration_email_suffix_whitelist") {
		req.RegistrationEmailSuffixWhitelist = previous.RegistrationEmailSuffixWhitelist
	}
	if !fieldProvided(fields, "upstream_url_allowlist_extra_hosts") {
		req.UpstreamURLAllowlistExtraHosts = previous.UpstreamURLAllowlistExtraHosts
	}
	if !fieldProvided(fields, "promo_code_enabled") {
		req.PromoCodeEnabled = previous.PromoCodeEnabled
	}
	if !fieldProvided(fields, "password_reset_enabled") {
		req.PasswordResetEnabled = previous.PasswordResetEnabled
	}
	if !fieldProvided(fields, "frontend_url") {
		req.FrontendURL = previous.FrontendURL
	}
	if !fieldProvided(fields, "invitation_code_enabled") {
		req.InvitationCodeEnabled = previous.InvitationCodeEnabled
	}
	if !fieldProvided(fields, "totp_enabled") {
		req.TotpEnabled = previous.TotpEnabled
	}
	if !fieldProvided(fields, "login_agreement_enabled") {
		req.LoginAgreementEnabled = &previous.LoginAgreementEnabled
	}
	if !fieldProvided(fields, "login_agreement_mode") {
		req.LoginAgreementMode = previous.LoginAgreementMode
	}
	if !fieldProvided(fields, "login_agreement_updated_at") {
		req.LoginAgreementUpdatedAt = previous.LoginAgreementUpdatedAt
	}
	if !fieldProvided(fields, "login_agreement_documents") {
		docs := loginAgreementDocumentsToDTO(previous.LoginAgreementDocuments)
		req.LoginAgreementDocuments = docs
	}
	if !fieldProvided(fields, "smtp_host") {
		req.SMTPHost = previous.SMTPHost
	}
	if !fieldProvided(fields, "smtp_port") {
		req.SMTPPort = previous.SMTPPort
	}
	if !fieldProvided(fields, "smtp_username") {
		req.SMTPUsername = previous.SMTPUsername
	}
	if !fieldProvided(fields, "smtp_from_email") {
		req.SMTPFrom = previous.SMTPFrom
	}
	if !fieldProvided(fields, "smtp_from_name") {
		req.SMTPFromName = previous.SMTPFromName
	}
	if !fieldProvided(fields, "smtp_use_tls") {
		req.SMTPUseTLS = previous.SMTPUseTLS
	}
	if !fieldProvided(fields, "turnstile_enabled") {
		req.TurnstileEnabled = previous.TurnstileEnabled
	}
	if !fieldProvided(fields, "turnstile_site_key") {
		req.TurnstileSiteKey = previous.TurnstileSiteKey
	}
	if !fieldProvided(fields, "linuxdo_connect_enabled") {
		req.LinuxDoConnectEnabled = previous.LinuxDoConnectEnabled
	}
	if !fieldProvided(fields, "linuxdo_connect_client_id") {
		req.LinuxDoConnectClientID = previous.LinuxDoConnectClientID
	}
	if !fieldProvided(fields, "linuxdo_connect_redirect_url") {
		req.LinuxDoConnectRedirectURL = previous.LinuxDoConnectRedirectURL
	}
	if !fieldProvided(fields, "wechat_connect_enabled") {
		req.WeChatConnectEnabled = previous.WeChatConnectEnabled
	}
	if !fieldProvided(fields, "wechat_connect_app_id") {
		req.WeChatConnectAppID = previous.WeChatConnectAppID
	}
	if !fieldProvided(fields, "wechat_connect_open_app_id") {
		req.WeChatConnectOpenAppID = previous.WeChatConnectOpenAppID
	}
	if !fieldProvided(fields, "wechat_connect_mp_app_id") {
		req.WeChatConnectMPAppID = previous.WeChatConnectMPAppID
	}
	if !fieldProvided(fields, "wechat_connect_mobile_app_id") {
		req.WeChatConnectMobileAppID = previous.WeChatConnectMobileAppID
	}
	if !fieldProvided(fields, "wechat_connect_open_enabled") {
		req.WeChatConnectOpenEnabled = previous.WeChatConnectOpenEnabled
	}
	if !fieldProvided(fields, "wechat_connect_mp_enabled") {
		req.WeChatConnectMPEnabled = previous.WeChatConnectMPEnabled
	}
	if !fieldProvided(fields, "wechat_connect_mobile_enabled") {
		req.WeChatConnectMobileEnabled = previous.WeChatConnectMobileEnabled
	}
	if !fieldProvided(fields, "wechat_connect_mode") {
		req.WeChatConnectMode = previous.WeChatConnectMode
	}
	if !fieldProvided(fields, "wechat_connect_scopes") {
		req.WeChatConnectScopes = previous.WeChatConnectScopes
	}
	if !fieldProvided(fields, "wechat_connect_redirect_url") {
		req.WeChatConnectRedirectURL = previous.WeChatConnectRedirectURL
	}
	if !fieldProvided(fields, "wechat_connect_frontend_redirect_url") {
		req.WeChatConnectFrontendRedirectURL = previous.WeChatConnectFrontendRedirectURL
	}
	if !fieldProvided(fields, "oidc_connect_enabled") {
		req.OIDCConnectEnabled = previous.OIDCConnectEnabled
	}
	if !fieldProvided(fields, "oidc_connect_provider_name") {
		req.OIDCConnectProviderName = previous.OIDCConnectProviderName
	}
	if !fieldProvided(fields, "oidc_connect_client_id") {
		req.OIDCConnectClientID = previous.OIDCConnectClientID
	}
	if !fieldProvided(fields, "oidc_connect_issuer_url") {
		req.OIDCConnectIssuerURL = previous.OIDCConnectIssuerURL
	}
	if !fieldProvided(fields, "oidc_connect_discovery_url") {
		req.OIDCConnectDiscoveryURL = previous.OIDCConnectDiscoveryURL
	}
	if !fieldProvided(fields, "oidc_connect_authorize_url") {
		req.OIDCConnectAuthorizeURL = previous.OIDCConnectAuthorizeURL
	}
	if !fieldProvided(fields, "oidc_connect_token_url") {
		req.OIDCConnectTokenURL = previous.OIDCConnectTokenURL
	}
	if !fieldProvided(fields, "oidc_connect_userinfo_url") {
		req.OIDCConnectUserInfoURL = previous.OIDCConnectUserInfoURL
	}
	if !fieldProvided(fields, "oidc_connect_jwks_url") {
		req.OIDCConnectJWKSURL = previous.OIDCConnectJWKSURL
	}
	if !fieldProvided(fields, "oidc_connect_scopes") {
		req.OIDCConnectScopes = previous.OIDCConnectScopes
	}
	if !fieldProvided(fields, "oidc_connect_redirect_url") {
		req.OIDCConnectRedirectURL = previous.OIDCConnectRedirectURL
	}
	if !fieldProvided(fields, "oidc_connect_frontend_redirect_url") {
		req.OIDCConnectFrontendRedirectURL = previous.OIDCConnectFrontendRedirectURL
	}
	if !fieldProvided(fields, "oidc_connect_token_auth_method") {
		req.OIDCConnectTokenAuthMethod = previous.OIDCConnectTokenAuthMethod
	}
	if !fieldProvided(fields, "oidc_connect_allowed_signing_algs") {
		req.OIDCConnectAllowedSigningAlgs = previous.OIDCConnectAllowedSigningAlgs
	}
	if !fieldProvided(fields, "oidc_connect_clock_skew_seconds") {
		req.OIDCConnectClockSkewSeconds = previous.OIDCConnectClockSkewSeconds
	}
	if !fieldProvided(fields, "oidc_connect_require_email_verified") {
		req.OIDCConnectRequireEmailVerified = previous.OIDCConnectRequireEmailVerified
	}
	if !fieldProvided(fields, "oidc_connect_userinfo_email_path") {
		req.OIDCConnectUserInfoEmailPath = previous.OIDCConnectUserInfoEmailPath
	}
	if !fieldProvided(fields, "oidc_connect_userinfo_id_path") {
		req.OIDCConnectUserInfoIDPath = previous.OIDCConnectUserInfoIDPath
	}
	if !fieldProvided(fields, "oidc_connect_userinfo_username_path") {
		req.OIDCConnectUserInfoUsernamePath = previous.OIDCConnectUserInfoUsernamePath
	}
	if !fieldProvided(fields, "github_oauth_enabled") {
		req.GitHubOAuthEnabled = &previous.GitHubOAuthEnabled
	}
	if !fieldProvided(fields, "github_oauth_client_id") {
		req.GitHubOAuthClientID = previous.GitHubOAuthClientID
	}
	if !fieldProvided(fields, "github_oauth_redirect_url") {
		req.GitHubOAuthRedirectURL = previous.GitHubOAuthRedirectURL
	}
	if !fieldProvided(fields, "github_oauth_frontend_redirect_url") {
		req.GitHubOAuthFrontendRedirectURL = previous.GitHubOAuthFrontendRedirectURL
	}
	if !fieldProvided(fields, "google_oauth_enabled") {
		req.GoogleOAuthEnabled = &previous.GoogleOAuthEnabled
	}
	if !fieldProvided(fields, "google_oauth_client_id") {
		req.GoogleOAuthClientID = previous.GoogleOAuthClientID
	}
	if !fieldProvided(fields, "google_oauth_redirect_url") {
		req.GoogleOAuthRedirectURL = previous.GoogleOAuthRedirectURL
	}
	if !fieldProvided(fields, "google_oauth_frontend_redirect_url") {
		req.GoogleOAuthFrontendRedirectURL = previous.GoogleOAuthFrontendRedirectURL
	}
	if !fieldProvided(fields, "site_name") {
		req.SiteName = previous.SiteName
	}
	if !fieldProvided(fields, "site_logo") {
		req.SiteLogo = previous.SiteLogo
	}
	if !fieldProvided(fields, "site_subtitle") {
		req.SiteSubtitle = previous.SiteSubtitle
	}
	if !fieldProvided(fields, "api_base_url") {
		req.APIBaseURL = previous.APIBaseURL
	}
	if !fieldProvided(fields, "contact_info") {
		req.ContactInfo = previous.ContactInfo
	}
	if !fieldProvided(fields, "doc_url") {
		req.DocURL = previous.DocURL
	}
	if !fieldProvided(fields, "home_content") {
		req.HomeContent = previous.HomeContent
	}
	if !fieldProvided(fields, "hide_ccs_import_button") {
		req.HideCcsImportButton = previous.HideCcsImportButton
	}
	if !fieldProvided(fields, "table_default_page_size") {
		req.TableDefaultPageSize = previous.TableDefaultPageSize
	}
	if !fieldProvided(fields, "table_page_size_options") {
		req.TablePageSizeOptions = previous.TablePageSizeOptions
	}
	if !fieldProvided(fields, "default_concurrency") {
		req.DefaultConcurrency = previous.DefaultConcurrency
	}
	if !fieldProvided(fields, "default_balance") {
		req.DefaultBalance = previous.DefaultBalance
	}
	if !fieldProvided(fields, "risk_control_enabled") {
		req.RiskControlEnabled = &previous.RiskControlEnabled
	}
	if !fieldProvided(fields, "invoice_management_enabled") {
		req.InvoiceManagementEnabled = &previous.InvoiceManagementEnabled
	}
	if !fieldProvided(fields, "withdrawal_management_enabled") {
		req.WithdrawalManagementEnabled = &previous.WithdrawalManagementEnabled
	}
	if !fieldProvided(fields, "cyber_session_block_enabled") {
		req.CyberSessionBlockEnabled = &previous.CyberSessionBlockEnabled
	}
	if !fieldProvided(fields, "cyber_session_block_ttl_seconds") {
		req.CyberSessionBlockTTLSeconds = &previous.CyberSessionBlockTTLSeconds
	}
	if !fieldProvided(fields, "user_account_import_limit") {
		req.UserAccountImportLimit = &previous.UserAccountImportLimit
	}
	if !fieldProvided(fields, "default_user_rpm_limit") {
		req.DefaultUserRPMLimit = previous.DefaultUserRPMLimit
	}
	if !fieldProvided(fields, "default_affiliate_weekly_limit") {
		req.DefaultAffiliateWeeklyLimit = previous.DefaultAffiliateWeeklyLimit
	}
	if !fieldProvided(fields, "default_affiliate_code_auto_rotate") {
		req.DefaultAffiliateCodeAutoRotate = previous.DefaultAffiliateCodeAutoRotate
	}
	if !fieldProvided(fields, "user_private_group_daily_limit_usd") {
		req.UserPrivateGroupDailyLimitUSD = optionalFloat64Value(previous.UserPrivateGroupDailyLimitUSD)
	}
	if !fieldProvided(fields, "user_private_group_weekly_limit_usd") {
		req.UserPrivateGroupWeeklyLimitUSD = optionalFloat64Value(previous.UserPrivateGroupWeeklyLimitUSD)
	}
	if !fieldProvided(fields, "user_private_group_monthly_limit_usd") {
		req.UserPrivateGroupMonthlyLimitUSD = optionalFloat64Value(previous.UserPrivateGroupMonthlyLimitUSD)
	}
	if !fieldProvided(fields, "user_private_group_rate_multiplier") {
		req.UserPrivateGroupRateMultiplier = previous.UserPrivateGroupRateMultiplier
	}
	if !fieldProvided(fields, "user_private_group_rpm_limit") {
		req.UserPrivateGroupRPMLimit = previous.UserPrivateGroupRPMLimit
	}
	if !fieldProvided(fields, "user_private_group_commission_rate") {
		req.UserPrivateGroupCommissionRate = previous.UserPrivateGroupCommissionRate
	}
	if !fieldProvided(fields, "default_subscriptions") {
		req.DefaultSubscriptions = dtoDefaultSubscriptionsFromService(previous.DefaultSubscriptions)
	}
	if !fieldProvided(fields, "enable_model_fallback") {
		req.EnableModelFallback = previous.EnableModelFallback
	}
	if !fieldProvided(fields, "fallback_model_anthropic") {
		req.FallbackModelAnthropic = previous.FallbackModelAnthropic
	}
	if !fieldProvided(fields, "fallback_model_openai") {
		req.FallbackModelOpenAI = previous.FallbackModelOpenAI
	}
	if !fieldProvided(fields, "fallback_model_gemini") {
		req.FallbackModelGemini = previous.FallbackModelGemini
	}
	if !fieldProvided(fields, "fallback_model_antigravity") {
		req.FallbackModelAntigravity = previous.FallbackModelAntigravity
	}
	if !fieldProvided(fields, "enable_identity_patch") {
		req.EnableIdentityPatch = previous.EnableIdentityPatch
	}
	if !fieldProvided(fields, "identity_patch_prompt") {
		req.IdentityPatchPrompt = previous.IdentityPatchPrompt
	}
	if !fieldProvided(fields, "min_claude_code_version") {
		req.MinClaudeCodeVersion = previous.MinClaudeCodeVersion
	}
	if !fieldProvided(fields, "max_claude_code_version") {
		req.MaxClaudeCodeVersion = previous.MaxClaudeCodeVersion
	}
	if !fieldProvided(fields, "allow_ungrouped_key_scheduling") {
		req.AllowUngroupedKeyScheduling = previous.AllowUngroupedKeyScheduling
	}
	if !fieldProvided(fields, "backend_mode_enabled") {
		req.BackendModeEnabled = previous.BackendModeEnabled
	}
}

func positiveFloat64Ptr(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func systemSettingsResponseData(settings dto.SystemSettings, authSourceDefaults *service.AuthSourceDefaultSettings) map[string]any {
	data := make(map[string]any)
	raw, err := json.Marshal(settings)
	if err == nil {
		_ = json.Unmarshal(raw, &data)
	}
	if authSourceDefaults == nil {
		authSourceDefaults = &service.AuthSourceDefaultSettings{}
	}

	data["auth_source_default_email_balance"] = authSourceDefaults.Email.Balance
	data["auth_source_default_email_concurrency"] = authSourceDefaults.Email.Concurrency
	data["auth_source_default_email_subscriptions"] = authSourceDefaults.Email.Subscriptions
	data["auth_source_default_email_grant_on_signup"] = authSourceDefaults.Email.GrantOnSignup
	data["auth_source_default_email_grant_on_first_bind"] = authSourceDefaults.Email.GrantOnFirstBind
	data["auth_source_default_linuxdo_balance"] = authSourceDefaults.LinuxDo.Balance
	data["auth_source_default_linuxdo_concurrency"] = authSourceDefaults.LinuxDo.Concurrency
	data["auth_source_default_linuxdo_subscriptions"] = authSourceDefaults.LinuxDo.Subscriptions
	data["auth_source_default_linuxdo_grant_on_signup"] = authSourceDefaults.LinuxDo.GrantOnSignup
	data["auth_source_default_linuxdo_grant_on_first_bind"] = authSourceDefaults.LinuxDo.GrantOnFirstBind
	data["auth_source_default_dingtalk_balance"] = authSourceDefaults.DingTalk.Balance
	data["auth_source_default_dingtalk_concurrency"] = authSourceDefaults.DingTalk.Concurrency
	data["auth_source_default_dingtalk_subscriptions"] = authSourceDefaults.DingTalk.Subscriptions
	data["auth_source_default_dingtalk_grant_on_signup"] = authSourceDefaults.DingTalk.GrantOnSignup
	data["auth_source_default_dingtalk_grant_on_first_bind"] = authSourceDefaults.DingTalk.GrantOnFirstBind
	data["auth_source_default_oidc_balance"] = authSourceDefaults.OIDC.Balance
	data["auth_source_default_oidc_concurrency"] = authSourceDefaults.OIDC.Concurrency
	data["auth_source_default_oidc_subscriptions"] = authSourceDefaults.OIDC.Subscriptions
	data["auth_source_default_oidc_grant_on_signup"] = authSourceDefaults.OIDC.GrantOnSignup
	data["auth_source_default_oidc_grant_on_first_bind"] = authSourceDefaults.OIDC.GrantOnFirstBind
	data["auth_source_default_wechat_balance"] = authSourceDefaults.WeChat.Balance
	data["auth_source_default_wechat_concurrency"] = authSourceDefaults.WeChat.Concurrency
	data["auth_source_default_wechat_subscriptions"] = authSourceDefaults.WeChat.Subscriptions
	data["auth_source_default_wechat_grant_on_signup"] = authSourceDefaults.WeChat.GrantOnSignup
	data["auth_source_default_wechat_grant_on_first_bind"] = authSourceDefaults.WeChat.GrantOnFirstBind
	data["auth_source_default_github_balance"] = authSourceDefaults.GitHub.Balance
	data["auth_source_default_github_concurrency"] = authSourceDefaults.GitHub.Concurrency
	data["auth_source_default_github_subscriptions"] = authSourceDefaults.GitHub.Subscriptions
	data["auth_source_default_github_grant_on_signup"] = authSourceDefaults.GitHub.GrantOnSignup
	data["auth_source_default_github_grant_on_first_bind"] = authSourceDefaults.GitHub.GrantOnFirstBind
	data["auth_source_default_google_balance"] = authSourceDefaults.Google.Balance
	data["auth_source_default_google_concurrency"] = authSourceDefaults.Google.Concurrency
	data["auth_source_default_google_subscriptions"] = authSourceDefaults.Google.Subscriptions
	data["auth_source_default_google_grant_on_signup"] = authSourceDefaults.Google.GrantOnSignup
	data["auth_source_default_google_grant_on_first_bind"] = authSourceDefaults.Google.GrantOnFirstBind
	data["auth_source_default_email_platform_quotas"] = authSourceDefaults.Email.PlatformQuotas
	data["auth_source_default_linuxdo_platform_quotas"] = authSourceDefaults.LinuxDo.PlatformQuotas
	data["auth_source_default_oidc_platform_quotas"] = authSourceDefaults.OIDC.PlatformQuotas
	data["auth_source_default_wechat_platform_quotas"] = authSourceDefaults.WeChat.PlatformQuotas
	data["auth_source_default_github_platform_quotas"] = authSourceDefaults.GitHub.PlatformQuotas
	data["auth_source_default_google_platform_quotas"] = authSourceDefaults.Google.PlatformQuotas
	data["auth_source_default_dingtalk_platform_quotas"] = authSourceDefaults.DingTalk.PlatformQuotas
	data["force_email_on_third_party_signup"] = authSourceDefaults.ForceEmailOnThirdPartySignup

	return data
}

func equalOptionalFloat64(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
