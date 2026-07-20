package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokDefaultResponsesModel = "grok-4.5"
	grokCLIVersion            = xai.CLIClientVersion
)

func (s *OpenAIGatewayService) forwardGrokResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	reqStream bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	if account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("grok account type %s is not supported by Responses forwarding", account.Type)
	}

	upstreamModel := account.GetMappedModel(originalModel)
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	patchedBody, err := patchGrokResponsesBody(body, upstreamModel)
	if err != nil {
		return nil, err
	}
	// Derive the identity from the request xAI will actually see. This makes
	// Codex Responses Lite additional_tools part of the stable tool prefix.
	cacheIdentity := resolveGrokCacheIdentity(c, patchedBody, "", upstreamModel)
	mixedCacheIntentBody := append([]byte(nil), patchedBody...)
	patchedBody, err = applyGrokResponsesCacheIdentity(patchedBody, body, cacheIdentity, account.IsGrokOAuth())
	if err != nil {
		return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
	}
	patchedBody, err = applyGrokFreeRequestToolCacheRoute(c, patchedBody, mixedCacheIntentBody, account, cacheIdentity)
	if err != nil {
		return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
	}

	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}

	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, reqStream)
	defer releaseUpstreamCtx()

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamStart := time.Now()
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		upstreamReq, buildErr := buildGrokResponsesRequestWithPolicy(
			upstreamCtx, c, account, patchedBody, token, s.settingService, s.cfg, cacheIdentity,
		)
		if buildErr != nil {
			return nil, buildErr
		}

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
		if err != nil {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
		}

		// xAI can reject encrypted reasoning copied from a response produced under
		// another account or cache identity. Retry once with the same routing and
		// credential after removing only the rejected encrypted reasoning payload.
		if attempt > 0 || resp.StatusCode != http.StatusBadRequest {
			break
		}
		respBody := s.readUpstreamErrorBody(resp)
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		if !isGrokInvalidEncryptedContentResponse(resp.StatusCode, respBody) {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			break
		}

		retryBody, changed, trimErr := trimGrokInvalidEncryptedContentRetryBody(patchedBody)
		if trimErr != nil {
			return nil, fmt.Errorf("prepare Grok invalid encrypted_content retry: %w", trimErr)
		}
		if !changed {
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			break
		}

		patchedBody = retryBody
		slog.Info("grok_invalid_encrypted_content_retry", "account_id", account.ID, "cache_identity_present", cacheIdentity != "")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(respBody))
		if upstreamMsg == "" {
			upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
			Kind:               "failover",
			Message:            upstreamMsg,
		})
		s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		if s.shouldFailoverUpstreamError(resp.StatusCode) {
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				ResponseHeaders:        resp.Header.Clone(),
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleErrorResponse(ctx, resp, c, account, patchedBody, upstreamModel)
	}

	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)

	var usage *OpenAIUsage
	var firstTokenMs *int
	responseID := ""
	if reqStream {
		streamResult, err := s.handleStreamingResponse(ctx, resp, c, account, startTime, originalModel, upstreamModel)
		if err != nil {
			return nil, err
		}
		usage = streamResult.usage
		firstTokenMs = streamResult.firstTokenMs
		responseID = strings.TrimSpace(streamResult.responseID)
	} else {
		nonStreamResult, err := s.handleNonStreamingResponse(ctx, resp, c, account, originalModel, upstreamModel)
		if err != nil {
			return nil, err
		}
		usage = nonStreamResult.usage
		responseID = strings.TrimSpace(nonStreamResult.responseID)
	}

	if usage == nil {
		usage = &OpenAIUsage{}
	}
	return &OpenAIForwardResult{
		RequestID:       firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id")),
		ResponseID:      responseID,
		Usage:           *usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: extractOpenAIReasoningEffortFromBody(patchedBody, originalModel),
		Stream:          reqStream,
		OpenAIWSMode:    false,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(startTime),
		FirstTokenMs:    firstTokenMs,
	}, nil
}

func isGrokInvalidEncryptedContentResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}

	// xAI has used both flat and nested error envelopes:
	//   {"code":"invalid-argument","error":"Could not decrypt the provided encrypted_content."}
	//   {"error":{"message":"Could not decrypt the provided encrypted_content."}}
	code := strings.TrimSpace(gjson.GetBytes(body, "code").String())
	message := ""
	errNode := gjson.GetBytes(body, "error")
	switch {
	case errNode.Type == gjson.String:
		message = errNode.String()
	case errNode.IsObject():
		message = firstNonEmpty(errNode.Get("message").String(), errNode.Get("error").String())
		if code == "" {
			code = strings.TrimSpace(errNode.Get("code").String())
		}
	default:
		message = gjson.GetBytes(body, "message").String()
	}
	normalizedMessage := strings.ToLower(strings.TrimSpace(message))
	if normalizedMessage == "" {
		return false
	}

	if strings.EqualFold(code, "invalid_encrypted_content") {
		return true
	}
	// Keep the official xAI flat-code gate so unrelated 400s are not retried.
	if !strings.EqualFold(code, "invalid-argument") && code != "" {
		return false
	}
	// Nested OpenAI-style envelopes may omit top-level code; require decrypt text.
	if code == "" && !strings.Contains(normalizedMessage, "decrypt") {
		return false
	}
	return strings.Contains(normalizedMessage, "encrypted_content") &&
		(strings.Contains(normalizedMessage, "decrypt") ||
			strings.Contains(normalizedMessage, "unmodified"))
}

// requestHasGrokEncryptedReasoning reports whether the outbound Responses body
// still carries reasoning.encrypted_content that can be stripped for retry.
func requestHasGrokEncryptedReasoning(body []byte) bool {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() {
		return false
	}
	items := input.Array()
	if input.IsObject() {
		items = []gjson.Result{input}
	}
	for _, item := range items {
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			continue
		}
		enc := item.Get("encrypted_content")
		if enc.Exists() && enc.Type != gjson.Null && strings.TrimSpace(enc.String()) != "" {
			return true
		}
	}
	return false
}

type grokEncryptedContentStripRetriedKey struct{}

func markGrokEncryptedContentStripRetried(ctx context.Context) context.Context {
	return context.WithValue(ctx, grokEncryptedContentStripRetriedKey{}, true)
}

func grokEncryptedContentStripRetried(ctx context.Context) bool {
	v, _ := ctx.Value(grokEncryptedContentStripRetriedKey{}).(bool)
	return v
}

// stripAnthropicThinkingSignatures removes thinking.signature from Claude
// history so a different Grok OAuth account can accept multi-turn tool
// continuations after decrypt failures. Returns ok=false when nothing changed.
func stripAnthropicThinkingSignatures(body []byte) ([]byte, bool) {
	if len(body) == 0 || !bytes.Contains(body, []byte(`"signature"`)) {
		return body, false
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body, false
	}
	messages, ok := req["messages"].([]any)
	if !ok || len(messages) == 0 {
		return body, false
	}
	changed := false
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := block["type"].(string); typ != "thinking" {
				continue
			}
			if _, has := block["signature"]; has {
				delete(block, "signature")
				changed = true
			}
		}
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(req)
	if err != nil {
		return body, false
	}
	return out, true
}

func trimGrokInvalidEncryptedContentRetryBody(body []byte) ([]byte, bool, error) {
	input := gjson.GetBytes(body, "input")
	items := input.Array()
	if input.IsObject() {
		items = []gjson.Result{input}
	}

	hasEncryptedReasoning := false
	for _, item := range items {
		if strings.TrimSpace(item.Get("type").String()) == "reasoning" && item.Get("encrypted_content").Exists() {
			hasEncryptedReasoning = true
			break
		}
	}
	if !hasEncryptedReasoning {
		return body, false, nil
	}

	var requestBody map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, false, err
	}
	if !trimOpenAIEncryptedReasoningItems(requestBody) {
		return body, false, nil
	}

	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, false, err
	}
	return retryBody, true, nil
}

func patchGrokResponsesBody(body []byte, upstreamModel string) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid json request body")
	}
	out, err := sjson.SetBytes(body, "model", upstreamModel)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokResponsesModelCapabilities(out, upstreamModel)
	if err != nil {
		return nil, err
	}
	for _, unsupportedField := range []string{"prompt_cache_retention", "safety_identifier"} {
		if gjson.GetBytes(out, unsupportedField).Exists() {
			out, err = sjson.DeleteBytes(out, unsupportedField)
			if err != nil {
				return nil, err
			}
		}
	}
	if strings.EqualFold(upstreamModel, "grok-4.5") {
		for _, unsupportedField := range []string{"presence_penalty", "presencePenalty", "frequency_penalty", "frequencyPenalty", "stop"} {
			if gjson.GetBytes(out, unsupportedField).Exists() {
				out, err = sjson.DeleteBytes(out, unsupportedField)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	out, err = sanitizeGrokResponsesUnsupportedFields(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokResponsesInput(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokReasoningNullContent(out)
	if err != nil {
		return nil, err
	}
	out, err = sanitizeGrokResponsesTools(out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sanitizeGrokResponsesModelCapabilities(body []byte, upstreamModel string) ([]byte, error) {
	if !grokModelRejectsReasoningEffort(upstreamModel) {
		return body, nil
	}

	out := body
	for _, field := range []string{"reasoning", "reasoning_effort", "reasoningEffort"} {
		if !gjson.GetBytes(out, field).Exists() {
			continue
		}
		var err error
		out, err = sjson.DeleteBytes(out, field)
		if err != nil {
			return nil, fmt.Errorf("remove unsupported Grok Composer %s: %w", field, err)
		}
	}
	return out, nil
}

func grokModelRejectsReasoningEffort(model string) bool {
	model = strings.TrimSpace(strings.ToLower(model))
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = strings.TrimSpace(model[slash+1:])
	}
	switch model {
	case "grok-composer", "grok-composer-2.5-fast", "composer-2.5":
		return true
	default:
		return false
	}
}

var grokResponsesUnsupportedRecursiveFields = map[string]struct{}{
	"external_web_access": {},
}

func sanitizeGrokResponsesUnsupportedFields(body []byte) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"external_web_access"`)) {
		return body, nil
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if !deleteGrokJSONFields(payload, grokResponsesUnsupportedRecursiveFields) {
		return body, nil
	}
	return json.Marshal(payload)
}

func deleteGrokJSONFields(value any, fields map[string]struct{}) bool {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for field := range fields {
			if _, ok := typed[field]; ok {
				delete(typed, field)
				changed = true
			}
		}
		for _, child := range typed {
			changed = deleteGrokJSONFields(child, fields) || changed
		}
		return changed
	case []any:
		changed := false
		for _, child := range typed {
			changed = deleteGrokJSONFields(child, fields) || changed
		}
		return changed
	default:
		return false
	}
}

// additional_tools is a private Codex/Responses carrier that is not part of
// xAI's ModelInput union. Keep normal messages and top-level tools while
// removing this item before forwarding to the Grok subscription proxy.
func sanitizeGrokResponsesInput(body []byte) ([]byte, error) {
	if !bytes.Contains(body, []byte(`"additional_tools"`)) {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	rawItems := input.Array()
	filtered := make([]json.RawMessage, 0, len(rawItems))
	topLevelTools := gjson.GetBytes(body, "tools")
	mergedTools := make([]json.RawMessage, 0)
	seenTools := make(map[string]struct{})
	appendTool := func(tool gjson.Result) bool {
		key := grokResponsesToolDedupKey(tool)
		if _, exists := seenTools[key]; exists {
			return false
		}
		seenTools[key] = struct{}{}
		mergedTools = append(mergedTools, json.RawMessage(tool.Raw))
		return true
	}
	if topLevelTools.IsArray() {
		for _, tool := range topLevelTools.Array() {
			seenTools[grokResponsesToolDedupKey(tool)] = struct{}{}
			mergedTools = append(mergedTools, json.RawMessage(tool.Raw))
		}
	}

	promoted := false
	for _, item := range rawItems {
		if strings.TrimSpace(item.Get("type").String()) == "additional_tools" {
			tools := item.Get("tools")
			if tools.IsArray() {
				for _, tool := range tools.Array() {
					if appendTool(tool) {
						promoted = true
					}
				}
			}
			continue
		}
		filtered = append(filtered, json.RawMessage(item.Raw))
	}
	if len(filtered) == len(rawItems) {
		return body, nil
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	body, err = sjson.SetRawBytes(body, "input", encoded)
	if err != nil || !promoted {
		return body, err
	}
	encodedTools, err := json.Marshal(mergedTools)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(body, "tools", encodedTools)
}

func grokResponsesToolDedupKey(tool gjson.Result) string {
	toolType := strings.TrimSpace(tool.Get("type").String())
	if toolType != "" {
		if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
			return "type:" + toolType + "\x00name:" + name
		}
		if toolType == "mcp" {
			if label := strings.TrimSpace(tool.Get("server_label").String()); label != "" {
				return "type:mcp\x00server_label:" + label
			}
		}
	}
	return "json:" + normalizeCompatSeedJSON(json.RawMessage(tool.Raw))
}

// sanitizeGrokReasoningNullContent 删除 reasoning 项中的 "content": null。
// xAI 的 untagged enum 反序列化器拒收该字段，返回 422。
func sanitizeGrokReasoningNullContent(body []byte) ([]byte, error) {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	items := input.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			continue
		}
		contentResult := item.Get("content")
		if contentResult.Exists() && contentResult.Type == gjson.Null {
			var err error
			body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d.content", i))
			if err != nil {
				return nil, err
			}
		}
	}
	return body, nil
}

var grokResponsesSupportedToolTypes = map[string]struct{}{
	"code_execution":     {},
	"code_interpreter":   {},
	"collections_search": {},
	"file_search":        {},
	"function":           {},
	"mcp":                {},
	"shell":              {},
	"web_search":         {},
	"x_search":           {},
}

func sanitizeGrokResponsesTools(body []byte) ([]byte, error) {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return body, nil
	}
	rawTools := tools.Array()
	filteredTools := make([]json.RawMessage, 0, len(rawTools))
	for _, tool := range rawTools {
		if _, ok := grokResponsesSupportedToolTypes[strings.TrimSpace(tool.Get("type").String())]; ok {
			filteredTools = append(filteredTools, json.RawMessage(tool.Raw))
		}
	}

	var err error
	if len(filteredTools) != len(rawTools) {
		if len(filteredTools) == 0 {
			body, err = sjson.DeleteBytes(body, "tools")
		} else {
			encoded, marshalErr := json.Marshal(filteredTools)
			if marshalErr != nil {
				return nil, marshalErr
			}
			body, err = sjson.SetRawBytes(body, "tools", encoded)
		}
		if err != nil {
			return nil, err
		}
	}

	toolChoice := gjson.GetBytes(body, "tool_choice")
	if toolChoice.Exists() && shouldDropGrokToolChoice(toolChoice, filteredTools) {
		return sjson.DeleteBytes(body, "tool_choice")
	}
	return body, nil
}

func shouldDropGrokToolChoice(toolChoice gjson.Result, tools []json.RawMessage) bool {
	if len(tools) == 0 {
		return true
	}
	if !toolChoice.IsObject() {
		return false
	}
	choiceType := strings.TrimSpace(toolChoice.Get("type").String())
	if choiceType == "" {
		return false
	}
	if _, ok := grokResponsesSupportedToolTypes[choiceType]; !ok {
		return true
	}
	if choiceType != "function" {
		return false
	}
	choiceName := strings.TrimSpace(toolChoice.Get("name").String())
	if choiceName == "" {
		choiceName = strings.TrimSpace(toolChoice.Get("function.name").String())
	}
	if choiceName == "" {
		return false
	}
	for _, tool := range tools {
		name := strings.TrimSpace(gjson.GetBytes(tool, "name").String())
		if name == "" {
			name = strings.TrimSpace(gjson.GetBytes(tool, "function.name").String())
		}
		if strings.TrimSpace(gjson.GetBytes(tool, "type").String()) == "function" && name == choiceName {
			return false
		}
	}
	return true
}

func buildGrokResponsesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token, cacheIdentity string, cfg *config.Config) (*http.Request, error) {
	return buildGrokResponsesRequestWithPolicy(ctx, c, account, body, token, nil, cfg, cacheIdentity)
}

func buildGrokResponsesRequestWithPolicy(ctx context.Context, c *gin.Context, account *Account, body []byte, token string, settingService *SettingService, cfg *config.Config, cacheIdentityOpt ...string) (*http.Request, error) {
	targetURL, err := buildGrokResponsesURL(account, cfg, settingService)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if account.IsGrokOAuth() {
		applyGrokCLIHeaders(req.Header)
	}
	cacheIdentity := ""
	if len(cacheIdentityOpt) > 0 {
		cacheIdentity = cacheIdentityOpt[0]
	}
	applyGrokCacheHeaders(req.Header, cacheIdentity)
	if c != nil {
		if v := c.GetHeader("OpenAI-Beta"); strings.TrimSpace(v) != "" {
			req.Header.Set("OpenAI-Beta", v)
		}
	}
	// Apply account overrides after built-in defaults. Security filtering in
	// Account.ApplyHeaderOverrides prevents authentication/session takeover.
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

const grokUpstreamUserAgent = "sub2api-grok/1.0"

// applyGrokCLIHeaders identifies subscription traffic as a supported Grok CLI
// client. The official CLI gateway rejects otherwise valid OAuth requests
// without these headers. They are only added for OAuth accounts.
func applyGrokCLIHeaders(headers http.Header) {
	if headers == nil {
		return
	}
	headers.Set("User-Agent", grokUpstreamUserAgent)
	headers.Set(xai.CLIClientVersionHeader, xai.CLIClientVersion)
}

func (s *OpenAIGatewayService) handleGrokAccountUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) {
	if s == nil || account == nil {
		return
	}
	now := time.Now()
	s.updateGrokUsageSnapshot(ctx, account, parseGrokQuotaSnapshot(headers, statusCode, now))
	switch statusCode {
	case http.StatusUnauthorized:
		s.tempUnscheduleGrok(ctx, account, 10*time.Minute, "grok credentials unauthorized")
	case http.StatusForbidden:
		s.tempUnscheduleGrok(ctx, account, 30*time.Minute, "grok access or entitlement denied")
	case http.StatusTooManyRequests:
		// updateGrokUsageSnapshot installs both the immediate runtime fence and
		// the durable account-level rate limit.
	default:
		if statusCode >= 500 {
			s.tempUnscheduleGrok(ctx, account, 2*time.Minute, "grok upstream temporary error")
		}
	}
	_ = responseBody
}

func (s *OpenAIGatewayService) tempUnscheduleGrok(ctx context.Context, account *Account, cooldown time.Duration, reason string) {
	if s == nil || account == nil {
		return
	}
	until := time.Now().Add(cooldown)
	if account.TempUnschedulableUntil != nil && account.TempUnschedulableUntil.After(until) {
		until = *account.TempUnschedulableUntil
	}
	s.BlockAccountScheduling(account, until, reason)
	if s.accountRepo != nil {
		stateCtx, cancel := openAIAccountStateContext(ctx)
		defer cancel()
		_ = s.accountRepo.SetTempUnschedulable(stateCtx, account.ID, until, reason)
	}
}
