package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

const openaiResponsesProbeTimeout = 8 * time.Second

func openaiResponsesProbePayload(modelID string) []byte {
	if strings.TrimSpace(modelID) == "" {
		modelID = openai.DefaultTestModel
	}
	body, _ := json.Marshal(map[string]any{
		"model": modelID,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "hi"},
				},
			},
		},
		"instructions": openai.DefaultInstructions,
		"stream":       false,
	})
	return body
}

// ProbeOpenAIAPIKeyResponsesSupport probes whether an OpenAI API key account's
// upstream exposes /v1/responses and stores the result in account extra.
func (s *AccountTestService) ProbeOpenAIAPIKeyResponsesSupport(ctx context.Context, accountID int64) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_load_account_failed: account_id=%d err=%v", accountID, err)
		return
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return
	}

	apiKey := account.GetOpenAIApiKey()
	if strings.TrimSpace(apiKey) == "" {
		logger.LegacyPrintf("service.openai_probe", "probe_skip_no_apikey: account_id=%d", accountID)
		return
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_invalid_baseurl: account_id=%d base_url=%q err=%v", accountID, baseURL, err)
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, openaiResponsesProbeTimeout)
	defer cancel()

	probeURL := buildOpenAIResponsesURL(normalizedBaseURL)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, probeURL, bytes.NewReader(openaiResponsesProbePayload("")))
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_build_request_failed: account_id=%d err=%v", accountID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var tlsProfileResolver *TLSFingerprintProfileService
	if s.tlsFPProfileService != nil {
		tlsProfileResolver = s.tlsFPProfileService
	}
	var resp *http.Response
	if tlsProfileResolver != nil {
		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, tlsProfileResolver.ResolveTLSProfile(account))
	} else {
		resp, err = s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	if err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_request_failed: account_id=%d url=%s err=%v", accountID, probeURL, err)
		return
	}
	if resp == nil {
		logger.LegacyPrintf("service.openai_probe", "probe_empty_response: account_id=%d url=%s", accountID, probeURL)
		return
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
		}
	}()

	supported := isResponsesEndpointSupportedByStatus(resp.StatusCode)
	if err := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{
		openai_compat.ExtraKeyResponsesSupported: supported,
	}); err != nil {
		logger.LegacyPrintf("service.openai_probe", "probe_persist_failed: account_id=%d supported=%v err=%v", accountID, supported, err)
		return
	}

	logger.LegacyPrintf("service.openai_probe",
		"probe_done: account_id=%d base_url=%s status=%d supported=%v",
		accountID, normalizedBaseURL, resp.StatusCode, supported,
	)
}

func isResponsesEndpointSupportedByStatus(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false
	default:
		return true
	}
}
