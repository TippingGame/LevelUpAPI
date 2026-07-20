package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AdminComplianceVersion        = "v2026.06.10"
	AdminComplianceDocumentPathZH = "docs/legal/admin-compliance.zh.md"
	AdminComplianceDocumentPathEN = "docs/legal/admin-compliance.en.md"
	AdminComplianceDocumentURLZH  = "https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.zh.md"
	AdminComplianceDocumentURLEN  = "https://github.com/Wei-Shaw/sub2api/blob/main/docs/legal/admin-compliance.en.md"
	AdminComplianceAckPhraseZH    = "我已阅读、理解并同意 Sub2API 部署与运营合规承诺"
	AdminComplianceAckPhraseEN    = "I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment"

	settingKeyAdminComplianceAcknowledgement = "admin_compliance_acknowledgement"
	adminComplianceCacheTTL                  = 5 * time.Minute
	adminComplianceDBTimeout                 = 2 * time.Second
)

type cachedAdminComplianceAcknowledgement struct {
	ack       AdminComplianceAcknowledgement
	expiresAt int64
}

var (
	ErrAdminComplianceAcknowledgementRequired = infraerrors.New(
		http.StatusLocked,
		"ADMIN_COMPLIANCE_ACK_REQUIRED",
		"administrator compliance acknowledgement is required",
	)
	ErrAdminComplianceInvalidPhrase = infraerrors.BadRequest(
		"ADMIN_COMPLIANCE_INVALID_PHRASE",
		"confirmation phrase does not match",
	)
)

type AdminComplianceAcknowledgement struct {
	Version     string    `json:"version"`
	DocumentZH  string    `json:"document_zh"`
	DocumentEN  string    `json:"document_en"`
	AdminUserID int64     `json:"admin_user_id"`
	IPAddress   string    `json:"ip_address,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	AcceptedAt  time.Time `json:"accepted_at"`
}

type AdminComplianceStatus struct {
	Required        bool                            `json:"required"`
	Version         string                          `json:"version"`
	DocumentPathZH  string                          `json:"document_path_zh"`
	DocumentPathEN  string                          `json:"document_path_en"`
	DocumentURLZH   string                          `json:"document_url_zh"`
	DocumentURLEN   string                          `json:"document_url_en"`
	AckPhraseZH     string                          `json:"ack_phrase_zh"`
	AckPhraseEN     string                          `json:"ack_phrase_en"`
	Acknowledgement *AdminComplianceAcknowledgement `json:"acknowledgement,omitempty"`
}

type AdminComplianceAcceptInput struct {
	AdminUserID int64
	Phrase      string
	Language    string
	IPAddress   string
	UserAgent   string
}

func normalizeAdminComplianceLanguage(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(raw, "zh") {
		return "zh"
	}
	return "en"
}

func expectedAdminCompliancePhrase(language string) string {
	if normalizeAdminComplianceLanguage(language) == "zh" {
		return AdminComplianceAckPhraseZH
	}
	return AdminComplianceAckPhraseEN
}

func adminComplianceAcknowledgementKey(adminUserID int64) string {
	if adminUserID <= 0 {
		return settingKeyAdminComplianceAcknowledgement
	}
	return settingKeyAdminComplianceAcknowledgement + ":" + strconv.FormatInt(adminUserID, 10)
}

func (s *SettingService) GetAdminComplianceStatus(ctx context.Context, adminUserID int64) (*AdminComplianceStatus, error) {
	status := newAdminComplianceStatus()
	if s == nil || s.settingRepo == nil {
		return status, nil
	}
	if ack := s.loadAdminComplianceAcknowledgement(adminUserID); ack != nil {
		status.Required = false
		status.Acknowledgement = ack
		return status, nil
	}

	cacheKey := strconv.FormatInt(adminUserID, 10)
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	result, err, _ := s.adminComplianceSF.Do(cacheKey, func() (any, error) {
		if ack := s.loadAdminComplianceAcknowledgement(adminUserID); ack != nil {
			return ack, nil
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(baseCtx), adminComplianceDBTimeout)
		defer cancel()
		raw, err := s.settingRepo.GetValue(dbCtx, adminComplianceAcknowledgementKey(adminUserID))
		if err != nil {
			if errors.Is(err, ErrSettingNotFound) {
				return (*AdminComplianceAcknowledgement)(nil), nil
			}
			return nil, fmt.Errorf("get admin compliance acknowledgement: %w", err)
		}

		var ack AdminComplianceAcknowledgement
		if err := json.Unmarshal([]byte(raw), &ack); err != nil || ack.Version != AdminComplianceVersion {
			return (*AdminComplianceAcknowledgement)(nil), nil
		}
		s.storeAdminComplianceAcknowledgement(adminUserID, ack)
		return &ack, nil
	})
	if err != nil {
		return nil, err
	}
	ack, _ := result.(*AdminComplianceAcknowledgement)
	if ack != nil {
		copy := *ack
		status.Required = false
		status.Acknowledgement = &copy
	}
	return status, nil
}

func newAdminComplianceStatus() *AdminComplianceStatus {
	return &AdminComplianceStatus{
		Required:       true,
		Version:        AdminComplianceVersion,
		DocumentPathZH: AdminComplianceDocumentPathZH,
		DocumentPathEN: AdminComplianceDocumentPathEN,
		DocumentURLZH:  AdminComplianceDocumentURLZH,
		DocumentURLEN:  AdminComplianceDocumentURLEN,
		AckPhraseZH:    AdminComplianceAckPhraseZH,
		AckPhraseEN:    AdminComplianceAckPhraseEN,
	}
}

func (s *SettingService) loadAdminComplianceAcknowledgement(adminUserID int64) *AdminComplianceAcknowledgement {
	if s == nil {
		return nil
	}
	value, ok := s.adminComplianceCache.Load(adminUserID)
	if !ok {
		return nil
	}
	cached, _ := value.(*cachedAdminComplianceAcknowledgement)
	if cached == nil || cached.ack.Version != AdminComplianceVersion || time.Now().UnixNano() >= cached.expiresAt {
		s.adminComplianceCache.Delete(adminUserID)
		return nil
	}
	copy := cached.ack
	return &copy
}

func (s *SettingService) storeAdminComplianceAcknowledgement(adminUserID int64, ack AdminComplianceAcknowledgement) {
	if s == nil || ack.Version != AdminComplianceVersion {
		return
	}
	s.adminComplianceCache.Store(adminUserID, &cachedAdminComplianceAcknowledgement{
		ack:       ack,
		expiresAt: time.Now().Add(adminComplianceCacheTTL).UnixNano(),
	})
}

func (s *SettingService) IsAdminComplianceAcknowledged(ctx context.Context, adminUserID int64) (bool, error) {
	status, err := s.GetAdminComplianceStatus(ctx, adminUserID)
	if err != nil {
		return false, err
	}
	return status != nil && !status.Required, nil
}

func (s *SettingService) AcceptAdminCompliance(ctx context.Context, input AdminComplianceAcceptInput) (*AdminComplianceStatus, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.InternalServer("SETTING_SERVICE_UNAVAILABLE", "setting service is unavailable")
	}
	phrase := strings.TrimSpace(input.Phrase)
	if phrase != expectedAdminCompliancePhrase(input.Language) {
		return nil, ErrAdminComplianceInvalidPhrase
	}

	ack := AdminComplianceAcknowledgement{
		Version:     AdminComplianceVersion,
		DocumentZH:  AdminComplianceDocumentPathZH,
		DocumentEN:  AdminComplianceDocumentPathEN,
		AdminUserID: input.AdminUserID,
		IPAddress:   strings.TrimSpace(input.IPAddress),
		UserAgent:   strings.TrimSpace(input.UserAgent),
		AcceptedAt:  time.Now().UTC(),
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return nil, fmt.Errorf("marshal admin compliance acknowledgement: %w", err)
	}
	if err := s.settingRepo.Set(ctx, adminComplianceAcknowledgementKey(input.AdminUserID), string(payload)); err != nil {
		return nil, fmt.Errorf("save admin compliance acknowledgement: %w", err)
	}
	s.adminComplianceSF.Forget(strconv.FormatInt(input.AdminUserID, 10))
	s.storeAdminComplianceAcknowledgement(input.AdminUserID, ack)

	return s.GetAdminComplianceStatus(ctx, input.AdminUserID)
}
