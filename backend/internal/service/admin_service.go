package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// AdminService interface defines admin management operations
type AdminService interface {
	// User management
	ListUsers(ctx context.Context, page, pageSize int, filters UserListFilters, sortBy, sortOrder string) ([]User, int64, error)
	GetUser(ctx context.Context, id int64) (*User, error)
	GetUserIncludeDeleted(ctx context.Context, id int64) (*User, error)
	CreateUser(ctx context.Context, input *CreateUserInput) (*User, error)
	UpdateUser(ctx context.Context, id int64, input *UpdateUserInput) (*User, error)
	DeleteUser(ctx context.Context, id int64) error
	BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error)
	UpdateUserBalance(ctx context.Context, userID int64, balance float64, operation string, notes string) (*User, error)
	UpdateUserPoints(ctx context.Context, userID int64, points float64, operation string, notes string, operatorUserID int64) (*User, error)
	UpdateUserLoadFactorCredits(ctx context.Context, userID int64, amount int, operation string, notes string, operatorUserID int64) (*User, error)
	GetUserAPIKeys(ctx context.Context, userID int64, page, pageSize int, sortBy, sortOrder string) ([]APIKey, int64, error)
	GetUserUsageStats(ctx context.Context, userID int64, period string) (any, error)
	GetUserRPMStatus(ctx context.Context, userID int64) (*UserRPMStatus, error)
	// GetUserBalanceHistory returns paginated balance/concurrency change records for a user.
	// codeType is optional - pass empty string to return all types.
	// Also returns totalRecharged (sum of all positive balance top-ups).
	GetUserBalanceHistory(ctx context.Context, userID int64, page, pageSize int, codeType string) ([]RedeemCode, int64, float64, error)
	BindUserAuthIdentity(ctx context.Context, userID int64, input AdminBindAuthIdentityInput) (*AdminBoundAuthIdentity, error)

	// Group management
	ListGroups(ctx context.Context, page, pageSize int, platform, status, search string, isExclusive *bool, scope, sortBy, sortOrder string) ([]Group, int64, error)
	GetAllGroups(ctx context.Context, scope string) ([]Group, error)
	GetAllGroupsByPlatform(ctx context.Context, platform, scope string) ([]Group, error)
	GetGroup(ctx context.Context, id int64) (*Group, error)
	GetGroupModelsListCandidates(ctx context.Context, id int64, platform string) ([]string, error)
	CreateGroup(ctx context.Context, input *CreateGroupInput) (*Group, error)
	// DuplicateGroup creates an inactive independent copy of a group's configuration
	// and account bindings while preserving each binding's priority.
	DuplicateGroup(ctx context.Context, id int64, actorScope, operationKey string) (*Group, error)
	// RecoverDuplicateGroup returns a previously committed copy for an ambiguous retry.
	// It never creates a group.
	RecoverDuplicateGroup(ctx context.Context, id int64, actorScope, operationKey string) (*Group, error)
	UpdateGroup(ctx context.Context, id int64, input *UpdateGroupInput) (*Group, error)
	DeleteGroup(ctx context.Context, id int64) error
	GetGroupAPIKeys(ctx context.Context, groupID int64, page, pageSize int) ([]APIKey, int64, error)
	GetGroupRateMultipliers(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error)
	ClearGroupRateMultipliers(ctx context.Context, groupID int64) error
	BatchSetGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error
	ClearGroupRPMOverrides(ctx context.Context, groupID int64) error
	BatchSetGroupRPMOverrides(ctx context.Context, groupID int64, entries []GroupRPMOverrideInput) error
	UpdateGroupSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error

	// API Key management (admin)
	AdminUpdateAPIKeyGroupID(ctx context.Context, keyID int64, groupID *int64) (*AdminUpdateAPIKeyGroupIDResult, error)
	AdminUpdateAPIKeyGroupRoutes(ctx context.Context, keyID int64, groupID *int64, routes []APIKeyGroupRoute) (*AdminUpdateAPIKeyGroupIDResult, error)
	AdminResetAPIKeyRateLimitUsage(ctx context.Context, keyID int64) (*APIKey, error)

	// ReplaceUserGroup 替换用户的专属分组：授予新分组权限、迁移 Key、移除旧分组权限
	ReplaceUserGroup(ctx context.Context, userID, oldGroupID, newGroupID int64) (*ReplaceUserGroupResult, error)

	// Account management
	ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string, sortBy, sortOrder string) ([]Account, int64, error)
	ListAccountsForSchedulerScoreFilter(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, error)
	ListOpenAISchedulableAccountsForSchedulerScore(ctx context.Context, groupID *int64) ([]Account, error)
	GetAccount(ctx context.Context, id int64) (*Account, error)
	GetAccountsByIDs(ctx context.Context, ids []int64) ([]*Account, error)
	CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error)
	// DuplicateAccount creates an independent account from an existing account's configuration.
	// First-class runtime columns are intentionally reset by the normal account creation path.
	DuplicateAccount(ctx context.Context, id int64, actorScope, operationKey string) (*Account, error)
	// RecoverDuplicateAccount returns a previously committed duplicate for an ambiguous retry.
	// It never creates an account.
	RecoverDuplicateAccount(ctx context.Context, id int64, actorScope, operationKey string) (*Account, error)
	UpdateAccount(ctx context.Context, id int64, input *UpdateAccountInput) (*Account, error)
	UpdateAccountExtra(ctx context.Context, id int64, updates map[string]any) error
	DeleteAccount(ctx context.Context, id int64) error
	RefreshAccountCredentials(ctx context.Context, id int64) (*Account, error)
	ClearAccountError(ctx context.Context, id int64) (*Account, error)
	SetAccountError(ctx context.Context, id int64, errorMsg string) error
	// EnsureOpenAIPrivacy 检查 OpenAI OAuth 账号 privacy_mode，未设置则尝试关闭训练数据共享并持久化。
	EnsureOpenAIPrivacy(ctx context.Context, account *Account) string
	// EnsureAntigravityPrivacy 检查 Antigravity OAuth 账号 privacy_mode，未设置则调用 setUserSettings 并持久化。
	EnsureAntigravityPrivacy(ctx context.Context, account *Account) string
	// ForceOpenAIPrivacy 强制重新设置 OpenAI OAuth 账号隐私，无论当前状态。
	ForceOpenAIPrivacy(ctx context.Context, account *Account) string
	// ForceAntigravityPrivacy 强制重新设置 Antigravity OAuth 账号隐私，无论当前状态。
	ForceAntigravityPrivacy(ctx context.Context, account *Account) string
	SetAccountSchedulable(ctx context.Context, id int64, schedulable bool) (*Account, error)
	BulkUpdateAccounts(ctx context.Context, input *BulkUpdateAccountsInput) (*BulkUpdateAccountsResult, error)
	CheckMixedChannelRisk(ctx context.Context, currentAccountID int64, currentAccountPlatform string, groupIDs []int64) error
	GetAccountQuotaDashboard(ctx context.Context) (*AccountQuotaDashboard, error)
	// CreateShadow creates a linked spark-dimension account for an OpenAI OAuth parent.
	CreateShadow(ctx context.Context, parentID int64, opts ShadowOptions) (*Account, error)

	// Proxy management
	ListProxies(ctx context.Context, page, pageSize int, protocol, status, search string, sortBy, sortOrder string) ([]Proxy, int64, error)
	ListProxiesWithAccountCount(ctx context.Context, page, pageSize int, protocol, status, search string, sortBy, sortOrder string) ([]ProxyWithAccountCount, int64, error)
	GetAllProxies(ctx context.Context) ([]Proxy, error)
	GetAllProxiesWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error)
	GetProxy(ctx context.Context, id int64) (*Proxy, error)
	GetProxiesByIDs(ctx context.Context, ids []int64) ([]Proxy, error)
	CreateProxy(ctx context.Context, input *CreateProxyInput) (*Proxy, error)
	UpdateProxy(ctx context.Context, id int64, input *UpdateProxyInput) (*Proxy, error)
	DeleteProxy(ctx context.Context, id int64) error
	BatchDeleteProxies(ctx context.Context, ids []int64) (*ProxyBatchDeleteResult, error)
	GetProxyAccounts(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error)
	CheckProxyExists(ctx context.Context, host string, port int, username, password string) (bool, error)
	TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error)
	CheckProxyQuality(ctx context.Context, id int64) (*ProxyQualityCheckResult, error)

	// Redeem code management
	ListRedeemCodes(ctx context.Context, page, pageSize int, codeType, status, search string, sortBy, sortOrder string) ([]RedeemCode, int64, error)
	GetRedeemCode(ctx context.Context, id int64) (*RedeemCode, error)
	GenerateRedeemCodes(ctx context.Context, input *GenerateRedeemCodesInput) ([]RedeemCode, error)
	DeleteRedeemCode(ctx context.Context, id int64) error
	BatchDeleteRedeemCodes(ctx context.Context, ids []int64) (int64, error)
	ExpireRedeemCode(ctx context.Context, id int64) (*RedeemCode, error)
	ResetAccountQuota(ctx context.Context, id int64) error
}

// CreateUserInput represents input for creating a new user via admin operations.
type CreateUserInput struct {
	Email         string
	Password      string
	Username      string
	Notes         string
	Role          string
	Balance       *float64
	Concurrency   int
	RPMLimit      int
	AllowedGroups []int64
	ActorAdminID  int64
}

type UpdateUserInput struct {
	Email         string
	Password      string
	Username      *string
	Notes         *string
	Role          string
	Balance       *float64 // 使用指针区分"未提供"和"设置为0"
	Concurrency   *int     // 使用指针区分"未提供"和"设置为0"
	RPMLimit      *int     // 使用指针区分"未提供"和"设置为0"
	Status        string
	AllowedGroups *[]int64 // 使用指针区分"未提供"和"设置为空数组"
	// GroupRates 用户专属分组倍率配置
	// map[groupID]*rate，nil 表示删除该分组的专属倍率
	GroupRates   map[int64]*float64
	ActorAdminID int64
}

type AdminBindAuthIdentityInput struct {
	ProviderType    string
	ProviderKey     string
	ProviderSubject string
	Issuer          *string
	Metadata        map[string]any
	Channel         *AdminBindAuthIdentityChannelInput
}

type AdminBindAuthIdentityChannelInput struct {
	Channel        string
	ChannelAppID   string
	ChannelSubject string
	Metadata       map[string]any
}

type AdminBoundAuthIdentity struct {
	UserID          int64                          `json:"user_id"`
	ProviderType    string                         `json:"provider_type"`
	ProviderKey     string                         `json:"provider_key"`
	ProviderSubject string                         `json:"provider_subject"`
	VerifiedAt      *time.Time                     `json:"verified_at,omitempty"`
	Issuer          *string                        `json:"issuer,omitempty"`
	Metadata        map[string]any                 `json:"metadata"`
	CreatedAt       time.Time                      `json:"created_at"`
	UpdatedAt       time.Time                      `json:"updated_at"`
	Channel         *AdminBoundAuthIdentityChannel `json:"channel,omitempty"`
}

type AdminBoundAuthIdentityChannel struct {
	Channel        string         `json:"channel"`
	ChannelAppID   string         `json:"channel_app_id"`
	ChannelSubject string         `json:"channel_subject"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type CreateGroupInput struct {
	Name                 string
	Description          string
	Platform             string
	RateMultiplier       float64
	IsExclusive          bool
	SubscriptionType     string // standard/subscription
	RequiredAccountLevel string
	DailyLimitUSD        *float64 // 日限额 (USD)
	WeeklyLimitUSD       *float64 // 周限额 (USD)
	MonthlyLimitUSD      *float64 // 月限额 (USD)
	// 图片生成计费配置（仅 antigravity 平台使用）
	AllowImageGeneration         bool
	AllowBatchImageGeneration    bool
	ImageRateIndependent         bool
	ImageRateMultiplier          *float64
	BatchImageDiscountMultiplier *float64
	BatchImageHoldMultiplier     *float64
	ImagePrice1K                 *float64
	ImagePrice2K                 *float64
	ImagePrice4K                 *float64
	VideoRateIndependent         bool
	VideoRateMultiplier          *float64
	VideoPrice480P               *float64
	VideoPrice720P               *float64
	VideoPrice1080P              *float64
	PeakRateEnabled              bool
	PeakStart                    string
	PeakEnd                      string
	PeakRateMultiplier           *float64
	// Codex alpha/search 网页搜索单次价格（USD/次，仅 OpenAI 平台使用）；nil/负数使用默认价 0.01。
	WebSearchPricePerCall *float64
	ClaudeCodeOnly        bool   // 仅允许 Claude Code 客户端
	FallbackGroupID       *int64 // 降级分组 ID
	// 无效请求兜底分组 ID（仅 anthropic 平台使用）
	FallbackGroupIDOnInvalidRequest *int64
	// 模型路由配置（仅 anthropic 平台使用）
	ModelRouting        map[string][]int64
	ModelRoutingEnabled bool // 是否启用模型路由
	MCPXMLInject        *bool
	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes []string
	// OpenAI Messages 调度配置（仅 openai 平台使用）
	AllowMessagesDispatch       bool
	DefaultMappedModel          string
	RequireOAuthOnly            bool
	RequirePrivacySet           bool
	MessagesDispatchModelConfig OpenAIMessagesDispatchModelConfig
	ModelsListConfig            GroupModelsListConfig
	// RPMLimit 分组 RPM 上限（0 = 不限制）
	RPMLimit int
	// 从指定分组复制账号（创建分组后在同一事务内绑定）
	CopyAccountsFromGroupIDs []int64
}

type UpdateGroupInput struct {
	Name                 string
	Description          *string
	Platform             string
	RateMultiplier       *float64 // 使用指针以支持设置为0
	IsExclusive          *bool
	Status               string
	SubscriptionType     string // standard/subscription
	RequiredAccountLevel *string
	DailyLimitUSD        *float64 // 日限额 (USD)
	WeeklyLimitUSD       *float64 // 周限额 (USD)
	MonthlyLimitUSD      *float64 // 月限额 (USD)
	// 图片生成计费配置（仅 antigravity 平台使用）
	AllowImageGeneration         *bool
	AllowBatchImageGeneration    *bool
	ImageRateIndependent         *bool
	ImageRateMultiplier          *float64
	BatchImageDiscountMultiplier *float64
	BatchImageHoldMultiplier     *float64
	ImagePrice1K                 *float64
	ImagePrice2K                 *float64
	ImagePrice4K                 *float64
	VideoRateIndependent         *bool
	VideoRateMultiplier          *float64
	VideoPrice480P               *float64
	VideoPrice720P               *float64
	VideoPrice1080P              *float64
	PeakRateEnabled              *bool
	PeakStart                    *string
	PeakEnd                      *string
	PeakRateMultiplier           *float64
	// nil 表示不修改，负数表示清除并恢复默认价 0.01，0 表示免费。
	WebSearchPricePerCall *float64
	ClaudeCodeOnly        *bool  // 仅允许 Claude Code 客户端
	FallbackGroupID       *int64 // 降级分组 ID
	// 无效请求兜底分组 ID（仅 anthropic 平台使用）
	FallbackGroupIDOnInvalidRequest *int64
	// 模型路由配置（仅 anthropic 平台使用）
	ModelRouting        map[string][]int64
	ModelRoutingEnabled *bool // 是否启用模型路由
	MCPXMLInject        *bool
	// 支持的模型系列（仅 antigravity 平台使用）
	SupportedModelScopes *[]string
	// OpenAI Messages 调度配置（仅 openai 平台使用）
	AllowMessagesDispatch       *bool
	DefaultMappedModel          *string
	RequireOAuthOnly            *bool
	RequirePrivacySet           *bool
	MessagesDispatchModelConfig *OpenAIMessagesDispatchModelConfig
	ModelsListConfig            *GroupModelsListConfig
	// RPMLimit 分组 RPM 上限（0 = 不限制），nil 表示未提供不改动。
	RPMLimit *int
	// 从指定分组复制账号（同步操作：先清空当前分组的账号绑定，再绑定源分组的账号）
	CopyAccountsFromGroupIDs []int64
}

type CreateAccountInput struct {
	Name               string
	Notes              *string
	Platform           string
	AccountLevel       string
	Type               string
	Credentials        map[string]any
	Extra              map[string]any
	OwnerUserID        *int64
	ShareMode          string
	ShareStatus        string
	SharePolicyID      *int64
	ProxyID            *int64
	Concurrency        int
	Priority           int
	RateMultiplier     *float64 // 账号计费倍率（>=0，允许 0）
	LoadFactor         *int
	GroupIDs           []int64
	ExpiresAt          *int64
	AutoPauseOnExpired *bool
	ProbeEnabled       *bool
	// SkipDefaultGroupBind prevents auto-binding to platform default group when GroupIDs is empty.
	SkipDefaultGroupBind bool
	// SkipMixedChannelCheck skips the mixed channel risk check when binding groups.
	// This should only be set when the caller has explicitly confirmed the risk.
	SkipMixedChannelCheck bool
}

// ShadowOptions controls creation of an OpenAI spark shadow account.
type ShadowOptions struct {
	Name        string
	Priority    int
	Concurrency int
	GroupIDs    []int64
}

type UpdateAccountInput struct {
	Name                  string
	Notes                 *string
	Type                  string // Account type: oauth, setup-token, apikey
	AccountLevel          *string
	Credentials           map[string]any
	Extra                 map[string]any
	OwnerUserID           *int64
	ShareMode             string
	ShareStatus           string
	SharePolicyID         *int64
	ProxyID               *int64
	Concurrency           *int     // 使用指针区分"未提供"和"设置为0"
	Priority              *int     // 使用指针区分"未提供"和"设置为0"
	RateMultiplier        *float64 // 账号计费倍率（>=0，允许 0）
	LoadFactor            *int
	Status                string
	GroupIDs              *[]int64
	ExpiresAt             *int64
	AutoPauseOnExpired    *bool
	SkipMixedChannelCheck bool // 跳过混合渠道检查（用户已确认风险）
}

// BulkUpdateAccountsInput describes the payload for bulk updating accounts.
type BulkUpdateAccountsInput struct {
	AccountIDs     []int64
	Filters        *BulkUpdateAccountFilters
	Name           string
	ProxyID        *int64
	Concurrency    *int
	Priority       *int
	RateMultiplier *float64 // 账号计费倍率（>=0，允许 0）
	LoadFactor     *int
	Status         string
	Schedulable    *bool
	AccountLevel   *string
	GroupIDs       *[]int64
	Credentials    map[string]any
	Extra          map[string]any
	ProbeEnabled   *bool
	// SkipMixedChannelCheck skips the mixed channel risk check when binding groups.
	// This should only be set when the caller has explicitly confirmed the risk.
	SkipMixedChannelCheck bool
}

type BulkUpdateAccountFilters struct {
	Platform    string
	Type        string
	Status      string
	Group       string
	ProxyID     int64
	Search      string
	OwnerSearch string
	PrivacyMode string
}

// BulkUpdateAccountResult captures the result for a single account update.
type BulkUpdateAccountResult struct {
	AccountID int64  `json:"account_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// AdminUpdateAPIKeyGroupIDResult is the result of AdminUpdateAPIKeyGroupID.
type AdminUpdateAPIKeyGroupIDResult struct {
	APIKey                 *APIKey
	AutoGrantedGroupAccess bool   // true if a new exclusive group permission was auto-added
	GrantedGroupID         *int64 // the group ID that was auto-granted
	GrantedGroupName       string // the group name that was auto-granted
}

// ReplaceUserGroupResult 分组替换操作的结果
type ReplaceUserGroupResult struct {
	MigratedKeys int64 // 迁移的 Key 数量
}

// UserRPMStatus describes a user's current per-minute RPM usage.
type UserRPMStatus struct {
	UserRPMUsed  int                  `json:"user_rpm_used"`
	UserRPMLimit int                  `json:"user_rpm_limit"`
	PerGroup     []UserGroupRPMStatus `json:"per_group"`
}

// UserGroupRPMStatus describes current per-minute RPM usage for one user/group pair.
type UserGroupRPMStatus struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	Used      int    `json:"used"`
	Limit     int    `json:"limit"`
	Source    string `json:"source"` // "group" | "override"
}

// BulkUpdateAccountsResult is the aggregated response for bulk updates.
type BulkUpdateAccountsResult struct {
	Success    int                       `json:"success"`
	Failed     int                       `json:"failed"`
	SuccessIDs []int64                   `json:"success_ids"`
	FailedIDs  []int64                   `json:"failed_ids"`
	Results    []BulkUpdateAccountResult `json:"results"`
}

type CreateProxyInput struct {
	Name           string
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	MaxAccounts    int
	ExpiresAt      *time.Time
	FallbackMode   string
	BackupProxyID  *int64
	ExpiryWarnDays int
}

type UpdateProxyInput struct {
	Name           string
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	Status         string
	MaxAccounts    *int
	ExpiresAt      *time.Time
	FallbackMode   string
	BackupProxyID  *int64
	ExpiryWarnDays int
}

type GenerateRedeemCodesInput struct {
	Count        int
	Type         string
	Value        float64
	GroupID      *int64 // 订阅类型专用：关联的分组ID
	ValidityDays int    // 订阅类型专用：有效天数
	ExpiresAt    *time.Time
}

type ProxyBatchDeleteResult struct {
	DeletedIDs []int64                   `json:"deleted_ids"`
	Skipped    []ProxyBatchDeleteSkipped `json:"skipped"`
}

type ProxyBatchDeleteSkipped struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// ProxyTestResult represents the result of testing a proxy
type ProxyTestResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	IPAddress   string `json:"ip_address,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

type ProxyQualityCheckResult struct {
	ProxyID        int64                   `json:"proxy_id"`
	Score          int                     `json:"score"`
	Grade          string                  `json:"grade"`
	Summary        string                  `json:"summary"`
	ExitIP         string                  `json:"exit_ip,omitempty"`
	Country        string                  `json:"country,omitempty"`
	CountryCode    string                  `json:"country_code,omitempty"`
	BaseLatencyMs  int64                   `json:"base_latency_ms,omitempty"`
	PassedCount    int                     `json:"passed_count"`
	WarnCount      int                     `json:"warn_count"`
	FailedCount    int                     `json:"failed_count"`
	ChallengeCount int                     `json:"challenge_count"`
	CheckedAt      int64                   `json:"checked_at"`
	Items          []ProxyQualityCheckItem `json:"items"`
}

type ProxyQualityCheckItem struct {
	Target     string `json:"target"`
	Status     string `json:"status"` // pass/warn/fail/challenge
	HTTPStatus int    `json:"http_status,omitempty"`
	LatencyMs  int64  `json:"latency_ms,omitempty"`
	Message    string `json:"message,omitempty"`
	CFRay      string `json:"cf_ray,omitempty"`
}

// ProxyExitInfo represents proxy exit information from ip-api.com
type ProxyExitInfo struct {
	IP          string
	City        string
	Region      string
	Country     string
	CountryCode string
}

// ProxyExitInfoProber tests proxy connectivity and retrieves exit information
type ProxyExitInfoProber interface {
	ProbeProxy(ctx context.Context, proxyURL string) (*ProxyExitInfo, int64, error)
}

type groupExistenceBatchReader interface {
	ExistsByIDs(ctx context.Context, ids []int64) (map[int64]bool, error)
}

type proxyQualityTarget struct {
	Target          string
	URL             string
	Method          string
	AllowedStatuses map[int]struct{}
}

var proxyQualityTargets = []proxyQualityTarget{
	{
		Target: "openai",
		URL:    "https://api.openai.com/v1/models",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	},
	{
		Target: "anthropic",
		URL:    "https://api.anthropic.com/v1/messages",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized:     {},
			http.StatusMethodNotAllowed: {},
			http.StatusNotFound:         {},
			http.StatusBadRequest:       {},
		},
	},
	{
		Target: "gemini",
		URL:    "https://generativelanguage.googleapis.com/$discovery/rest?version=v1beta",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusOK: {},
		},
	},
}

const (
	proxyQualityRequestTimeout        = 15 * time.Second
	proxyQualityResponseHeaderTimeout = 10 * time.Second
	proxyQualityMaxBodyBytes          = int64(8 * 1024)
	proxyQualityClientUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
)

var ErrRPMStatusUnavailable = infraerrors.New(http.StatusNotImplemented, "RPM_STATUS_UNAVAILABLE", "RPM cache not available")

// adminServiceImpl implements AdminService
type adminServiceImpl struct {
	userRepo                UserRepository
	groupRepo               GroupRepository
	groupDuplicateRepo      GroupDuplicateRepository
	accountRepo             AccountRepository
	accountDuplicateRepo    AccountDuplicateRepository
	proxyRepo               ProxyRepository
	apiKeyRepo              APIKeyRepository
	redeemCodeRepo          RedeemCodeRepository
	userGroupRateRepo       UserGroupRateRepository
	userRPMCache            UserRPMCache
	billingCacheService     *BillingCacheService
	proxyProber             ProxyExitInfoProber
	proxyLatencyCache       ProxyLatencyCache
	authCacheInvalidator    APIKeyAuthCacheInvalidator
	entClient               *dbent.Client // 用于开启数据库事务
	settingService          *SettingService
	defaultSubAssigner      DefaultSubscriptionAssigner
	userSubRepo             UserSubscriptionRepository
	privacyClientFactory    PrivacyClientFactory
	runtimeBlocker          AccountRuntimeBlocker
	affiliateService        adminRechargeAffiliateAccruer
	privateGroupProvisioner UserPrivateGroupProvisioner
	systemNoticeService     *SystemNoticeService
}

type adminRechargeAffiliateAccruer interface {
	AccrueInviteRebate(ctx context.Context, inviteeUserID int64, baseRechargeAmount float64) (float64, error)
}

type userGroupRateBatchReader interface {
	GetByUserIDs(ctx context.Context, userIDs []int64) (map[int64]map[int64]float64, error)
}

// NewAdminService creates a new AdminService
func NewAdminService(
	userRepo UserRepository,
	groupRepo AdminGroupRepository,
	accountRepo AdminAccountRepository,
	proxyRepo ProxyRepository,
	apiKeyRepo APIKeyRepository,
	redeemCodeRepo RedeemCodeRepository,
	userGroupRateRepo UserGroupRateRepository,
	userRPMCache UserRPMCache,
	billingCacheService *BillingCacheService,
	proxyProber ProxyExitInfoProber,
	proxyLatencyCache ProxyLatencyCache,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	entClient *dbent.Client,
	settingService *SettingService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	userSubRepo UserSubscriptionRepository,
	privacyClientFactory PrivacyClientFactory,
	runtimeBlocker AccountRuntimeBlocker,
	affiliateService *AffiliateService,
) AdminService {
	return &adminServiceImpl{
		userRepo:             userRepo,
		groupRepo:            groupRepo,
		groupDuplicateRepo:   groupRepo,
		accountRepo:          accountRepo,
		accountDuplicateRepo: accountRepo,
		proxyRepo:            proxyRepo,
		apiKeyRepo:           apiKeyRepo,
		redeemCodeRepo:       redeemCodeRepo,
		userGroupRateRepo:    userGroupRateRepo,
		userRPMCache:         userRPMCache,
		billingCacheService:  billingCacheService,
		proxyProber:          proxyProber,
		proxyLatencyCache:    proxyLatencyCache,
		authCacheInvalidator: authCacheInvalidator,
		entClient:            entClient,
		settingService:       settingService,
		defaultSubAssigner:   defaultSubAssigner,
		userSubRepo:          userSubRepo,
		privacyClientFactory: privacyClientFactory,
		runtimeBlocker:       runtimeBlocker,
		affiliateService:     affiliateService,
	}
}

func SetAdminUserPrivateGroupProvisioner(svc AdminService, provisioner UserPrivateGroupProvisioner) AdminService {
	if impl, ok := svc.(*adminServiceImpl); ok {
		impl.privateGroupProvisioner = provisioner
	}
	return svc
}

func SetAdminSystemNoticeService(svc AdminService, noticeService *SystemNoticeService) AdminService {
	if impl, ok := svc.(*adminServiceImpl); ok {
		impl.systemNoticeService = noticeService
	}
	return svc
}

func (s *adminServiceImpl) UpdateUserPoints(ctx context.Context, userID int64, points float64, operation string, notes string, operatorUserID int64) (*User, error) {
	if points <= 0 {
		return nil, infraerrors.BadRequest("POINTS_AMOUNT_INVALID", "points amount must be greater than 0")
	}

	var delta float64
	switch operation {
	case "set":
	case "add":
		delta = points
	case "subtract":
		delta = -points
	default:
		return nil, infraerrors.BadRequest("POINTS_OPERATION_INVALID", "invalid points operation")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin points adjustment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	if operation == "set" {
		currentPoints, err := currentPointsBalanceInTx(txCtx, tx, userID)
		if err != nil {
			return nil, fmt.Errorf("lock user points: %w", err)
		}
		delta = points - currentPoints
	}
	if delta == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit noop points adjustment transaction: %w", err)
		}
		updated, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return nil, err
		}
		return updated, nil
	}

	code, err := GenerateRedeemCode()
	if err != nil {
		return nil, fmt.Errorf("generate points adjustment code: %w", err)
	}
	now := time.Now()
	adjustmentRecord := &RedeemCode{
		Code:   code,
		Type:   AdjustmentTypeAdminPoints,
		Value:  delta,
		Status: StatusUsed,
		UsedBy: &userID,
		UsedAt: &now,
		Notes:  notes,
	}
	if err := s.redeemCodeRepo.Create(txCtx, adjustmentRecord); err != nil {
		return nil, fmt.Errorf("create points adjustment redeem code: %w", err)
	}
	if err := applyPointsAdjustmentInTx(txCtx, tx, pointsAdjustmentInput{
		UserID:         userID,
		Delta:          delta,
		Reason:         "admin_adjustment",
		RefType:        "redeem_code",
		RefID:          adjustmentRecord.ID,
		OperatorUserID: operatorUserID,
		Metadata: map[string]any{
			"operation": operation,
			"notes":     notes,
		},
	}); err != nil {
		return nil, fmt.Errorf("update user points: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit points adjustment transaction: %w", err)
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.billingCacheService.InvalidateUserBalance(cacheCtx, userID); err != nil {
				logger.LegacyPrintf("service.admin", "invalidate user balance cache after points update failed: user_id=%d err=%v", userID, err)
			}
		}()
	}

	updated, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *adminServiceImpl) UpdateUserLoadFactorCredits(ctx context.Context, userID int64, amount int, operation string, notes string, operatorUserID int64) (*User, error) {
	if amount <= 0 {
		return nil, infraerrors.BadRequest("LOAD_FACTOR_CREDITS_AMOUNT_INVALID", "load factor credits amount must be greater than 0")
	}

	var delta int
	switch operation {
	case "set":
	case "add":
		delta = amount
	case "subtract":
		delta = -amount
	default:
		return nil, infraerrors.BadRequest("LOAD_FACTOR_CREDITS_OPERATION_INVALID", "invalid load factor credits operation")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin load factor credits adjustment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	balanceBefore, err := currentLoadFactorCreditsBalanceInTx(txCtx, tx, userID)
	if err != nil {
		return nil, fmt.Errorf("lock user load factor credits: %w", err)
	}
	if operation == "set" {
		delta = amount - balanceBefore
	}
	balanceAfter := balanceBefore + delta
	if balanceAfter < 0 {
		return nil, infraerrors.BadRequest("LOAD_FACTOR_CREDITS_BALANCE_NEGATIVE", "load factor credits balance cannot be negative")
	}
	if delta != 0 {
		if err := applyLoadFactorCreditsAdjustmentInTx(txCtx, tx, loadFactorCreditsAdjustmentInput{
			UserID:         userID,
			Delta:          delta,
			BalanceBefore:  balanceBefore,
			BalanceAfter:   balanceAfter,
			OperatorUserID: operatorUserID,
			Metadata: map[string]any{
				"operation": operation,
				"notes":     notes,
			},
		}); err != nil {
			return nil, fmt.Errorf("update user load factor credits: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit load factor credits adjustment transaction: %w", err)
	}

	if s.authCacheInvalidator != nil && delta != 0 {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}

	updated, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

type loadFactorCreditsAdjustmentInput struct {
	UserID         int64
	Delta          int
	Reason         string
	RefType        string
	RefID          int64
	BalanceBefore  int
	BalanceAfter   int
	OperatorUserID int64
	Metadata       map[string]any
}

func currentLoadFactorCreditsBalanceInTx(ctx context.Context, tx *dbent.Tx, userID int64) (int, error) {
	if tx == nil {
		return 0, errors.New("load factor credits lookup requires transaction")
	}
	if userID <= 0 {
		return 0, ErrUserNotFound
	}
	queryer, ok := tx.Driver().(serviceSQLQueryer)
	if !ok {
		return 0, errors.New("load factor credits lookup requires QueryContext support")
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT load_factor_credits_balance
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, userID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, ErrUserNotFound
	}
	var balance int
	if err := rows.Scan(&balance); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return balance, nil
}

func applyLoadFactorCreditsAdjustmentInTx(ctx context.Context, tx *dbent.Tx, in loadFactorCreditsAdjustmentInput) error {
	if tx == nil {
		return errors.New("load factor credits adjustment requires transaction")
	}
	if in.UserID <= 0 {
		return ErrUserNotFound
	}
	if in.Delta == 0 {
		return nil
	}
	execer, ok := tx.Driver().(serviceSQLExecer)
	if !ok {
		return errors.New("load factor credits adjustment requires ExecContext support")
	}

	if _, err := execer.ExecContext(ctx, `
		UPDATE users
		SET load_factor_credits_balance = $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, in.BalanceAfter, in.UserID); err != nil {
		return err
	}

	direction := "credit"
	amount := in.Delta
	if amount < 0 {
		direction = "debit"
		amount = -amount
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "admin_adjustment"
	}
	refType := strings.TrimSpace(in.RefType)
	var refID any
	if in.RefID > 0 {
		refID = in.RefID
	}
	metadata := in.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	rawMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var operatorUserID any
	if in.OperatorUserID > 0 {
		operatorUserID = in.OperatorUserID
	}
	_, err = execer.ExecContext(ctx, `
		INSERT INTO user_load_factor_ledger (
			user_id, account_id, direction, amount, reason, ref_type, ref_id,
			balance_before, balance_after, operator_user_id, metadata
		) VALUES (
			$1, NULL, $2, $3, $4, $5, $6,
			$7, $8, $9, $10::jsonb
		)
	`, in.UserID, direction, amount, reason, refType, refID, in.BalanceBefore, in.BalanceAfter, operatorUserID, string(rawMetadata))
	return err
}

type groupScopeFilterRepository interface {
	ListWithScopeFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool, scope string) ([]Group, *pagination.PaginationResult, error)
}

func (s *adminServiceImpl) repairVisibleOpenAISharedPoolsForGroupView(ctx context.Context) error {
	if s == nil || s.accountRepo == nil {
		return nil
	}
	repo, ok := s.accountRepo.(accountQuotaPoolGlobalVisibleRepairRepository)
	if !ok {
		return nil
	}
	_, err := repo.RepairAllVisibleOpenAISharedPoolBindings(ctx)
	return err
}

func filterGroupsByScope(groups []Group, scope string) []Group {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" || scope == "all" {
		return groups
	}
	scope = NormalizeGroupScope(scope)
	filtered := make([]Group, 0, len(groups))
	for _, group := range groups {
		if NormalizeGroupScope(group.Scope) == scope {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func normalizeImageRateMultiplier(multiplier *float64) float64 {
	if multiplier == nil || *multiplier < 0 {
		return 1.0
	}
	return *multiplier
}

func normalizeVideoRateMultiplier(multiplier *float64) float64 {
	if multiplier == nil {
		return 1.0
	}
	if *multiplier < 0 {
		return 0
	}
	return *multiplier
}

// AdminUpdateAPIKeyGroupRoutes 管理员修改 API Key 多分组路由绑定。
// routes 为空时解绑；非空时校验分组状态、平台一致性和订阅权限，并自动授予专属标准分组权限。
func (s *adminServiceImpl) AdminUpdateAPIKeyGroupRoutes(ctx context.Context, keyID int64, groupID *int64, routes []APIKeyGroupRoute) (*AdminUpdateAPIKeyGroupIDResult, error) {
	apiKey, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}

	groupRoutes, err := normalizeAPIKeyGroupRoutes(routes)
	if err != nil {
		return nil, err
	}
	if len(groupRoutes) == 0 {
		groupRoutes = defaultAPIKeyGroupRoute(groupID)
	}

	result := &AdminUpdateAPIKeyGroupIDResult{APIKey: apiKey}
	if len(groupRoutes) == 0 {
		apiKey.GroupID = nil
		apiKey.Group = nil
		apiKey.GroupRoutes = nil
		if err := s.apiKeyRepo.Update(ctx, apiKey); err != nil {
			return nil, fmt.Errorf("update api key: %w", err)
		}
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
		}
		return result, nil
	}

	platform := ""
	exclusiveStandardGroupIDs := make([]int64, 0)
	for i := range groupRoutes {
		group, err := s.groupRepo.GetByID(ctx, groupRoutes[i].GroupID)
		if err != nil {
			return nil, err
		}
		if group.Status != StatusActive {
			return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
		}
		if i == 0 {
			platform = strings.TrimSpace(group.Platform)
		} else if !strings.EqualFold(platform, strings.TrimSpace(group.Platform)) {
			return nil, infraerrors.BadRequest("API_KEY_GROUP_ROUTE_INVALID", "all groups must use the same platform")
		}
		if group.IsSubscriptionType() {
			if s.userSubRepo == nil {
				return nil, infraerrors.InternalServer("SUBSCRIPTION_REPOSITORY_UNAVAILABLE", "subscription repository is not configured")
			}
			if _, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, apiKey.UserID, group.ID); err != nil {
				if errors.Is(err, ErrSubscriptionNotFound) {
					return nil, infraerrors.BadRequest("SUBSCRIPTION_REQUIRED", "user does not have an active subscription for this group")
				}
				return nil, err
			}
		}
		if group.IsExclusive && !group.IsSubscriptionType() {
			exclusiveStandardGroupIDs = append(exclusiveStandardGroupIDs, group.ID)
			if result.GrantedGroupID == nil {
				gid := group.ID
				result.AutoGrantedGroupAccess = true
				result.GrantedGroupID = &gid
				result.GrantedGroupName = group.Name
			}
		}
		groupRoutes[i].Group = group
	}

	normalizeAPIKeyGroupRoutePriority(groupRoutes)
	primaryGroupID := primaryGroupIDFromRoutes(groupRoutes)
	if primaryGroupID == nil {
		return nil, infraerrors.BadRequest("API_KEY_GROUP_ROUTE_INVALID", "at least one enabled group route is required")
	}
	apiKey.GroupID = primaryGroupID
	apiKey.Group = nil
	for i := range groupRoutes {
		if groupRoutes[i].GroupID == *primaryGroupID {
			apiKey.Group = groupRoutes[i].Group
			break
		}
	}
	apiKey.GroupRoutes = groupRoutes

	opCtx := ctx
	var tx *dbent.Tx
	if len(exclusiveStandardGroupIDs) > 0 && s.entClient != nil {
		var txErr error
		tx, txErr = s.entClient.Tx(ctx)
		if txErr != nil {
			return nil, fmt.Errorf("begin transaction: %w", txErr)
		}
		defer func() { _ = tx.Rollback() }()
		opCtx = dbent.NewTxContext(ctx, tx)
	}

	for _, gid := range exclusiveStandardGroupIDs {
		if err := s.userRepo.AddGroupToAllowedGroups(opCtx, apiKey.UserID, gid); err != nil {
			return nil, fmt.Errorf("add group to user allowed groups: %w", err)
		}
	}
	if err := s.apiKeyRepo.Update(opCtx, apiKey); err != nil {
		return nil, fmt.Errorf("update api key: %w", err)
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	result.APIKey = apiKey
	return result, nil
}

func normalizeGrokOAuthConcurrency(platform, accountType string, concurrency int) int {
	if platform == PlatformGrok && accountType == AccountTypeOAuth && concurrency <= 0 {
		return 1
	}
	return concurrency
}

func (s *adminServiceImpl) notifyAccountCreated(ctx context.Context, account *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountCreated(ctx, account)
}

func (s *adminServiceImpl) notifyAccountDeleted(ctx context.Context, account *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountDeleted(ctx, account)
}

func (s *adminServiceImpl) notifyAccountChanged(ctx context.Context, before, after *Account) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyAccountChanged(ctx, before, after)
}

func (s *adminServiceImpl) notifyGroupRateMultiplierChanged(ctx context.Context, group *Group, before, after float64, event string) {
	if s == nil || s.systemNoticeService == nil || group == nil {
		return
	}
	invalidateUserGroupRateCacheByGroupID(group.ID)
	userIDs := collectGroupNoticeUserIDs(ctx, group, s.apiKeyRepo, s.userSubRepo, s.userGroupRateRepo)
	s.systemNoticeService.NotifyGroupRateMultiplierChanged(ctx, userIDs, group, before, after, event)
}

func (s *adminServiceImpl) groupForRateNotice(ctx context.Context, groupID int64) *Group {
	if s == nil || s.groupRepo == nil || groupID <= 0 {
		return &Group{ID: groupID}
	}
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		logger.LegacyPrintf("service.admin", "failed to load group for rate notice: group_id=%d err=%v", groupID, err)
		return &Group{ID: groupID}
	}
	return group
}

func (s *adminServiceImpl) notifyUserGroupRateChanges(ctx context.Context, userID int64, beforeRates map[int64]float64, changedRates map[int64]*float64) {
	if s == nil || s.systemNoticeService == nil || s.groupRepo == nil || userID <= 0 || changedRates == nil {
		return
	}
	invalidateUserGroupRateCacheByUserID(userID)
	if len(changedRates) == 0 {
		for groupID, before := range beforeRates {
			group, err := s.groupRepo.GetByIDLite(ctx, groupID)
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to load group for user group rate notice: group_id=%d err=%v", groupID, err)
				continue
			}
			beforeRate := before
			s.systemNoticeService.NotifyUserGroupRateChanged(ctx, userID, group, &beforeRate, nil)
		}
		return
	}
	for groupID, afterRate := range changedRates {
		var beforePtr *float64
		if before, ok := beforeRates[groupID]; ok {
			beforeRate := before
			beforePtr = &beforeRate
		}
		if !noticeOptionalRatesChanged(beforePtr, afterRate) {
			continue
		}
		group, err := s.groupRepo.GetByIDLite(ctx, groupID)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to load group for user group rate notice: group_id=%d err=%v", groupID, err)
			continue
		}
		s.systemNoticeService.NotifyUserGroupRateChanged(ctx, userID, group, beforePtr, afterRate)
	}
}

func (s *adminServiceImpl) notifyClearedGroupRateMultipliers(ctx context.Context, group *Group, beforeRates map[int64]float64) {
	if s == nil || s.systemNoticeService == nil || group == nil {
		return
	}
	invalidateUserGroupRateCacheByGroupID(group.ID)
	for userID, before := range beforeRates {
		beforeRate := before
		s.systemNoticeService.NotifyUserGroupRateChanged(ctx, userID, group, &beforeRate, nil)
	}
}

func (s *adminServiceImpl) notifySyncedGroupRateMultipliers(ctx context.Context, group *Group, beforeRates map[int64]float64, entries []GroupRateMultiplierInput) {
	if s == nil || s.systemNoticeService == nil || group == nil {
		return
	}
	invalidateUserGroupRateCacheByGroupID(group.ID)
	afterRates := make(map[int64]float64, len(entries))
	for _, entry := range entries {
		if entry.UserID > 0 {
			afterRates[entry.UserID] = entry.RateMultiplier
		}
	}
	userIDs := make(map[int64]struct{}, len(beforeRates)+len(afterRates))
	for userID := range beforeRates {
		userIDs[userID] = struct{}{}
	}
	for userID := range afterRates {
		userIDs[userID] = struct{}{}
	}
	for userID := range userIDs {
		var beforePtr *float64
		if before, ok := beforeRates[userID]; ok {
			beforeRate := before
			beforePtr = &beforeRate
		}
		var afterPtr *float64
		if after, ok := afterRates[userID]; ok {
			afterRate := after
			afterPtr = &afterRate
		}
		if !noticeOptionalRatesChanged(beforePtr, afterPtr) {
			continue
		}
		s.systemNoticeService.NotifyUserGroupRateChanged(ctx, userID, group, beforePtr, afterPtr)
	}
}

func collectGroupNoticeUserIDs(ctx context.Context, group *Group, apiKeyRepo APIKeyRepository, userSubRepo UserSubscriptionRepository, userGroupRateRepo UserGroupRateRepository) []int64 {
	if group == nil || group.ID <= 0 {
		return nil
	}
	seen := make(map[int64]struct{})
	customRateUserIDs := make(map[int64]struct{})
	if userGroupRateRepo != nil {
		rates, err := userGroupRateRepo.GetRateMultipliersByGroupID(ctx, group.ID)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to list custom group rates for notice: group_id=%d err=%v", group.ID, err)
		} else {
			for userID := range rates {
				customRateUserIDs[userID] = struct{}{}
			}
		}
	}
	add := func(userID int64) {
		if userID <= 0 {
			return
		}
		if _, ok := customRateUserIDs[userID]; ok {
			return
		}
		seen[userID] = struct{}{}
	}
	if group.OwnerUserID != nil {
		add(*group.OwnerUserID)
	}
	if apiKeyRepo != nil {
		params := pagination.PaginationParams{Page: 1, PageSize: 100}
		for {
			keys, page, err := apiKeyRepo.ListByGroupID(ctx, group.ID, params)
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to list api keys for group notice: group_id=%d err=%v", group.ID, err)
				break
			}
			for i := range keys {
				add(keys[i].UserID)
			}
			if page == nil || len(keys) == 0 || int64(params.Page*params.PageSize) >= page.Total {
				break
			}
			params.Page++
		}
	}
	if userSubRepo != nil {
		params := pagination.PaginationParams{Page: 1, PageSize: 100}
		for {
			subs, page, err := userSubRepo.ListByGroupID(ctx, group.ID, params)
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to list subscriptions for group notice: group_id=%d err=%v", group.ID, err)
				break
			}
			for i := range subs {
				if subs[i].IsActive() {
					add(subs[i].UserID)
				}
			}
			if page == nil || len(subs) == 0 || int64(params.Page*params.PageSize) >= page.Total {
				break
			}
			params.Page++
		}
	}
	userIDs := make([]int64, 0, len(seen))
	for userID := range seen {
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	return userIDs
}

func (s *adminServiceImpl) notifyBulkAccountsChanged(ctx context.Context, beforeByID map[int64]*Account, accountIDs []int64) {
	if s == nil || s.systemNoticeService == nil || len(accountIDs) == 0 {
		return
	}
	afterAccounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		slog.Warn("admin.account.system_notice_bulk_reload_failed", "error", err)
		return
	}
	for _, after := range afterAccounts {
		if after == nil {
			continue
		}
		s.notifyAccountChanged(ctx, beforeByID[after.ID], after)
	}
}

func proxyAccountLimitExceededError(proxyID, current, limit, additional int64) error {
	return ProxyAccountLimitExceededError(proxyID, current, limit, additional)
}

func proxyAccountLimitBelowCurrentError(proxyID, current int64) error {
	return infraerrors.BadRequest(
		"PROXY_ACCOUNT_LIMIT_BELOW_CURRENT",
		fmt.Sprintf("proxy %d already has %d bound accounts; max_accounts cannot be lower than current count unless set to 0", proxyID, current),
	)
}

func validateProxyMaxAccountsValue(maxAccounts int) error {
	if maxAccounts < 0 {
		return infraerrors.BadRequest("PROXY_MAX_ACCOUNTS_INVALID", "max_accounts must be >= 0")
	}
	return nil
}

func (s *adminServiceImpl) ensureProxyAccountCapacity(ctx context.Context, proxyID int64, additional int64) error {
	if proxyID <= 0 || additional <= 0 || s == nil || s.proxyRepo == nil {
		return nil
	}
	proxy, err := s.proxyRepo.GetByID(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("get proxy: %w", err)
	}
	if proxy.MaxAccounts <= 0 {
		return nil
	}
	current, err := s.proxyRepo.CountAccountsByProxyID(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("count proxy accounts: %w", err)
	}
	limit := int64(proxy.MaxAccounts)
	if current+additional > limit {
		return proxyAccountLimitExceededError(proxyID, current, limit, additional)
	}
	return nil
}

func (s *adminServiceImpl) ensureAccountProxyCapacityForUpdate(ctx context.Context, account *Account, proxyID *int64) error {
	if proxyID == nil || *proxyID <= 0 {
		return nil
	}
	if account != nil && account.ProxyID != nil && *account.ProxyID == *proxyID {
		return nil
	}
	return s.ensureProxyAccountCapacity(ctx, *proxyID, 1)
}

func (s *adminServiceImpl) ensureProxyMaxAccountsCanBeSaved(ctx context.Context, proxyID int64, maxAccounts int) error {
	if err := validateProxyMaxAccountsValue(maxAccounts); err != nil {
		return err
	}
	if maxAccounts == 0 {
		return nil
	}
	current, err := s.proxyRepo.CountAccountsByProxyID(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("count proxy accounts: %w", err)
	}
	if current > int64(maxAccounts) {
		return proxyAccountLimitBelowCurrentError(proxyID, current)
	}
	return nil
}

func (s *adminServiceImpl) validateAccountLevelGroupBinding(ctx context.Context, accountPlatform, accountType, accountLevel string, credentials, extra map[string]any, groupIDs []int64) error {
	if len(groupIDs) == 0 || accountPlatform != PlatformOpenAI {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(accountType), AccountTypeAPIKey) {
		return nil
	}
	level := EffectiveOpenAISharedPoolAccountLevel(accountPlatform, accountLevel, credentials, extra)
	for _, groupID := range groupIDs {
		group, err := s.groupRepo.GetByIDLite(ctx, groupID)
		if err != nil {
			return fmt.Errorf("get group: %w", err)
		}
		required := NormalizeRequiredAccountLevel(group.RequiredAccountLevel)
		if group.Platform != PlatformOpenAI || required == "" {
			continue
		}
		if !CanOpenAIAccountJoinSharedPool(level, required) {
			return infraerrors.BadRequest(
				"ACCOUNT_GROUP_BINDING_INVALID",
				fmt.Sprintf("account_level mismatch: OpenAI account level %s cannot bind to group %s requiring %s", NormalizeOpenAISharedPoolAccountLevel(level), group.Name, required),
			)
		}
	}
	return nil
}

func (s *adminServiceImpl) validateAccountShareGroupBinding(ctx context.Context, account *Account, groupIDs []int64) error {
	if len(groupIDs) == 0 || account == nil {
		return nil
	}
	if s.groupRepo == nil {
		return errors.New("group repository not configured")
	}
	for _, groupID := range groupIDs {
		group, err := s.groupRepo.GetByIDLite(ctx, groupID)
		if err != nil {
			return fmt.Errorf("get group: %w", err)
		}
		if group == nil || group.ID <= 0 {
			return ErrGroupNotFound
		}
		if isOAuthOnlyGroup(group) && requiresOAuthOnlyGroupCheck(account.Type) {
			return infraerrors.BadRequest(
				"ACCOUNT_GROUP_BINDING_INVALID",
				fmt.Sprintf("group %s only allows OAuth accounts", group.Name),
			)
		}

		scope := NormalizeGroupScope(group.Scope)
		if account.OwnerUserID == nil {
			if scope == GroupScopeUserPrivate {
				return infraerrors.BadRequest(
					"ACCOUNT_GROUP_BINDING_INVALID",
					fmt.Sprintf("platform account cannot bind to user private group %s", group.Name),
				)
			}
			continue
		}

		if scope == GroupScopeUserPrivate {
			if group.OwnerUserID == nil || *group.OwnerUserID != *account.OwnerUserID {
				return infraerrors.BadRequest(
					"ACCOUNT_GROUP_BINDING_INVALID",
					fmt.Sprintf("owned account cannot bind to another user's private group %s", group.Name),
				)
			}
			continue
		}

		if NormalizeAccountShareMode(account.ShareMode) != AccountShareModePublic ||
			NormalizeAccountShareStatus(account.ShareStatus) != AccountShareStatusApproved {
			return infraerrors.BadRequest(
				"ACCOUNT_GROUP_BINDING_INVALID",
				fmt.Sprintf("owned account must be approved public share before binding to public group %s", group.Name),
			)
		}
	}
	return nil
}

func (s *adminServiceImpl) normalizeAccountIDsForGroupBinding(ctx context.Context, group *Group, accountIDs []int64) ([]int64, error) {
	if group == nil || len(accountIDs) == 0 {
		return accountIDs, nil
	}

	requiresOAuthFilter := group.RequireOAuthOnly &&
		(group.Platform == PlatformOpenAI ||
			group.Platform == PlatformAntigravity ||
			group.Platform == PlatformAnthropic ||
			group.Platform == PlatformGemini ||
			group.Platform == PlatformGrok)
	requiredLevel := NormalizeRequiredAccountLevel(group.RequiredAccountLevel)
	requiresLevelCheck := group.Platform == PlatformOpenAI && requiredLevel != ""
	if !requiresOAuthFilter && !requiresLevelCheck {
		return accountIDs, nil
	}
	if s.accountRepo == nil {
		return nil, errors.New("account repository not configured")
	}

	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accounts for group binding: %w", err)
	}
	accountByID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			accountByID[account.ID] = account
		}
	}

	filtered := make([]int64, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		account := accountByID[accountID]
		if account == nil {
			if requiresOAuthFilter {
				continue
			}
			return nil, fmt.Errorf("account %d not found for group binding", accountID)
		}
		if requiresOAuthFilter && account.Type == AccountTypeAPIKey {
			continue
		}
		accountLevel := EffectiveOpenAISharedPoolAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra)
		if requiresLevelCheck && account.Platform == PlatformOpenAI && !strings.EqualFold(strings.TrimSpace(account.Type), AccountTypeAPIKey) && !CanOpenAIAccountJoinSharedPool(accountLevel, requiredLevel) {
			return nil, fmt.Errorf("account_level mismatch: OpenAI account %s level %s cannot bind to group %s requiring %s", account.Name, NormalizeOpenAISharedPoolAccountLevel(accountLevel), group.Name, requiredLevel)
		}
		filtered = append(filtered, accountID)
	}
	return filtered, nil
}

func (s *adminServiceImpl) persistAccountPrivacyMode(ctx context.Context, account *Account, mode string) error {
	if s == nil || s.accountRepo == nil || account == nil || strings.TrimSpace(mode) == "" {
		return nil
	}

	writeCtx, cancel := rateLimitStateContext(ctx)
	defer cancel()

	if err := s.accountRepo.UpdateExtra(writeCtx, account.ID, map[string]any{"privacy_mode": mode}); err != nil {
		return err
	}
	if account.Platform == PlatformAntigravity {
		applyAntigravityPrivacyMode(account, mode)
		return nil
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra["privacy_mode"] = mode
	return nil
}
