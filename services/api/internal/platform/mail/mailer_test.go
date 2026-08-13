package mail

import (
	"strings"
	"testing"
)

func TestBuildRFC822_HeadersAndMultipart(t *testing.T) {
	cfg := SMTPConfig{FromEmail: "no-reply@example.com", FromName: "Acme"}
	msg := Message{To: "user@example.com", Subject: "Hello", HTML: "<p>Hi <strong>there</strong></p>"}

	raw := string(BuildRFC822(cfg, msg))

	for _, want := range []string{
		"From: Acme <no-reply@example.com>",
		"To: user@example.com",
		"Subject: Hello",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"text/plain",
		"text/html",
		"<p>Hi <strong>there</strong></p>",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("message missing %q\n---\n%s", want, raw)
		}
	}
}

func TestBuildRFC822_FromWithoutName_UsesBareAddress(t *testing.T) {
	cfg := SMTPConfig{FromEmail: "no-reply@example.com"}
	raw := string(BuildRFC822(cfg, Message{To: "a@b.com", Subject: "s", HTML: "<p>x</p>"}))
	if !strings.Contains(raw, "From: no-reply@example.com\r\n") {
		t.Errorf("expected bare From address, got:\n%s", raw)
	}
}

func TestBuildRFC822_DerivesPlaintextFromHTML(t *testing.T) {
	cfg := SMTPConfig{FromEmail: "f@x.com"}
	raw := string(BuildRFC822(cfg, Message{To: "a@b.com", Subject: "s", HTML: "<p>Line one</p><p>Line two</p>"}))
	if !strings.Contains(raw, "Line one") || !strings.Contains(raw, "Line two") {
		t.Errorf("plaintext fallback missing content:\n%s", raw)
	}
	// The plaintext part must not carry raw tags.
	plainStart := strings.Index(raw, "text/plain")
	htmlStart := strings.Index(raw, "text/html")
	plainSection := raw[plainStart:htmlStart]
	if strings.Contains(plainSection, "<p>") {
		t.Errorf("plaintext section still contains tags:\n%s", plainSection)
	}
}

func TestTLSConfig_SkipVerify(t *testing.T) {
	secure := SMTPConfig{Host: "smtp.example.com"}.tlsConfig()
	if secure.InsecureSkipVerify {
		t.Error("verification should be on by default")
	}
	if secure.ServerName != "smtp.example.com" {
		t.Errorf("ServerName not set: %q", secure.ServerName)
	}
	insecure := SMTPConfig{Host: "smtp.example.com", SkipVerify: true}.tlsConfig()
	if !insecure.InsecureSkipVerify {
		t.Error("SkipVerify should disable verification")
	}
}

func TestSend_NotConfigured_ReturnsErr(t *testing.T) {
	s := NewSMTPSender()
	// Missing host/port/from → not configured.
	err := s.Send(SMTPConfig{}, Message{To: "a@b.com", Subject: "s", HTML: "x"})
	if err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}
