package acp

import (
	"errors"
	"fmt"
	"strings"
)

// ProviderErrorKind classifies a known-shape LLM provider failure relayed
// through goose's JSON-RPC error (see rpcError.Error's doc comment: goose
// puts the actual upstream HTTP status/response body in Data, which is what
// this classification runs against). Distinct from the generic "failed"
// status every other executor error gets — these two are common enough,
// and confusing enough to read as a raw provider error blob, that callers
// should show the user a specific, actionable message instead.
type ProviderErrorKind string

const (
	// ProviderErrorRateLimited covers transient "too many requests" /
	// provider-overloaded responses — retrying later is expected to work.
	ProviderErrorRateLimited ProviderErrorKind = "rate_limited"
	// ProviderErrorQuotaExceeded covers the account itself being out of
	// tokens/credits or otherwise blocked on payment — retrying won't help
	// until the account's billing/plan is fixed.
	ProviderErrorQuotaExceeded ProviderErrorKind = "quota_exceeded"
)

// quotaPatterns and rateLimitPatterns are matched case-insensitively
// against the provider error text. Sourced from providers goose actually
// routes to (see resolveProviderEnv) as each wording is confirmed live:
// Anthropic's low-balance error ("credit balance is too low"), OpenAI's
// insufficient_quota error code/message, Mistral's "Credits exhausted:
// {"detail":"Check your subscription..."}" (confirmed against a real
// session/new failure — conversation 76b6854b-f5ab-4ddb-9c17-04318ac00644,
// a Mistral agent out of credits, which this pattern set originally missed
// entirely and fell through to the raw-error fallback), and both
// providers' 429 rate-limit and Anthropic's 529 overloaded_error shapes.
// Deliberately broad substrings rather than exact provider strings: no
// test fixture captures every provider's exact relay wording up front, so
// this favors matching real occurrences over precision — expect to keep
// adding patterns as new providers/wordings show up in real failures.
var (
	quotaPatterns = []string{
		"insufficient_quota",
		"exceeded your current quota",
		"credit balance is too low",
		"credit balance",
		"credits exhausted",
		"payment required",
		"status: 402",
	}
	rateLimitPatterns = []string{
		"rate_limit",
		"rate limit",
		"too many requests",
		"overloaded_error",
		"status: 429",
		"status: 529",
	}
)

// ClassifyProviderError inspects err's chain for an rpcError and matches
// its text against known quota/rate-limit patterns. ok is false when err
// isn't an rpcError at all, or doesn't match any known pattern — callers
// should fall back to showing err's own message in that case.
//
// Quota patterns are checked before rate-limit ones since OpenAI's
// insufficient_quota error also carries HTTP 429, which would otherwise
// also match the generic "status: 429" rate-limit pattern.
func ClassifyProviderError(err error) (kind ProviderErrorKind, ok bool) {
	var rpcErr *rpcError
	if !errors.As(err, &rpcErr) {
		return "", false
	}
	text := strings.ToLower(rpcErr.Error())

	for _, p := range quotaPatterns {
		if strings.Contains(text, p) {
			return ProviderErrorQuotaExceeded, true
		}
	}
	for _, p := range rateLimitPatterns {
		if strings.Contains(text, p) {
			return ProviderErrorRateLimited, true
		}
	}
	return "", false
}

// FriendlyMessage returns the user-facing sentence for kind, meant to
// replace the raw provider error text in a conversation's persisted
// error_message — the raw text goose relays (a wrapped upstream HTTP
// status/JSON body, see rpcError.Error) is diagnostic, not something an
// end user should have to parse.
func (kind ProviderErrorKind) FriendlyMessage() string {
	switch kind {
	case ProviderErrorRateLimited:
		return "The LLM provider is rate-limiting requests right now. Please wait a moment and try again."
	case ProviderErrorQuotaExceeded:
		return "The LLM provider account has run out of tokens/credits or needs payment. Please check its billing or plan, then try again."
	default:
		return fmt.Sprintf("The LLM provider returned an error (%s).", kind)
	}
}
