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
	geminiTransportErrorTempUnschedDuration = 10 * time.Minute
	geminiTransportErrorStateUpdateTimeout  = 3 * time.Second
)

var geminiTransportFailoverBody = []byte(`{"error":{"code":502,"message":"Upstream request failed","status":"BAD_GATEWAY"}}`)

type geminiTransportErrorClass struct {
	Persistent bool
}

var geminiPersistentTransportErrorMarkers = []string{
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

func classifyGeminiTransportError(err error) geminiTransportErrorClass {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return geminiTransportErrorClass{}
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return geminiTransportErrorClass{Persistent: true}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return geminiTransportErrorClass{Persistent: true}
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range geminiPersistentTransportErrorMarkers {
		if strings.Contains(msg, marker) {
			return geminiTransportErrorClass{Persistent: true}
		}
	}
	return geminiTransportErrorClass{}
}

func (s *GeminiMessagesCompatService) handleGeminiUpstreamTransportError(ctx context.Context, c *gin.Context, account *Account, err error) error {
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: 0,
		Kind:               "request_error",
		Message:            safeErr,
	})

	if errors.Is(err, context.Canceled) {
		return err
	}
	if classifyGeminiTransportError(err).Persistent {
		s.tempUnscheduleGeminiTransportError(ctx, account, safeErr)
	}
	return &UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: geminiTransportFailoverBody,
	}
}

func (s *GeminiMessagesCompatService) tempUnscheduleGeminiTransportError(ctx context.Context, account *Account, safeErr string) {
	if s == nil || account == nil || s.accountRepo == nil {
		return
	}
	if account.Platform != PlatformGemini && account.Platform != PlatformAntigravity {
		return
	}

	until := time.Now().Add(geminiTransportErrorTempUnschedDuration)
	reason := "upstream transport error (proxy/network): " + strings.TrimSpace(safeErr)

	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), geminiTransportErrorStateUpdateTimeout)
	defer cancel()
	if err := s.accountRepo.SetTempUnschedulable(bgCtx, account.ID, until, reason); err != nil {
		logger.L().With(zap.String("component", "service.gemini_messages_compat")).Warn(
			"gemini.account_temp_unscheduled_transport_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
		return
	}

	account.TempUnschedulableUntil = &until
	account.TempUnschedulableReason = reason
	logger.L().With(zap.String("component", "service.gemini_messages_compat")).Warn(
		"gemini.account_temp_unscheduled_transport",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("platform", account.Platform),
		zap.String("type", account.Type),
		zap.Time("until", until),
		zap.String("reason", reason),
	)
}
