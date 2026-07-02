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
	anthropicTransportErrorTempUnschedDuration = 10 * time.Minute
	anthropicTransportErrorStateUpdateTimeout  = 3 * time.Second
)

var anthropicTransportFailoverBody = []byte(`{"type":"error","error":{"type":"upstream_error","message":"Upstream request failed"}}`)

type anthropicTransportErrorClass struct {
	Persistent bool
}

var anthropicPersistentTransportErrorMarkers = []string{
	"authentication failed",
	"proxy authentication required",
	"connection refused",
	"no route to host",
	"network is unreachable",
	"no such host",
	"malformed proxy",
	"unsupported protocol scheme",
	"tls: failed to verify certificate",
}

func classifyAnthropicTransportError(err error) anthropicTransportErrorClass {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return anthropicTransportErrorClass{}
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return anthropicTransportErrorClass{Persistent: true}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return anthropicTransportErrorClass{Persistent: true}
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range anthropicPersistentTransportErrorMarkers {
		if strings.Contains(msg, marker) {
			return anthropicTransportErrorClass{Persistent: true}
		}
	}
	return anthropicTransportErrorClass{}
}

func (s *GatewayService) handleAnthropicUpstreamTransportError(ctx context.Context, c *gin.Context, account *Account, err error, upstreamURL string, passthrough bool) error {
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: 0,
		UpstreamURL:        strings.TrimSpace(upstreamURL),
		Passthrough:        passthrough,
		Kind:               "request_error",
		Message:            safeErr,
	})

	if errors.Is(err, context.Canceled) {
		return err
	}
	s.maybeTempUnscheduleAnthropicTransportError(ctx, account, err, safeErr)
	return &UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: anthropicTransportFailoverBody,
	}
}

func (s *GatewayService) maybeTempUnscheduleAnthropicTransportError(ctx context.Context, account *Account, err error, safeErr string) {
	if s == nil || account == nil || s.accountRepo == nil {
		return
	}
	if !account.IsAnthropicOAuthOrSetupToken() {
		return
	}
	if !classifyAnthropicTransportError(err).Persistent {
		return
	}

	until := time.Now().Add(anthropicTransportErrorTempUnschedDuration)
	reason := "upstream transport error (proxy/network): " + strings.TrimSpace(safeErr)

	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), anthropicTransportErrorStateUpdateTimeout)
	defer cancel()
	if setErr := s.accountRepo.SetTempUnschedulable(bgCtx, account.ID, until, reason); setErr != nil {
		logger.L().With(zap.String("component", "service.gateway")).Warn(
			"anthropic.account_temp_unscheduled_transport_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(setErr),
		)
		return
	}

	account.TempUnschedulableUntil = &until
	account.TempUnschedulableReason = reason
	logger.L().With(zap.String("component", "service.gateway")).Warn(
		"anthropic.account_temp_unscheduled_transport",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("platform", account.Platform),
		zap.String("type", account.Type),
		zap.Time("until", until),
		zap.String("reason", reason),
	)
}
