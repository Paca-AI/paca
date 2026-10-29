package gitsvc

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/gitprovider"
)

// secretPatterns match credentials that commonly leak into CI logs. Masking is
// defence in depth: platforms mask their own secrets, but a token a job printed
// itself (or one for another service) is not theirs to know.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{16,}\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}\b`),
	regexp.MustCompile(`(?i)\b(bearer|token)\s+[A-Za-z0-9._\-]{24,}`),
	regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^@\s/]+(@)`),
	// No leading \b: names like DB_PASSWORD, STRIPE_API_KEY or GITHUB_ACCESS_TOKEN
	// have a word character (the underscore) before the keyword.
	regexp.MustCompile(`(?i)([A-Za-z0-9_]*(?:password|passwd|secret|api[_-]?key|access[_-]?token)[A-Za-z0-9_]*\s*[=:]\s*)\S+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`),
}

// RedactSecrets masks credentials that commonly leak into text a caller will
// see: CI logs and error messages a provider relays.
func RedactSecrets(text string) string {
	for i, re := range secretPatterns {
		switch i {
		case 7:
			text = re.ReplaceAllString(text, "${1}***${2}")
		case 8:
			text = re.ReplaceAllString(text, "${1}***")
		default:
			text = re.ReplaceAllString(text, "***")
		}
	}
	return text
}

// allowRerun records a check re-run for the repository, refusing when it has
// had maxRerunsPerHour in the last hour.
func (s *Service) allowRerun(repo uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	cutoff := now.Add(-time.Hour)
	kept := s.reruns[repo][:0]
	for _, at := range s.reruns[repo] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= maxRerunsPerHour {
		s.reruns[repo] = kept
		return false
	}
	s.reruns[repo] = append(kept, now)
	return true
}

// maxProviderMessage caps a provider's explanation shown to a user.
const maxProviderMessage = 500

// ProviderMessage is the message of a provider failure in err, safe to show a
// user: credentials masked, whitespace trimmed, control characters dropped and
// capped at maxProviderMessage characters. Empty when err is not a provider
// failure or the provider said nothing.
func ProviderMessage(err error) string {
	var pe *gitprovider.Error
	if !errors.As(err, &pe) {
		return ""
	}
	msg := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(RedactSecrets(pe.Message), ""))
	msg = strings.TrimSpace(msg)
	if r := []rune(msg); len(r) > maxProviderMessage {
		msg = string(r[:maxProviderMessage]) + "…"
	}
	return msg
}
