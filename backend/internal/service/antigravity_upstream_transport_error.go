package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	antigravityTransportErrorTempUnschedDuration = 10 * time.Minute
	antigravityTransportErrorStateUpdateTimeout  = 3 * time.Second
)

var antigravityTransportFailoverBody = []byte(`{"error":{"code":502,"message":"Upstream request failed","status":"BAD_GATEWAY"}}`)

type antigravityTransportErrorClass struct {
	Persistent bool
}

var antigravityPersistentTransportErrorMarkers = []string{
	"authentication failed",
	"proxy authentication required",
	"connection refused",
	"no route to host",
	"network is unreachable",
	"no such host",
	"malformed proxy",
	"unsupported protocol scheme",
	"tls: failed to verify certificate",
	"x509: certificate signed by unknown authority",
}

func classifyAntigravityTransportError(err error) antigravityTransportErrorClass {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return antigravityTransportErrorClass{}
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return antigravityTransportErrorClass{Persistent: true}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return antigravityTransportErrorClass{Persistent: true}
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range antigravityPersistentTransportErrorMarkers {
		if strings.Contains(msg, marker) {
			return antigravityTransportErrorClass{Persistent: true}
		}
	}
	return antigravityTransportErrorClass{}
}

func (s *AntigravityGatewayService) handleAntigravityUpstreamTransportError(ctx context.Context, c *gin.Context, account *Account, err error, upstreamURL string) error {
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: 0,
		UpstreamURL:        strings.TrimSpace(upstreamURL),
		Kind:               "request_error",
		Message:            safeErr,
	})

	if errors.Is(err, context.Canceled) {
		return err
	}
	if classifyAntigravityTransportError(err).Persistent {
		s.tempUnscheduleAntigravityTransportError(ctx, account, safeErr)
	}
	return &UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: antigravityTransportFailoverBody,
	}
}

func (s *AntigravityGatewayService) tempUnscheduleAntigravityTransportError(ctx context.Context, account *Account, safeErr string) {
	if s == nil || account == nil || s.accountRepo == nil {
		return
	}
	if account.Platform != PlatformAntigravity {
		return
	}

	until := time.Now().Add(antigravityTransportErrorTempUnschedDuration)
	reason := "upstream transport error (proxy/network): " + strings.TrimSpace(safeErr)
	var cache TempUnschedCache
	if s.rateLimitService != nil {
		cache = s.rateLimitService.tempUnschedCache
	}
	state := newTempUnschedState(until, 0, "antigravity_transport_error", reason)

	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), antigravityTransportErrorStateUpdateTimeout)
	defer cancel()
	if err := s.accountRepo.SetTempUnschedulable(bgCtx, account.ID, until, reason); err != nil {
		logger.L().With(zap.String("component", "service.antigravity_gateway")).Warn(
			"antigravity.account_temp_unscheduled_transport_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
		markTempUnschedRuntimeState(bgCtx, cache, account, until, reason, state, "antigravity_transport_error_runtime_fallback")
		return
	}

	markTempUnschedRuntimeState(bgCtx, cache, account, until, reason, state, "antigravity_transport_error")

	logger.L().With(zap.String("component", "service.antigravity_gateway")).Warn(
		"antigravity.account_temp_unscheduled_transport",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("platform", account.Platform),
		zap.String("type", account.Type),
		zap.Time("until", until),
		zap.String("reason", reason),
	)
}

func antigravityRetryErrorToFailover(err error) (*UpstreamFailoverError, bool) {
	if switchErr, ok := IsAntigravityAccountSwitchError(err); ok {
		return &UpstreamFailoverError{
			StatusCode:        http.StatusServiceUnavailable,
			ForceCacheBilling: switchErr.IsStickySession,
		}, true
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		return failoverErr, true
	}
	return nil, false
}
