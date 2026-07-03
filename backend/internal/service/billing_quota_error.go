package service

import "strings"

var recoverableBillingQuotaKeywords = []string{
	"billing error",
	"billing issue",
	"payment required",
	"your credit balance is too low",
	"credit balance is too low",
	"credit balance too low",
	"insufficient credit balance",
	"not enough credit balance",
	"credit balance exhausted",
	"credit balance has been exhausted",
	"insufficient credit",
	"insufficient credits",
	"not enough credit",
	"not enough credits",
	"credit exhausted",
	"credits exhausted",
	"billing hard limit has been reached",
	"billing hard limit reached",
	"billing account is not active",
	"billing account has been disabled",
	"billing account disabled",
	"billing has been disabled",
	"you exceeded your current quota",
	"exceeded your current quota",
	"insufficient quota",
	"quota exceeded",
	"cloud billing is not enabled",
	"billing is not enabled",
	"billing not enabled",
}

func normalizeLooseErrorText(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.NewReplacer("_", " ", "-", " ").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}

func isRecoverableBillingQuotaText(text string) bool {
	normalized := normalizeLooseErrorText(text)
	if normalized == "" {
		return false
	}
	for _, keyword := range recoverableBillingQuotaKeywords {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}
