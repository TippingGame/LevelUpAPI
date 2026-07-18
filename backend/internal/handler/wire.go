package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
)

// ProvideAdminHandlers creates the AdminHandlers struct
func ProvideAdminHandlers(
	dashboardHandler *admin.DashboardHandler,
	userHandler *admin.UserHandler,
	groupHandler *admin.GroupHandler,
	accountHandler *admin.AccountHandler,
	accountSharePolicyHandler *admin.AccountSharePolicyHandler,
	announcementHandler *admin.AnnouncementHandler,
	adminConversationHandler *admin.ConversationHandler,
	dataManagementHandler *admin.DataManagementHandler,
	backupHandler *admin.BackupHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	geminiOAuthHandler *admin.GeminiOAuthHandler,
	antigravityOAuthHandler *admin.AntigravityOAuthHandler,
	grokOAuthHandler *admin.GrokOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	redeemHandler *admin.RedeemHandler,
	promoHandler *admin.PromoHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	subscriptionHandler *admin.SubscriptionHandler,
	usageHandler *admin.UsageHandler,
	userAttributeHandler *admin.UserAttributeHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	channelHandler *admin.ChannelHandler,
	channelMonitorHandler *admin.ChannelMonitorHandler,
	channelMonitorTemplateHandler *admin.ChannelMonitorRequestTemplateHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	promptAuditHandler *securityaudit.PromptAdminHandler,
	paymentHandler *admin.PaymentHandler,
	revenueHandler *admin.RevenueHandler,
	withdrawalHandler *admin.WithdrawalHandler,
	invoiceHandler *admin.InvoiceHandler,
	shopHandler *admin.ShopHandler,
	affiliateHandler *admin.AffiliateHandler,
	complianceHandler *admin.ComplianceHandler,
	auditLogHandler *admin.AuditLogHandler,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
) *AdminHandlers {
	accountHandler.SetUpstreamBillingProbeService(upstreamBillingProbe)
	return &AdminHandlers{
		Dashboard:              dashboardHandler,
		User:                   userHandler,
		Group:                  groupHandler,
		Account:                accountHandler,
		AccountSharePolicy:     accountSharePolicyHandler,
		Announcement:           announcementHandler,
		Conversation:           adminConversationHandler,
		DataManagement:         dataManagementHandler,
		Backup:                 backupHandler,
		OAuth:                  oauthHandler,
		OpenAIOAuth:            openaiOAuthHandler,
		GeminiOAuth:            geminiOAuthHandler,
		AntigravityOAuth:       antigravityOAuthHandler,
		GrokOAuth:              grokOAuthHandler,
		Proxy:                  proxyHandler,
		Redeem:                 redeemHandler,
		Promo:                  promoHandler,
		Setting:                settingHandler,
		Ops:                    opsHandler,
		System:                 systemHandler,
		Subscription:           subscriptionHandler,
		Usage:                  usageHandler,
		UserAttribute:          userAttributeHandler,
		ErrorPassthrough:       errorPassthroughHandler,
		TLSFingerprintProfile:  tlsFingerprintProfileHandler,
		APIKey:                 apiKeyHandler,
		ScheduledTest:          scheduledTestHandler,
		Channel:                channelHandler,
		ChannelMonitor:         channelMonitorHandler,
		ChannelMonitorTemplate: channelMonitorTemplateHandler,
		ContentModeration:      contentModerationHandler,
		PromptAudit:            promptAuditHandler,
		Payment:                paymentHandler,
		Revenue:                revenueHandler,
		Withdrawal:             withdrawalHandler,
		Invoice:                invoiceHandler,
		Shop:                   shopHandler,
		Affiliate:              affiliateHandler,
		Compliance:             complianceHandler,
		AuditLog:               auditLogHandler,
	}
}

func ProvideGatewayHandler(
	gatewayService *service.GatewayService,
	openAIGatewayService *service.OpenAIGatewayService,
	geminiCompatService *service.GeminiMessagesCompatService,
	antigravityGatewayService *service.AntigravityGatewayService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	userMsgQueueService *service.UserMessageQueueService,
	cfg *config.Config,
	settingService *service.SettingService,
	coordinator *securityaudit.Coordinator,
) *GatewayHandler {
	h := NewGatewayHandler(gatewayService, openAIGatewayService, geminiCompatService, antigravityGatewayService,
		userService, concurrencyService, billingCacheService, usageService, apiKeyService, usageRecordWorkerPool,
		errorPassthroughService, contentModerationService, userMsgQueueService, cfg, settingService)
	h.securityAuditCoordinator = coordinator
	return h
}

func ProvideOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	opsService *service.OpsService,
	grokQuotaService *service.GrokQuotaService,
	cfg *config.Config,
	coordinator *securityaudit.Coordinator,
) *OpenAIGatewayHandler {
	h := NewOpenAIGatewayHandler(gatewayService, concurrencyService, billingCacheService, apiKeyService,
		usageRecordWorkerPool, errorPassthroughService, contentModerationService, opsService, cfg)
	h.securityAuditCoordinator = coordinator
	h.grokMediaEligibilityProber = grokQuotaService
	return h
}

func ProvideBatchImageHandler(
	batchService *service.BatchImagePublicService,
	download *service.BatchImageDownloadService,
	cleanup *service.BatchImageCleanupService,
	openAI *OpenAIGatewayHandler,
) *BatchImageHandler {
	h := NewBatchImageHandler(batchService, download, cleanup)
	h.openAI = openAI
	return h
}

// ProvideSystemHandler creates admin.SystemHandler with UpdateService
func ProvideSystemHandler(updateService *service.UpdateService, lockService *service.SystemOperationLockService) *admin.SystemHandler {
	return admin.NewSystemHandler(updateService, lockService)
}

// ProvideSettingHandler creates SettingHandler with version from BuildInfo
func ProvideSettingHandler(settingService *service.SettingService, buildInfo BuildInfo) *SettingHandler {
	return NewSettingHandler(settingService, buildInfo.Version)
}

func ProvideAdminUserHandler(
	adminService service.AdminService,
	concurrencyService *service.ConcurrencyService,
	userAttributeService *service.UserAttributeService,
	userPlatformQuotaRepo service.UserPlatformQuotaRepository,
	billingCache service.BillingCache,
	totpService *service.TotpService,
	userService *service.UserService,
	settingService *service.SettingService,
) *admin.UserHandler {
	return admin.NewUserHandler(adminService, concurrencyService, userAttributeService, userPlatformQuotaRepo, billingCache, totpService, userService, settingService)
}

func ProvideAuthHandler(
	cfg *config.Config,
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	promoService *service.PromoService,
	redeemService *service.RedeemService,
	totpService *service.TotpService,
	userAttributeService *service.UserAttributeService,
) *AuthHandler {
	return NewAuthHandler(cfg, authService, userService, settingService, promoService, redeemService, totpService, userAttributeService)
}

func ProvideUserHandler(
	userService *service.UserService,
	authService *service.AuthService,
	emailService *service.EmailService,
	emailCache service.EmailCache,
	affiliateService *service.AffiliateService,
	userAttributeService *service.UserAttributeService,
	userPlatformQuotaRepo service.UserPlatformQuotaRepository,
) *UserHandler {
	return NewUserHandler(userService, authService, emailService, emailCache, affiliateService, userAttributeService, userPlatformQuotaRepo)
}

func ProvideAdminSettingHandler(
	settingService *service.SettingService,
	emailService *service.EmailService,
	turnstileService *service.TurnstileService,
	opsService *service.OpsService,
	paymentConfigService *service.PaymentConfigService,
	paymentService *service.PaymentService,
	userAttributeService *service.UserAttributeService,
	notificationEmailService *service.NotificationEmailService,
	totpService *service.TotpService,
	userService *service.UserService,
) *admin.SettingHandler {
	h := admin.NewSettingHandler(settingService, emailService, turnstileService, opsService, paymentConfigService, paymentService, userAttributeService)
	h.SetNotificationEmailService(notificationEmailService)
	h.SetStepUpDeps(totpService, userService)
	return h
}

func ProvideUserAccountHandler(
	accountService *service.AccountService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	rateLimitService *service.RateLimitService,
	settingService *service.SettingService,
	oauthService *service.OAuthService,
	openaiOAuthService *service.OpenAIOAuthService,
	openaiQuotaService *service.OpenAIQuotaService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	userAttributeService *service.UserAttributeService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	sessionLimitCache service.SessionLimitCache,
	rpmCache service.RPMCache,
	accountBatchTaskService *service.AccountBatchTaskService,
	proxyService *service.ProxyService,
	accountSharePolicyService *service.AccountSharePolicyService,
	grokOAuthService *service.GrokOAuthService,
	grokQuotaService *service.GrokQuotaService,
) *UserAccountHandler {
	h := NewUserAccountHandler(
		accountService,
		accountUsageService,
		accountTestService,
		rateLimitService,
		settingService,
		oauthService,
		openaiOAuthService,
		geminiOAuthService,
		antigravityOAuthService,
		accountBatchTaskService,
	)
	h.SetUserAttributeService(userAttributeService)
	h.SetUserService(userService)
	h.SetOpenAIQuotaService(openaiQuotaService)
	h.SetSharedOwnerSupport(proxyService, accountSharePolicyService)
	h.SetGrokSupport(grokOAuthService, grokQuotaService)
	h.SetRuntimeCapacityProviders(concurrencyService, sessionLimitCache, rpmCache)
	return h
}

// ProvideHandlers creates the Handlers struct
func ProvideHandlers(
	authHandler *AuthHandler,
	userHandler *UserHandler,
	apiKeyHandler *APIKeyHandler,
	userAccountHandler *UserAccountHandler,
	usageHandler *UsageHandler,
	redeemHandler *RedeemHandler,
	subscriptionHandler *SubscriptionHandler,
	announcementHandler *AnnouncementHandler,
	conversationHandler *ConversationHandler,
	channelMonitorUserHandler *ChannelMonitorUserHandler,
	adminHandlers *AdminHandlers,
	gatewayHandler *GatewayHandler,
	openaiGatewayHandler *OpenAIGatewayHandler,
	settingHandler *SettingHandler,
	totpHandler *TotpHandler,
	paymentHandler *PaymentHandler,
	paymentWebhookHandler *PaymentWebhookHandler,
	availableChannelHandler *AvailableChannelHandler,
	receiptCodeHandler *ReceiptCodeHandler,
	withdrawalHandler *WithdrawalHandler,
	invoiceHandler *InvoiceHandler,
	shopHandler *ShopHandler,
	asyncImageHandler *AsyncImageHandler,
	batchImageHandler *BatchImageHandler,
	_ *service.IdempotencyCoordinator,
	_ *service.IdempotencyCleanupService,
) *Handlers {
	return &Handlers{
		Auth:             authHandler,
		User:             userHandler,
		APIKey:           apiKeyHandler,
		UserAccount:      userAccountHandler,
		Usage:            usageHandler,
		Redeem:           redeemHandler,
		Subscription:     subscriptionHandler,
		Announcement:     announcementHandler,
		Conversation:     conversationHandler,
		ChannelMonitor:   channelMonitorUserHandler,
		Admin:            adminHandlers,
		Gateway:          gatewayHandler,
		OpenAIGateway:    openaiGatewayHandler,
		Setting:          settingHandler,
		Totp:             totpHandler,
		Payment:          paymentHandler,
		PaymentWebhook:   paymentWebhookHandler,
		AvailableChannel: availableChannelHandler,
		ReceiptCode:      receiptCodeHandler,
		Withdrawal:       withdrawalHandler,
		Invoice:          invoiceHandler,
		Shop:             shopHandler,
		AsyncImage:       asyncImageHandler,
		BatchImage:       batchImageHandler,
	}
}

// ProviderSet is the Wire provider set for all handlers
var ProviderSet = wire.NewSet(
	// Top-level handlers
	ProvideAuthHandler,
	ProvideUserHandler,
	NewAPIKeyHandler,
	ProvideUserAccountHandler,
	NewUsageHandler,
	NewRedeemHandler,
	NewSubscriptionHandler,
	NewAnnouncementHandler,
	NewConversationHandler,
	NewChannelMonitorUserHandler,
	ProvideGatewayHandler,
	ProvideOpenAIGatewayHandler,
	NewTotpHandler,
	ProvideSettingHandler,
	NewPaymentHandler,
	NewPaymentWebhookHandler,
	NewAvailableChannelHandler,
	NewReceiptCodeHandler,
	NewWithdrawalHandler,
	NewInvoiceHandler,
	NewShopHandler,
	NewAsyncImageHandler,
	ProvideBatchImageHandler,

	// Admin handlers
	admin.NewDashboardHandler,
	ProvideAdminUserHandler,
	admin.NewGroupHandler,
	admin.ProvideAccountHandler,
	admin.NewAccountSharePolicyHandler,
	admin.NewAnnouncementHandler,
	admin.NewConversationHandler,
	admin.NewDataManagementHandler,
	admin.NewBackupHandler,
	admin.NewOAuthHandler,
	admin.NewOpenAIOAuthHandler,
	admin.NewGeminiOAuthHandler,
	admin.NewAntigravityOAuthHandler,
	admin.NewGrokOAuthHandler,
	admin.NewProxyHandler,
	admin.NewRedeemHandler,
	admin.NewPromoHandler,
	ProvideAdminSettingHandler,
	admin.NewOpsHandler,
	ProvideSystemHandler,
	admin.NewSubscriptionHandler,
	admin.NewUsageHandler,
	admin.NewUserAttributeHandler,
	admin.NewErrorPassthroughHandler,
	admin.NewTLSFingerprintProfileHandler,
	admin.NewAdminAPIKeyHandler,
	admin.NewScheduledTestHandler,
	admin.NewChannelHandler,
	admin.NewChannelMonitorHandler,
	admin.NewChannelMonitorRequestTemplateHandler,
	admin.NewContentModerationHandler,
	admin.NewPaymentHandler,
	admin.NewRevenueHandler,
	admin.NewWithdrawalHandler,
	admin.NewInvoiceHandler,
	admin.NewShopHandler,
	admin.NewAffiliateHandler,
	admin.NewComplianceHandler,
	admin.NewAuditLogHandler,

	// AdminHandlers and Handlers constructors
	ProvideAdminHandlers,
	ProvideHandlers,
)
