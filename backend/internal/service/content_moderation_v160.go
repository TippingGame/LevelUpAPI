package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *ContentModerationService) enqueueRecord(input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) {
	if s == nil || s.asyncQueue == nil || log == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action,
			"queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	task := contentModerationTask{
		input:            input,
		inputHash:        inputHash,
		log:              log,
		config:           cloneContentModerationConfig(cfg),
		recordHash:       recordHash,
		applySideEffects: applySideEffects,
		enqueuedAt:       time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) callModerationOnceWithInput(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out moderationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return &out.Results[0], nil
}

func (s *ContentModerationService) applyFlaggedAccountSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) bool {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return false
	}
	count := 1
	if s.repo != nil && cfg.ViolationWindowHours > 0 {
		since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
		if n, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {
			count = n + 1
		}
	}
	log.ViolationCount = count
	autoBanJustApplied := false
	if cfg.AutoBanEnabled && cfg.BanThreshold > 0 && count >= cfg.BanThreshold && s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, *log.UserID)
		if err != nil {
			slog.Warn("content_moderation.ban_get_user_failed", "user_id", *log.UserID, "error", err)
			return false
		}
		if user.IsAdmin() {
			slog.Warn("content_moderation.autoban_skipped_admin", "user_id", *log.UserID, "role", user.Role, "count", count, "threshold", cfg.BanThreshold)
			// TODO: Disable the triggering API key instead when API key mutation is available here.
			return false
		}
		if user.Status != StatusDisabled {
			user.Status = StatusDisabled
			if err := s.userRepo.Update(ctx, user); err != nil {
				slog.Warn("content_moderation.ban_update_user_failed", "user_id", *log.UserID, "error", err)
				return false
			}
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
			}
			autoBanJustApplied = true
		}
		log.AutoBanned = true
	}
	return autoBanJustApplied
}

func (s *ContentModerationService) sendFlaggedNotificationSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, autoBanJustApplied bool) {
	if s == nil || cfg == nil || log == nil || !log.Flagged {
		return
	}
	if s.emailService == nil || strings.TrimSpace(log.UserEmail) == "" {
		return
	}
	emailSent := false
	if cfg.EmailOnHit {
		if err := s.sendViolationEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	if autoBanJustApplied {
		if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.ban_email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	log.EmailSent = emailSent
}

func contentModerationEmailSourceID(log *ContentModerationLog) string {
	if log == nil || log.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", log.ID)
}

func contentModerationEmailVariables(log *ContentModerationLog, cfg *ContentModerationConfig) map[string]string {
	variables := map[string]string{
		"triggered_at":        time.Now().UTC().Format(time.RFC3339),
		"group_name":          "-",
		"moderation_category": "-",
		"moderation_score":    "0.000",
		"violation_count":     "0",
		"ban_threshold":       "0",
	}
	if log != nil {
		if !log.CreatedAt.IsZero() {
			variables["triggered_at"] = log.CreatedAt.UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(log.GroupName) != "" {
			variables["group_name"] = strings.TrimSpace(log.GroupName)
		}
		if strings.TrimSpace(log.HighestCategory) != "" {
			variables["moderation_category"] = strings.TrimSpace(log.HighestCategory)
		}
		variables["moderation_score"] = fmt.Sprintf("%.3f", log.HighestScore)
		variables["violation_count"] = fmt.Sprintf("%d", log.ViolationCount)
	}
	if cfg != nil {
		variables["ban_threshold"] = fmt.Sprintf("%d", cfg.BanThreshold)
	}
	return variables
}

func cloneModerationInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

// CyberPolicyRecordInput 是一次 cyber_policy 硬阻断的风控记录入参。
type CyberPolicyRecordInput struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	UpstreamMessage string
	UpstreamBody    string
	UpstreamStatus  int
	UpstreamInTok   int
	UpstreamOutTok  int
}

// RecordCyberPolicyEvent 把一次 cyber_policy 硬阻断写入风控中心日志、计入违规计数、
// 并给用户发邮件。当前请求已由 gateway 透传给用户；本方法仅做事后记录/通知/计数。
// 仅受 risk_control_enabled 总开关约束（不受内容审核 Enabled/Mode/scope/sample 约束）。
func (s *ContentModerationService) RecordCyberPolicyEvent(ctx context.Context, in CyberPolicyRecordInput) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.isRiskControlEnabled(ctx) {
		return
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cyber_load_config_failed", "error", err)
		cfg = &ContentModerationConfig{}
	}
	var userID *int64
	if in.UserID > 0 {
		userID = &in.UserID
	}
	var apiKeyID *int64
	if in.APIKeyID > 0 {
		apiKeyID = &in.APIKeyID
	}
	errBody := strings.TrimSpace(in.UpstreamMessage)
	if b := strings.TrimSpace(in.UpstreamBody); b != "" {
		// 原始 body 不在此预脱敏；写入 log.Error 前由 redactContentModerationSecrets 统一脱敏。
		errBody = strings.TrimSpace(errBody + "\n" + b)
	}
	if in.UpstreamInTok > 0 || in.UpstreamOutTok > 0 {
		errBody = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errBody, in.UpstreamInTok, in.UpstreamOutTok)
	}
	log := &ContentModerationLog{
		RequestID:       in.RequestID,
		UserID:          userID,
		UserEmail:       in.UserEmail,
		APIKeyID:        apiKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneModerationInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Provider:        "openai",
		Model:           in.Model,
		Mode:            "post_upstream",
		Action:          ContentModerationActionCyberPolicy,
		Flagged:         true,
		HighestCategory: "cyber_policy",
		HighestScore:    1.0,
		Error:           trimRunes(redactContentModerationSecrets(errBody), maxModerationExcerptRunes*4),
		CreatedAt:       time.Now(),
	}
	// 开关开时 cyber_policy 不参与封号计数：当次不判定（此处跳过），
	// 历史行由 CountFlaggedByUserSince 的 excludeCyberPolicy 排除。
	autoBanned := false
	if !cfg.CyberPolicyExcludeFromBanCount {
		autoBanned = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
	}
	log.EmailSent = false
	logPersisted := true
	if err := s.repo.CreateLog(ctx, log); err != nil {
		logPersisted = false
		slog.Warn("content_moderation.cyber_create_log_failed", "user_id", in.UserID, "error", err)
	}
	emailSent := false
	if s.emailService != nil && strings.TrimSpace(log.UserEmail) != "" {
		if err := s.sendCyberPolicyEmail(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_email_failed", "user_id", in.UserID, "error", err)
		} else {
			emailSent = true
		}
		if autoBanned {
			if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
				slog.Warn("content_moderation.cyber_ban_email_failed", "user_id", in.UserID, "error", err)
			} else {
				emailSent = true
			}
		}
	}
	if logPersisted && emailSent {
		updater, ok := s.repo.(interface {
			UpdateLogEmailSent(context.Context, int64, bool) error
		})
		if ok {
			if err := updater.UpdateLogEmailSent(ctx, log.ID, true); err != nil {
				slog.Warn("content_moderation.cyber_update_email_sent_failed", "log_id", log.ID, "error", err)
			}
		} else {
			slog.Warn("content_moderation.cyber_update_email_sent_unsupported", "log_id", log.ID)
		}
	}
}

func (s *ContentModerationService) sendCyberPolicyEmail(ctx context.Context, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		variables := map[string]string{
			"triggered_at":     log.CreatedAt.UTC().Format(time.RFC3339),
			"model":            defaultContentModerationString(log.Model, "-"),
			"group_name":       defaultContentModerationString(log.GroupName, "-"),
			"upstream_message": defaultContentModerationString(log.Error, "-"),
		}
		err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventCyberPolicyNotice,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      variables,
		})
		if err == nil {
			return nil
		}
		if !shouldFallbackNotificationEmail(err) {
			return err
		}
		slog.Warn("template cyber policy email failed; falling back", "err", err.Error())
	}
	subject := fmt.Sprintf("[%s] 网络安全策略拦截 / Cyber Policy Notice", sanitizeEmailHeader(siteName))
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, buildCyberPolicyNoticeEmailBody(siteName, log))
}

func buildCyberPolicyNoticeEmailBody(siteName string, log *ContentModerationLog) string {
	if log == nil {
		return ""
	}
	userName := strings.TrimSpace(log.UserEmail)
	if userName == "" && log.UserID != nil {
		userName = fmt.Sprintf("UID %d", *log.UserID)
	}
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:#f5f6fb;color:#222;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif;">
  <div style="max-width:680px;margin:0 auto;padding:32px 20px;">
    <div style="height:8px;background:#ef4444;border-radius:14px 14px 0 0;"></div>
    <div style="background:#fff;border-radius:0 0 14px 14px;padding:40px 48px;box-shadow:0 8px 28px rgba(15,23,42,.08);">
      <div style="letter-spacing:4px;color:#999;font-size:14px;text-transform:uppercase;">Risk Control / 网络安全策略</div>
      <h1 style="margin:20px 0 28px;font-size:30px;line-height:1.25;">请求被网络安全策略拦截</h1>
      <p style="font-size:17px;line-height:1.9;margin:0 0 24px;">尊敬的用户 <strong>%s</strong>，您的请求被上游网络安全策略（cyber policy）拦截。</p>
      <div style="background:#fff1f2;border:1px solid #fecdd3;border-radius:12px;padding:22px 28px;margin:28px 0;">
        <table style="width:100%%;border-collapse:collapse;font-size:16px;">
          <tr><td style="padding:12px 0;color:#888;border-bottom:1px solid #fee2e2;">触发时间</td><td style="padding:12px 0;border-bottom:1px solid #fee2e2;">%s</td></tr>
          <tr><td style="padding:12px 0;color:#888;border-bottom:1px solid #fee2e2;">模型</td><td style="padding:12px 0;border-bottom:1px solid #fee2e2;">%s</td></tr>
          <tr><td style="padding:12px 0;color:#888;">上游说明</td><td style="padding:12px 0;">%s</td></tr>
        </table>
      </div>
      <p style="font-size:15px;line-height:1.8;color:#666;">如认为系误判，可调整请求措辞后重试，或申请获得授权的安全访问权限。</p>
      <p style="font-size:14px;line-height:1.8;color:#777;margin-top:28px;">此邮件由 %s 自动发送，请勿回复。</p>
    </div>
  </div>
</body></html>`,
		html.EscapeString(userName),
		html.EscapeString(log.CreatedAt.Format("2006-01-02 15:04:05")),
		html.EscapeString(defaultContentModerationString(log.Model, "-")),
		html.EscapeString(defaultContentModerationString(log.Error, "-")),
		html.EscapeString(siteName),
	)
}
