// Package email owns the SMTP e-mail configuration and the transactional
// e-mails Paca sends: new-user credentials, password resets, and a
// test message. It reads/writes the SMTP portion of the singleton
// workspace_settings row, encrypts the SMTP password at rest, and delegates
// the actual send to a mail.Sender.
package email

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"

	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/platform/mail"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// Encryptor is the subset of secret.Encryptor this service needs.
type Encryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encoded string) (string, error)
}

// Service manages e-mail settings and sending.
type Service struct {
	repo      settingsdom.Repository
	enc       Encryptor
	sender    mail.Sender
	publicURL string
}

// NewService wires the e-mail service. publicURL is the externally reachable
// base URL (e.g. https://paca.example.com) used to build the login link in
// e-mails; it may be empty (the link is then omitted).
func NewService(repo settingsdom.Repository, enc Encryptor, sender mail.Sender, publicURL string) *Service {
	return &Service{repo: repo, enc: enc, sender: sender, publicURL: strings.TrimRight(publicURL, "/")}
}

// SettingsView is the safe read model of the e-mail settings — never exposes
// the SMTP password, only whether one is stored.
type SettingsView struct {
	FromEmail            string
	FromName             string
	Host                 string
	Port                 int
	Username             string
	UseSSL               bool
	UseTLS               bool
	SkipVerify           bool
	SendUserCreatedEmail bool
	// PasswordSet reports whether a password is stored, so the UI can show a
	// "leave blank to keep current" placeholder instead of the secret.
	PasswordSet bool
	// Configured mirrors WorkspaceSettings.SMTPConfigured — enough fields to
	// attempt a send.
	Configured bool
}

// Get returns the current e-mail settings without the password.
func (s *Service) Get(ctx context.Context) (*SettingsView, error) {
	ws, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &SettingsView{
		FromEmail:            deref(ws.SMTPFromEmail),
		FromName:             deref(ws.SMTPFromName),
		Host:                 deref(ws.SMTPHost),
		Port:                 derefInt(ws.SMTPPort),
		Username:             deref(ws.SMTPUsername),
		UseSSL:               ws.SMTPUseSSL,
		UseTLS:               ws.SMTPUseTLS,
		SkipVerify:           ws.SMTPSkipVerify,
		SendUserCreatedEmail: ws.SendUserCreatedEmail,
		PasswordSet:          ws.SMTPPasswordEncrypted != nil && *ws.SMTPPasswordEncrypted != "",
		Configured:           ws.SMTPConfigured(),
	}, nil
}

// UpdateInput carries the writable e-mail settings. Password is tri-state:
// nil keeps the stored password, "" clears it, any other value replaces it
// (encrypted before persisting).
type UpdateInput struct {
	FromEmail            string
	FromName             string
	Host                 string
	Port                 int
	Username             string
	Password             *string
	UseSSL               bool
	UseTLS               bool
	SkipVerify           bool
	SendUserCreatedEmail bool
}

// Update persists the e-mail settings, encrypting the password when present.
func (s *Service) Update(ctx context.Context, in UpdateInput, updatedBy uuid.UUID) (*SettingsView, error) {
	_, err := s.repo.WithLock(ctx, func(ws *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		ws.SMTPFromEmail = strPtr(in.FromEmail)
		ws.SMTPFromName = strPtr(in.FromName)
		ws.SMTPHost = strPtr(in.Host)
		ws.SMTPUsername = strPtr(in.Username)
		if in.Port > 0 {
			p := in.Port
			ws.SMTPPort = &p
		} else {
			ws.SMTPPort = nil
		}
		ws.SMTPUseSSL = in.UseSSL
		ws.SMTPUseTLS = in.UseTLS
		ws.SMTPSkipVerify = in.SkipVerify
		ws.SendUserCreatedEmail = in.SendUserCreatedEmail

		switch {
		case in.Password == nil:
			// keep existing encrypted password
		case *in.Password == "":
			ws.SMTPPasswordEncrypted = nil
		default:
			enc, err := s.enc.Encrypt(*in.Password)
			if err != nil {
				return nil, fmt.Errorf("email svc: encrypt smtp password: %w", err)
			}
			ws.SMTPPasswordEncrypted = &enc
		}

		ws.UpdatedAt = time.Now().UTC()
		ws.UpdatedBy = &updatedBy
		return ws, nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx)
}

// smtpConfig reads the settings row and decrypts the password into a
// mail.SMTPConfig. Returns ok=false when SMTP isn't configured enough to send.
func (s *Service) smtpConfig(ctx context.Context) (mail.SMTPConfig, *settingsdom.WorkspaceSettings, bool, error) {
	ws, err := s.repo.Get(ctx)
	if err != nil {
		return mail.SMTPConfig{}, nil, false, err
	}
	if !ws.SMTPConfigured() {
		return mail.SMTPConfig{}, ws, false, nil
	}
	password := ""
	if ws.SMTPPasswordEncrypted != nil && *ws.SMTPPasswordEncrypted != "" {
		password, err = s.enc.Decrypt(*ws.SMTPPasswordEncrypted)
		if err != nil {
			return mail.SMTPConfig{}, ws, false, fmt.Errorf("email svc: decrypt smtp password: %w", err)
		}
	}
	cfg := mail.SMTPConfig{
		Host:      deref(ws.SMTPHost),
		Port:      derefInt(ws.SMTPPort),
		Username:  deref(ws.SMTPUsername),
		Password:  password,
		FromEmail: deref(ws.SMTPFromEmail),
		FromName:  deref(ws.SMTPFromName),
		UseSSL:     ws.SMTPUseSSL,
		UseTLS:     ws.SMTPUseTLS,
		SkipVerify: ws.SMTPSkipVerify,
	}
	return cfg, ws, true, nil
}

// brandName returns the configured brand name or "Paca".
func brandName(ws *settingsdom.WorkspaceSettings) string {
	if ws != nil && ws.BrandName != nil && *ws.BrandName != "" {
		return *ws.BrandName
	}
	return "Paca"
}

// SendTest sends a test e-mail to `to`, surfacing SMTP errors to the caller
// so the admin can debug their configuration.
func (s *Service) SendTest(ctx context.Context, to string) error {
	cfg, ws, ok, err := s.smtpConfig(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return mail.ErrNotConfigured
	}
	brand := brandName(ws)
	msg := mail.Message{
		To:      to,
		Subject: fmt.Sprintf("%s — SMTP test e-mail", brand),
		HTML: s.wrap(brand, "SMTP test",
			fmt.Sprintf("<p>This is a test e-mail from <strong>%s</strong>.</p>"+
				"<p>If you received it, your SMTP settings are working.</p>", html.EscapeString(brand))),
	}
	return s.sender.Send(cfg, msg)
}

// NotifyUserCreated e-mails credentials to a newly created user. It is a
// no-op (returns sent=false, err=nil) when sending is disabled, SMTP is
// unconfigured, or `to` is empty — so callers can invoke it unconditionally.
func (s *Service) NotifyUserCreated(ctx context.Context, to, username, password string) (bool, error) {
	return s.notifyCredentials(ctx, to, username, password, true)
}

// NotifyPasswordReset e-mails a user their newly reset password, under the
// same enable/config gating as NotifyUserCreated.
func (s *Service) NotifyPasswordReset(ctx context.Context, to, username, password string) (bool, error) {
	return s.notifyCredentials(ctx, to, username, password, false)
}

func (s *Service) notifyCredentials(ctx context.Context, to, username, password string, created bool) (bool, error) {
	if to == "" {
		return false, nil
	}
	cfg, ws, ok, err := s.smtpConfig(ctx)
	if err != nil {
		return false, err
	}
	if !ok || !ws.SendUserCreatedEmail {
		return false, nil
	}

	brand := brandName(ws)
	var subject, lead string
	if created {
		subject = fmt.Sprintf("Your %s account is ready", brand)
		lead = fmt.Sprintf("An account has been created for you on <strong>%s</strong>. Use the credentials below to sign in.", html.EscapeString(brand))
	} else {
		subject = fmt.Sprintf("Your %s password was reset", brand)
		lead = fmt.Sprintf("Your password on <strong>%s</strong> has been reset. Use the new credentials below to sign in.", html.EscapeString(brand))
	}

	var body strings.Builder
	fmt.Fprintf(&body, "<p>%s</p>", lead)
	body.WriteString(`<table cellpadding="0" cellspacing="0" style="margin:16px 0;font-size:14px">`)
	fmt.Fprintf(&body, `<tr><td style="padding:4px 16px 4px 0;color:#6b7280">Username</td><td style="font-weight:600">%s</td></tr>`, html.EscapeString(username))
	fmt.Fprintf(&body, `<tr><td style="padding:4px 16px 4px 0;color:#6b7280">Password</td><td style="font-weight:600;font-family:monospace">%s</td></tr>`, html.EscapeString(password))
	body.WriteString(`</table>`)
	if s.publicURL != "" {
		fmt.Fprintf(&body, `<p><a href="%s" style="display:inline-block;padding:10px 18px;background:#111827;color:#fff;border-radius:8px;text-decoration:none">Sign in</a></p>`, html.EscapeString(s.publicURL))
	}
	body.WriteString(`<p style="color:#6b7280;font-size:13px">For your security, you'll be asked to set a new password the first time you sign in.</p>`)

	msg := mail.Message{To: to, Subject: subject, HTML: s.wrap(brand, subject, body.String())}
	if err := s.sender.Send(cfg, msg); err != nil {
		return false, err
	}
	return true, nil
}

// wrap renders the inner HTML into a minimal, self-contained e-mail shell.
func (s *Service) wrap(brand, title, inner string) string {
	return fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#f3f4f6;padding:24px;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#111827">`+
		`<div style="max-width:520px;margin:0 auto;background:#fff;border-radius:12px;padding:28px 32px;border:1px solid #e5e7eb">`+
		`<h1 style="margin:0 0 4px;font-size:18px">%s</h1>`+
		`<p style="margin:0 0 16px;color:#6b7280;font-size:13px">%s</p>%s</div>`+
		`</body></html>`, html.EscapeString(brand), html.EscapeString(title), inner)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// compile-time check that *secret.Encryptor satisfies Encryptor.
var _ Encryptor = (*secret.Encryptor)(nil)
