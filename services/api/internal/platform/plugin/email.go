package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// -------------------------------------------------------------------------
// Outbound SMTP send function
// -------------------------------------------------------------------------

// smtpDialTimeout bounds how long send_email waits to connect + hand off
// the message before giving up, so a slow/unreachable mail server can't
// stall a plugin call indefinitely.
const smtpDialTimeout = 15 * time.Second

// smtpSendRequest is the JSON payload a plugin sends to paca.send_email.
type smtpSendRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	// UseTLS selects implicit TLS on connect (typically port 465). When
	// false, the host still opportunistically upgrades via STARTTLS
	// (typically port 587/25) if the server offers it.
	UseTLS   bool   `json:"use_tls"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	To       string `json:"to"`
	ToName   string `json:"to_name"`
	Subject  string `json:"subject"`
	HTMLBody string `json:"html_body"`
	TextBody string `json:"text_body"`
}

func (req smtpSendRequest) validate() error {
	if req.Host == "" || req.Port == 0 {
		return fmt.Errorf("host and port are required")
	}
	if req.From == "" || req.To == "" {
		return fmt.Errorf("from and to are required")
	}
	// From/To are written verbatim into MIME headers and passed as-is to
	// the SMTP envelope (MAIL FROM/RCPT TO); requiring a valid RFC 5322
	// address here rejects the CR/LF a malicious plugin could otherwise use
	// to inject extra headers or SMTP commands.
	if _, err := mail.ParseAddress(req.From); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	if _, err := mail.ParseAddress(req.To); err != nil {
		return fmt.Errorf("invalid to address: %w", err)
	}
	if req.HTMLBody == "" && req.TextBody == "" {
		return fmt.Errorf("html_body or text_body is required")
	}
	return nil
}

// registerEmailFunction adds paca.send_email to the host module builder.
// This is the only way a WASM plugin can reach outside the sandbox other
// than paca.fetch (HTTPS to an allowlisted domain) — genuine SMTP requires a
// raw TCP/TLS socket, which WASI does not expose to guests, so the host
// dials it directly, unsandboxed, on the plugin's behalf. Unlike fetch there
// is no domain allowlist to constrain the target (the SMTP host is
// admin-configured plugin data, not caller-supplied request input), so this
// is instead restricted to plugins that explicitly declare the "email.send"
// permission in their manifest.
func (r *Runtime) registerEmailFunction(b wazero.HostModuleBuilder, p plugindom.Plugin) {
	// paca.send_email(reqPtr, reqLen, resPtrPtr, resLenPtr)
	// Request JSON: smtpSendRequest.
	// Response JSON: {"error":""} on success, {"error":"<msg>"} on failure.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			writeBack := func(ptrLen []uint64) {
				m.Memory().WriteUint32Le(uint32(stack[2]), uint32(ptrLen[0]))
				m.Memory().WriteUint32Le(uint32(stack[3]), uint32(ptrLen[1]))
			}
			writeErr := func(msg string) {
				writeBack(writeJSONResult(m, map[string]string{"error": msg}))
			}

			if !hasPermission(p.Manifest.Permissions, "email.send") {
				writeErr("send_email: plugin manifest does not declare the email.send permission")
				return
			}

			reqBytes, err := readFromMemory(m, stack[0], stack[1])
			if err != nil {
				writeErr("send_email: read request: " + err.Error())
				return
			}
			var req smtpSendRequest
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				writeErr("send_email: decode request: " + err.Error())
				return
			}
			if err := req.validate(); err != nil {
				writeErr("send_email: " + err.Error())
				return
			}

			sendCtx, cancel := context.WithTimeout(ctx, smtpDialTimeout)
			defer cancel()
			if err := dialAndSendSMTP(sendCtx, req); err != nil {
				writeErr(err.Error())
				return
			}
			writeBack(writeJSONResult(m, map[string]string{"error": ""}))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64}, nil).
		Export("send_email")
}

// hasPermission reports whether want is present in perms.
func hasPermission(perms []string, want string) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}

// dialAndSendSMTP connects to req.Host:req.Port and sends the message via
// the standard SMTP conversation (EHLO, optional STARTTLS, optional AUTH,
// MAIL FROM/RCPT TO/DATA). Uses only the standard library: net/smtp plus
// crypto/tls for both the implicit-TLS (port 465 style) and STARTTLS
// (port 587/25 style) cases.
func dialAndSendSMTP(ctx context.Context, req smtpSendRequest) error {
	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))
	dialer := &net.Dialer{Timeout: smtpDialTimeout}

	var conn net.Conn
	var err error
	if req.UseTLS {
		tlsDialer := tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: req.Host}}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("send_email: dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, req.Host)
	if err != nil {
		return fmt.Errorf("send_email: smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if !req.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: req.Host}); err != nil {
				return fmt.Errorf("send_email: starttls: %w", err)
			}
		}
	}

	if req.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", req.Username, req.Password, req.Host)); err != nil {
			return fmt.Errorf("send_email: auth: %w", err)
		}
	}

	if err := client.Mail(req.From); err != nil {
		return fmt.Errorf("send_email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(req.To); err != nil {
		return fmt.Errorf("send_email: RCPT TO: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("send_email: DATA: %w", err)
	}
	if _, err := wc.Write(buildMIMEMessage(req)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("send_email: write message: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("send_email: close message: %w", err)
	}

	return client.Quit()
}

// buildMIMEMessage renders a minimal RFC 5322 message with a
// multipart/alternative text+HTML body (or a single part when only one is
// supplied).
func buildMIMEMessage(req smtpSendRequest) []byte {
	var buf bytes.Buffer

	from := req.From
	if req.FromName != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", req.FromName), req.From)
	}
	to := req.To
	if req.ToName != "" {
		to = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", req.ToName), req.To)
	}

	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", req.Subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")

	switch {
	case req.HTMLBody != "" && req.TextBody != "":
		boundary := "paca-" + randomBoundary()
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"utf-8\"\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n\r\n", req.TextBody)
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: text/html; charset=\"utf-8\"\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n\r\n", req.HTMLBody)
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	case req.HTMLBody != "":
		fmt.Fprintf(&buf, "Content-Type: text/html; charset=\"utf-8\"\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", req.HTMLBody)
	default:
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=\"utf-8\"\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", req.TextBody)
	}

	return buf.Bytes()
}

// randomBoundary returns a random hex string suitable for a MIME boundary.
func randomBoundary() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
