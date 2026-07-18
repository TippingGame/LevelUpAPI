package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const cyberPolicyRecordedKey = "ops_cyber_recorded"

type cyberPolicyOpsErrorMeta struct {
	RequestID       string
	ClientRequestID string
	Platform        string
	Model           string
	RequestPath     string
	Stream          bool
	InboundEndpoint string
	UserAgent       string
	APIKeyPrefix    string
	UserID          int64
	APIKeyID        int64
	AccountID       int64
	GroupID         *int64
	ClientIP        string
	CreatedAt       time.Time
	SessionBlockKey string
}

func buildCyberPolicyOpsErrorEntry(meta cyberPolicyOpsErrorMeta, mark *service.CyberPolicyMark) *service.OpsInsertErrorLogInput {
	rt := int16(service.RequestTypeCyberBlocked)
	entry := &service.OpsInsertErrorLogInput{
		RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID,
		Platform: meta.Platform, Model: meta.Model, RequestPath: meta.RequestPath,
		Stream: meta.Stream, InboundEndpoint: meta.InboundEndpoint, RequestType: &rt,
		UserAgent: meta.UserAgent, APIKeyPrefix: meta.APIKeyPrefix,
		ErrorPhase: "request", ErrorType: "cyber_policy", Severity: "P3",
		StatusCode: mark.UpstreamStatus, IsBusinessLimited: true,
		ErrorMessage: "cyber_policy: " + mark.Message, ErrorBody: mark.Body,
		ErrorSource: "upstream_http", ErrorOwner: "provider", CreatedAt: meta.CreatedAt,
	}
	setCyberPolicyOpsIdentity(entry, meta, true)
	return entry
}

func buildCyberSessionBlockedOpsEntry(meta cyberPolicyOpsErrorMeta) *service.OpsInsertErrorLogInput {
	rt := int16(service.RequestTypeCyberBlocked)
	entry := &service.OpsInsertErrorLogInput{
		RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID,
		Platform: meta.Platform, Model: meta.Model, RequestPath: meta.RequestPath,
		Stream: meta.Stream, InboundEndpoint: meta.InboundEndpoint, RequestType: &rt,
		UserAgent: meta.UserAgent, APIKeyPrefix: meta.APIKeyPrefix,
		ErrorPhase: "request", ErrorType: "cyber_policy_session_blocked", Severity: "P3",
		StatusCode: http.StatusForbidden, IsBusinessLimited: true,
		ErrorMessage: "cyber_policy_session_blocked: request rejected locally by session block",
		ErrorSource:  "gateway_local", ErrorOwner: "platform", CreatedAt: meta.CreatedAt,
	}
	if meta.SessionBlockKey != "" {
		entry.ErrorBody = "session_block_key=" + meta.SessionBlockKey
	}
	setCyberPolicyOpsIdentity(entry, meta, false)
	return entry
}

func setCyberPolicyOpsIdentity(entry *service.OpsInsertErrorLogInput, meta cyberPolicyOpsErrorMeta, includeAccount bool) {
	if meta.UserID > 0 {
		entry.UserID = &meta.UserID
	}
	if meta.APIKeyID > 0 {
		entry.APIKeyID = &meta.APIKeyID
	}
	if includeAccount && meta.AccountID > 0 {
		entry.AccountID = &meta.AccountID
	}
	entry.GroupID = meta.GroupID
	if meta.ClientIP != "" {
		entry.ClientIP = &meta.ClientIP
	}
}

func (h *OpenAIGatewayHandler) enqueueCyberSessionBlockedOpsEntry(c *gin.Context, apiKey *service.APIKey, model, sessionBlockKey string) {
	if h == nil || h.opsService == nil || c == nil || apiKey == nil {
		return
	}
	meta := captureCyberPolicyOpsMeta(c, apiKey, nil, model)
	meta.SessionBlockKey = sessionBlockKey
	enqueueOpsErrorLog(h.opsService, buildCyberSessionBlockedOpsEntry(meta))
}

func (h *OpenAIGatewayHandler) recordCyberPolicyIfMarked(c *gin.Context, apiKey *service.APIKey, account *service.Account, subscription *service.UserSubscription, model string, forwardErrored bool, cyberBlockKey string, channelFields service.ChannelUsageFields, requestPayloadHash string) {
	if h == nil || c == nil {
		return
	}
	mark := service.GetOpsCyberPolicy(c)
	if mark == nil || c.GetBool(cyberPolicyRecordedKey) {
		return
	}
	c.Set(cyberPolicyRecordedKey, true)

	meta := captureCyberPolicyOpsMeta(c, apiKey, account, model)
	var userEmail, apiKeyName, groupName string
	if apiKey != nil {
		apiKeyName = apiKey.Name
		if apiKey.User != nil {
			userEmail = apiKey.User.Email
		}
		if apiKey.Group != nil {
			groupName = apiKey.Group.Name
		}
	}
	upstreamEndpoint := ""
	if account != nil {
		upstreamEndpoint = resolveOpenAIUpstreamEndpoint(c, account)
	}
	cmSvc, gwSvc, opsSvc, apiKeySvc := h.contentModerationService, h.gatewayService, h.opsService, h.apiKeyService

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cmSvc != nil {
			cmSvc.RecordCyberPolicyEvent(ctx, service.CyberPolicyRecordInput{
				RequestID: meta.RequestID, UserID: meta.UserID, UserEmail: userEmail,
				APIKeyID: meta.APIKeyID, APIKeyName: apiKeyName, GroupID: meta.GroupID,
				GroupName: groupName, Endpoint: meta.InboundEndpoint, Model: model,
				UpstreamMessage: mark.Message, UpstreamBody: mark.Body,
				UpstreamStatus: mark.UpstreamStatus, UpstreamInTok: mark.UpstreamInTok,
				UpstreamOutTok: mark.UpstreamOutTok,
			})
		}
		if forwardErrored && gwSvc != nil {
			gwSvc.RecordCyberPolicyUsageLog(ctx, service.CyberPolicyUsageInput{
				APIKey: apiKey, Account: account, Subscription: subscription,
				RequestID: meta.RequestID, Model: model, Stream: meta.Stream,
				InputTokens: mark.UpstreamInTok, OutputTokens: mark.UpstreamOutTok,
				InboundEndpoint: meta.InboundEndpoint, UpstreamEndpoint: upstreamEndpoint,
				UserAgent: meta.UserAgent, IPAddress: meta.ClientIP,
				RequestPayloadHash: requestPayloadHash, APIKeyService: apiKeySvc,
				ChannelUsageFields: channelFields,
			})
		}
		if gwSvc != nil && cyberBlockKey != "" {
			gwSvc.MarkCyberSessionBlocked(ctx, cyberBlockKey)
		}
		if opsSvc != nil {
			enqueueOpsErrorLog(opsSvc, buildCyberPolicyOpsErrorEntry(meta, mark))
		}
	}()
}

func captureCyberPolicyOpsMeta(c *gin.Context, apiKey *service.APIKey, account *service.Account, model string) cyberPolicyOpsErrorMeta {
	meta := cyberPolicyOpsErrorMeta{Model: model, InboundEndpoint: GetInboundEndpoint(c), CreatedAt: time.Now()}
	meta.RequestID = c.Writer.Header().Get("X-Request-Id")
	if value, ok := c.Get(opsStreamKey); ok {
		meta.Stream, _ = value.(bool)
	}
	if c.Request != nil {
		meta.ClientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		meta.UserAgent = c.GetHeader("User-Agent")
		meta.ClientIP = strings.TrimSpace(ip.GetClientIP(c))
		if c.Request.URL != nil {
			meta.RequestPath = c.Request.URL.Path
		}
	}
	meta.Platform = resolveOpsPlatform(apiKey, guessPlatformFromPath(meta.RequestPath))
	if apiKey != nil {
		meta.APIKeyID, meta.GroupID = apiKey.ID, apiKey.GroupID
		meta.APIKeyPrefix = keyPrefix(apiKey.Key, 8)
		meta.UserID = apiKey.UserID
		if apiKey.User != nil {
			meta.UserID = apiKey.User.ID
		}
	}
	if account != nil {
		meta.AccountID = account.ID
	}
	return meta
}

func clearCyberPolicyTurnState(c *gin.Context) {
	if c == nil {
		return
	}
	service.ClearOpsCyberPolicy(c)
	c.Set(cyberPolicyRecordedKey, false)
}
