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

// Embeddings handles the OpenAI-compatible Embeddings API.
// POST /v1/embeddings
func (h *OpenAIGatewayHandler) Embeddings(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.embeddings",
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
	reqModel := modelResult.String()
	reqLog = reqLog.With(zap.String("model", reqModel))

	setOpsRequestContext(c, reqModel, false, body)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	maxAccountSwitches := h.maxAccountSwitches
	routeCursor := newAPIKeyGroupRouteCursor(apiKey)
	if _, ok := routeCursor.current(); !ok {
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
		return
	}

routeLoop:
	for {
		routeCandidate, ok := routeCursor.current()
		if !ok {
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available API key group routes", streamStarted)
			return
		}
		currentAPIKey := routeCandidate.APIKey
		currentSubscription, subErr := h.gatewayService.ResolveRouteSubscription(c.Request.Context(), currentAPIKey, subscription)
		if subErr != nil {
			status, code, message, retryAfter := billingErrorDetails(subErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}
		channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), currentAPIKey.GroupID, reqModel)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), currentAPIKey.User, currentAPIKey, currentAPIKey.Group, currentSubscription); err != nil {
			reqLog.Info("openai_embeddings.billing_eligibility_check_failed",
				zap.Error(err),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}

		switchCount := 0
		failedAccountIDs := make(map[int64]struct{})
		sameAccountRetryCount := make(map[int64]int)
		var lastFailoverErr *service.UpstreamFailoverError

		for {
			reqLog.Debug("openai_embeddings.account_selecting",
				zap.Int("excluded_account_count", len(failedAccountIDs)),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			selectionModel := resolveOpenAIAccountSelectionModel(reqModel, channelMapping)
			selectionCtx := openAIAccountShareModeRequestContext(c, currentAPIKey)
			selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
				selectionCtx,
				currentAPIKey.GroupID,
				"",
				"",
				selectionModel,
				failedAccountIDs,
				service.OpenAIUpstreamTransportHTTPSSE,
				service.OpenAIEndpointCapabilityEmbeddings,
				false,
			)
			if err != nil {
				reqLog.Warn("openai_embeddings.account_select_failed",
					zap.Error(err),
					zap.Int("excluded_account_count", len(failedAccountIDs)),
					zap.Int64p("group_id", currentAPIKey.GroupID),
				)
				if len(failedAccountIDs) == 0 {
					if h.handleAccountShareModeSelectionError(c, err, streamStarted) {
						return
					}
					if routeCursor.switchToNext(apiKey.ID, "account_select_failed", reqLog, zap.Error(err)) {
						continue routeLoop
					}
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available compatible accounts", streamStarted)
					return
				}
				if lastFailoverErr != nil {
					if shouldSwitchAPIKeyGroupRoute(lastFailoverErr) &&
						routeCursor.switchToNext(apiKey.ID, "account_selection_exhausted", reqLog, zap.Int("upstream_status", lastFailoverErr.StatusCode)) {
						continue routeLoop
					}
					h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.handleFailoverExhaustedSimple(c, 502, streamStarted)
				}
				return
			}
			if selection == nil || selection.Account == nil {
				h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available compatible accounts", streamStarted)
				return
			}

			reqLog.Debug("openai_embeddings.account_schedule_decision",
				zap.String("layer", scheduleDecision.Layer),
				zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
				zap.Int("candidate_count", scheduleDecision.CandidateCount),
				zap.Int("top_k", scheduleDecision.TopK),
				zap.Int64("latency_ms", scheduleDecision.LatencyMs),
				zap.Float64("load_skew", scheduleDecision.LoadSkew),
			)

			account := selection.Account
			reqLog.Debug("openai_embeddings.account_selected",
				zap.Int64("account_id", account.ID),
				zap.String("account_name", account.Name),
				zap.Int64p("group_id", currentAPIKey.GroupID),
			)
			setOpsSelectedAccount(c, account.ID, account.Platform)

			accountReleaseFunc, acquired := h.acquireResponsesAccountSlot(c, currentAPIKey.GroupID, "", selection, false, &streamStarted, reqLog)
			if !acquired {
				return
			}

			service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
			forwardStart := time.Now()
			writerSizeBeforeForward := c.Writer.Size()
			result, err := h.gatewayService.ForwardEmbeddings(c.Request.Context(), c, account, body, channelMapping.MappedModel)
			forwardDurationMs := time.Since(forwardStart).Milliseconds()
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
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
						reqLog.Warn("openai_embeddings.upstream_failover_skipped_after_flush",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai_embeddings.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(sameAccountRetryDelay):
							}
							continue
						}
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					failedAccountIDs[account.ID] = struct{}{}
					lastFailoverErr = failoverErr
					if switchCount >= maxAccountSwitches {
						if canSwitchAPIKeyGroupRouteAfterForward(c, routeCursor, failoverErr, streamStarted, writerSizeBeforeForward) &&
							routeCursor.switchToNext(apiKey.ID, "upstream_failover_exhausted", reqLog, zap.Int("upstream_status", failoverErr.StatusCode)) {
							continue routeLoop
						}
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					reqLog.Warn("openai_embeddings.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				wroteFallback := h.ensureForwardErrorResponse(c, streamStarted)
				reqLog.Warn("openai_embeddings.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				)
				return
			}

			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
			routeCursor.recordSuccess(apiKey.ID)

			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				usageCtx := service.WithAccountShareModeRequestFromContext(ctx, selectionCtx)
				if err := h.gatewayService.RecordUsage(usageCtx, &service.OpenAIRecordUsageInput{
					Result:             result,
					APIKey:             currentAPIKey,
					User:               currentAPIKey.User,
					Account:            account,
					Subscription:       currentSubscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					RequestPayloadHash: requestPayloadHash,
					APIKeyService:      h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.embeddings"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", currentAPIKey.ID),
						zap.Any("group_id", currentAPIKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("openai_embeddings.record_usage_failed", zap.Error(err))
				}
			})
			reqLog.Debug("openai_embeddings.request_completed",
				zap.Int64("account_id", account.ID),
				zap.Int("switch_count", switchCount),
			)
			return
		}
	}
}
