package service

import (
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// grokBaseURLValidator resolves the outbound URL policy for one account.
// OAuth lifecycle endpoints are never routed through this helper: they remain
// pinned to xAI's official authorization/token hosts. This validator only
// governs forwarding and probing endpoints.
func grokBaseURLValidator(account *Account, cfg *config.Config) (xai.BaseURLValidator, error) {
	if account == nil || !account.IsGrok() {
		return nil, fmt.Errorf("grok account is required")
	}

	policyValidator := grokOperatorPolicyValidator(cfg)
	if account.IsGrokOAuth() {
		// Official xAI hosts are trusted even when the operator allowlist is
		// restrictive. A custom OAuth forwarding host still goes through the
		// operator policy; unsafe debug flags must not bypass this boundary.
		return redactedGrokBaseURLValidator(func(raw string) (string, error) {
			if xai.IsOfficialBaseURL(raw) {
				return xai.ValidateTrustedBaseURL(raw)
			}
			return policyValidator(raw)
		}), nil
	}
	if account.IsGrokAPIKey() {
		return redactedGrokBaseURLValidator(policyValidator), nil
	}
	return nil, fmt.Errorf("unsupported grok account type: %s", account.Type)
}

func grokOperatorPolicyValidator(cfg *config.Config) xai.BaseURLValidator {
	if cfg == nil {
		return xai.ValidateBaseURL
	}
	if !cfg.Security.URLAllowlist.Enabled {
		return func(raw string) (string, error) {
			return urlvalidator.ValidateURLFormat(raw, cfg.Security.URLAllowlist.AllowInsecureHTTP)
		}
	}
	return func(raw string) (string, error) {
		return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
			AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
			RequireAllowlist: true,
			AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
		})
	}
}

func redactedGrokBaseURLValidator(validator xai.BaseURLValidator) xai.BaseURLValidator {
	return func(raw string) (string, error) {
		validated, err := validator(raw)
		if err != nil {
			// Do not echo a configured URL. It can contain credentials or other
			// sensitive components and the caller only needs the policy result.
			return "", errors.New("base URL rejected by URL security policy")
		}
		return validated, nil
	}
}

func buildGrokResponsesURL(account *Account, cfg *config.Config) (string, error) {
	validator, err := grokBaseURLValidator(account, cfg)
	if err != nil {
		return "", err
	}
	return xai.BuildResponsesURLWithValidator(account.GetGrokBaseURL(), validator)
}

func buildGrokChatCompletionsURL(account *Account, cfg *config.Config) (string, error) {
	validator, err := grokBaseURLValidator(account, cfg)
	if err != nil {
		return "", err
	}
	return xai.BuildChatCompletionsURLWithValidator(account.GetGrokBaseURL(), validator)
}

func buildGrokBillingURL(account *Account, cfg *config.Config, weekly bool) (string, error) {
	validator, err := grokBaseURLValidator(account, cfg)
	if err != nil {
		return "", err
	}
	return xai.BuildBillingURLWithValidator(account.GetGrokBaseURL(), weekly, validator)
}
