package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// AlphaSearch proxies the standalone search endpoint used by Codex Responses Lite.
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	setOpenAIClientTransportHTTP(c)
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if !apiKeyHasConfiguredOpenAIGroup(apiKey) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI groups")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.alpha_search",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	requestedModel := strings.TrimSpace(modelResult.String())
	reqLog = reqLog.With(zap.String("model", requestedModel))
	setOpsRequestContext(c, requestedModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_alpha_search", requestedModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}

	searchID := strings.TrimSpace(gjson.GetBytes(body, "id").String())
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, nil, searchID)
	routeCursor := newOpenAIAlphaSearchRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
		return
	}

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError

routeLoop:
	for {
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes")
			return
		}
		currentAPIKey := routeCandidate.APIKey
		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if subErr != nil {
			if shouldSkipRouteOnSubscriptionResolveError(subErr) &&
				routeCursor.skipToNext("route_subscription_resolve_failed", reqLog, zap.Error(subErr), zap.Int64p("group_id", currentAPIKey.GroupID)) {
				continue routeLoop
			}
			status, code, message, retryAfter := billingErrorDetails(subErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}

		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, requestedModel)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("openai_alpha_search.billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}

		selectionModel := resolveOpenAIAccountSelectionModel(requestedModel, channelMapping)
		selection, scheduleDecision, selectErr := h.gatewayService.SelectAccountWithCleanRelayScheduler(
			c.Request.Context(),
			c,
			currentAPIKey.GroupID,
			"",
			sessionHash,
			requestedModel,
			selectionModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			false,
			body,
			service.PlatformOpenAI,
		)
		if selectErr != nil {
			reqLog.Warn("openai_alpha_search.account_select_failed",
				zap.Error(selectErr),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			if len(failedAccountIDs) == 0 {
				if routeCursor.switchToNextWithoutCooldown(apiKey.ID, "account_select_failed", reqLog, zap.Error(selectErr)) {
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					continue routeLoop
				}
				h.errorResponse(c, http.StatusServiceUnavailable, "api_error", openAIAccountSelectionUnavailableMessage(selectErr))
				return
			}
			if lastFailoverErr != nil {
				if shouldSwitchAPIKeyGroupRoute(lastFailoverErr) &&
					routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", lastFailoverErr.StatusCode)) {
					failedAccountIDs = make(map[int64]struct{})
					sameAccountRetryCount = make(map[int64]int)
					switchCount = 0
					lastFailoverErr = nil
					continue routeLoop
				}
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			return
		}
		if selection == nil || selection.Account == nil {
			if routeCursor.switchToNextWithoutCooldown(apiKey.ID, "account_selection_empty", reqLog, zap.Int64p("group_id", currentAPIKey.GroupID)) {
				failedAccountIDs = make(map[int64]struct{})
				sameAccountRetryCount = make(map[int64]int)
				switchCount = 0
				lastFailoverErr = nil
				continue routeLoop
			}
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available compatible accounts")
			return
		}

		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		_ = scheduleDecision
		setOpsSelectedAccount(c, account.ID, account.Platform)
		accountRelease, acquired := h.acquireResponsesAccountSlot(c, currentAPIKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if !acquired {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		writerSizeBeforeForward := c.Writer.Size()
		forwardStart := time.Now()
		forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
		result, err := func() (*service.OpenAIForwardResult, error) {
			if accountRelease != nil {
				defer accountRelease()
			}
			return h.gatewayService.ForwardAlphaSearch(c.Request.Context(), c, account, forwardBody)
		}()
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				switch handleOpenAISameAccountRetry(
					c.Request.Context(),
					h.gatewayService,
					account.ID,
					account.GetPoolModeRetryCount(),
					failoverErr,
					sameAccountRetryCount,
					sameAccountRetryDelay,
					reqLog,
					"openai_alpha_search.pool_mode_same_account_retry",
				) {
				case openAISameAccountRetryContinue:
					continue
				case openAISameAccountRetryCanceled:
					return
				}
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				h.clearStickySessionIfBoundTo(c.Request.Context(), currentAPIKey.GroupID, sessionHash, account.ID, reqLog, "upstream_failover")
				h.clearCleanRelayMappingIfBoundTo(c.Request.Context(), c, body, account.ID, reqLog, "upstream_failover")
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, failoverErr, false, writerSizeBeforeForward) &&
						routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", failoverErr.StatusCode)) {
						failedAccountIDs = make(map[int64]struct{})
						sameAccountRetryCount = make(map[int64]int)
						switchCount = 0
						lastFailoverErr = nil
						continue routeLoop
					}
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				reqLog.Warn("openai_alpha_search.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
			wroteFallback := h.ensureForwardErrorResponse(c, false)
			reqLog.Warn("openai_alpha_search.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
		routeCursor.recordSuccess(apiKey.ID)
		if result != nil {
			h.recordAlphaSearchUsage(c, currentAPIKey, account, currentSubscription, channelMapping, requestedModel, body, result, subject.UserID)
		}
		return
	}
}

func apiKeyHasConfiguredOpenAIGroup(apiKey *service.APIKey) bool {
	if apiKey == nil {
		return false
	}
	if len(apiKey.GroupRoutes) > 0 {
		for _, route := range apiKey.GroupRoutes {
			if route.Group != nil && route.Group.Platform == service.PlatformOpenAI {
				return true
			}
		}
		return false
	}
	return apiKey.Group != nil && apiKey.Group.Platform == service.PlatformOpenAI
}

func newOpenAIAlphaSearchRouteCursor(apiKey *service.APIKey) *apiKeyGroupRouteCursor {
	base := newAPIKeyGroupRouteCursor(apiKey)
	if base == nil {
		return newAPIKeyGroupRouteCursorFromCandidates(nil, false)
	}
	candidates := make([]apiKeyGroupRouteCandidate, 0, len(base.candidates))
	for _, candidate := range base.candidates {
		if candidate.APIKey == nil || candidate.APIKey.Group == nil || candidate.APIKey.Group.Platform != service.PlatformOpenAI {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return newAPIKeyGroupRouteCursorFromCandidates(candidates, len(candidates) > 0)
}

func (h *OpenAIGatewayHandler) recordAlphaSearchUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	account *service.Account,
	subscription *service.UserSubscription,
	channelMapping service.ChannelMappingResult,
	requestedModel string,
	body []byte,
	result *service.OpenAIForwardResult,
	userID int64,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

	h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: channelMapping.ToUsageFields(requestedModel, result.UpstreamModel),
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.alpha_search"),
				zap.Int64("user_id", userID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestedModel),
				zap.Int64("account_id", account.ID),
			).Error("openai_alpha_search.record_usage_failed", zap.Error(err))
		}
	})
}
