// Package totp implements RFC 6238 time-based one-time passwords (SHA-1,
// 6 digits, 30 s step) — the parameters every authenticator app supports.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // SHA-1 is what RFC 6238 authenticator apps use.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Period is the length of one time step.
	Period = 30 * time.Second
	// Digits is the length of a code.
	Digits = 6
	// secretBytes is the secret length: 160 bits, as RFC 4226 recommends.
	secretBytes = 20
	// skewSteps is how many steps either side of now a code may fall in, to
	// tolerate clock drift between the server and the user's device.
	skewSteps = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new random base32-encoded shared secret.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("totp: generate secret: %w", err)
	}
	return b32.EncodeToString(buf), nil
}

// ProvisioningURI returns the otpauth:// URI an authenticator app consumes
// (usually as a QR code) to enrol secret for account under issuer.
func ProvisioningURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(Digits))
	q.Set("period", fmt.Sprint(int(Period.Seconds())))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// stepAt returns the time-step counter for t.
func stepAt(t time.Time) int64 { return t.Unix() / int64(Period.Seconds()) }

// codeForStep computes the code for the given step.
func codeForStep(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("totp: decode secret: %w", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // step is non-negative.
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, bin%mod), nil
}

// Code returns the code valid at t.
func Code(secret string, t time.Time) (string, error) {
	return codeForStep(secret, stepAt(t))
}

// Verify checks code against secret at now, allowing for clock skew. It
// returns the matched time step so a caller can reject a replay of the same
// code (accept only steps greater than the last one used), and whether code
// matched at all.
func Verify(secret, code string, now time.Time) (step int64, ok bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != Digits {
		return 0, false
	}
	cur := stepAt(now)
	for d := int64(-skewSteps); d <= skewSteps; d++ {
		want, err := codeForStep(secret, cur+d)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return cur + d, true
		}
	}
	return 0, false
}
