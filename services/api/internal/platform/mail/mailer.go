// Package mail sends transactional e-mail over SMTP. It is a thin
// wrapper over net/smtp supporting three transport modes — plain, STARTTLS
// (opportunistic upgrade, typically port 587), and implicit TLS/SSL
// (typically port 465) — selected by the SMTPConfig flags.
package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// ErrNotConfigured is returned by Send when the config lacks the minimum
// fields (host, port, from address) needed to reach a server.
var ErrNotConfigured = errors.New("mail: SMTP not configured")

// SMTPConfig holds everything needed to submit a message to an SMTP server.
// Password is the plaintext password (callers decrypt it before building the
// config); it is never logged.
type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromEmail string
	FromName  string
	// UseSSL uses implicit TLS from the first byte (SMTPS, usually :465).
	UseSSL bool
	// UseTLS issues STARTTLS after connecting in plaintext (usually :587).
	UseTLS bool
	// SkipVerify disables TLS certificate verification (both implicit TLS and
	// STARTTLS). Insecure — set only for shared hosting whose certificate
	// doesn't match the SMTP hostname.
	SkipVerify bool
}

// tlsConfig builds the TLS settings for cfg, honoring SkipVerify.
func (c SMTPConfig) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: c.Host, InsecureSkipVerify: c.SkipVerify} //nolint:gosec // opt-in, admin-controlled for shared hosting cert mismatch
}

func (c SMTPConfig) configured() bool {
	return c.Host != "" && c.Port > 0 && c.FromEmail != ""
}

// Message is a single e-mail to one recipient. Body is HTML; a plaintext
// alternative is derived automatically so text-only clients get readable
// content.
type Message struct {
	To      string
	Subject string
	HTML    string
	// Text is the plaintext alternative. When empty, Send derives one from
	// HTML by stripping tags.
	Text string
}

// Sender submits messages to an SMTP server. The interface lets callers
// (e.g. the e-mail service and its tests) depend on the behavior, not the
// concrete net/smtp transport.
type Sender interface {
	Send(cfg SMTPConfig, msg Message) error
}

// SMTPSender is the production Sender backed by net/smtp.
type SMTPSender struct {
	// Timeout bounds the dial + handshake. Zero means 20s.
	Timeout time.Duration
}

// NewSMTPSender returns an SMTPSender with a sane default timeout.
func NewSMTPSender() *SMTPSender { return &SMTPSender{Timeout: 20 * time.Second} }

// Send submits msg using cfg. It returns ErrNotConfigured if cfg is
// incomplete, and wraps any transport error otherwise.
func (s *SMTPSender) Send(cfg SMTPConfig, msg Message) error {
	if !cfg.configured() {
		return ErrNotConfigured
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	raw := BuildRFC822(cfg, msg)

	client, err := dial(addr, cfg, timeout)
	if err != nil {
		return fmt.Errorf("mail: connect %s: %w", addr, err)
	}
	defer func() { _ = client.Close() }()

	if cfg.UseTLS && !cfg.UseSSL {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(cfg.tlsConfig()); err != nil {
				return fmt.Errorf("mail: starttls: %w", err)
			}
		}
	}

	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}

	if err := client.Mail(cfg.FromEmail); err != nil {
		return fmt.Errorf("mail: from: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("mail: rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("mail: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: close body: %w", err)
	}
	return client.Quit()
}

// dial opens an SMTP client, using implicit TLS when UseSSL is set.
func dial(addr string, cfg SMTPConfig, timeout time.Duration) (*smtp.Client, error) {
	d := net.Dialer{Timeout: timeout}
	if cfg.UseSSL {
		conn, err := tls.DialWithDialer(&d, "tcp", addr, cfg.tlsConfig())
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(conn, cfg.Host)
	}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return smtp.NewClient(conn, cfg.Host)
}

// BuildRFC822 renders a MIME multipart/alternative message (plaintext +
// HTML). Exported so tests can assert headers/body without a live server.
func BuildRFC822(cfg SMTPConfig, msg Message) []byte {
	text := msg.Text
	if text == "" {
		text = htmlToText(msg.HTML)
	}

	from := cfg.FromEmail
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromEmail)
	}

	boundary := "paca-boundary-9c1f"
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString(`Content-Type: multipart/alternative; boundary="` + boundary + "\"\r\n")
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(text + "\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(msg.HTML + "\r\n\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// htmlToText produces a crude plaintext fallback by dropping tags and
// collapsing whitespace — good enough for the simple, mostly-linear
// credential e-mails this package sends.
func htmlToText(html string) string {
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</h1>", "\n\n", "</h2>", "\n\n",
	)
	s := replacer.Replace(html)
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	lines := strings.Split(out.String(), "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
