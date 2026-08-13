package email

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/platform/mail"
)

// --- fakes -----------------------------------------------------------------

type fakeRepo struct{ ws *settingsdom.WorkspaceSettings }

func (r *fakeRepo) Get(_ context.Context) (*settingsdom.WorkspaceSettings, error) {
	cp := *r.ws
	return &cp, nil
}

func (r *fakeRepo) WithLock(_ context.Context, fn func(*settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error)) (*settingsdom.WorkspaceSettings, error) {
	updated, err := fn(r.ws)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		r.ws = updated
	}
	return r.ws, nil
}

type fakeEnc struct{}

func (fakeEnc) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (fakeEnc) Decrypt(e string) (string, error) { return strings.TrimPrefix(e, "enc:"), nil }

type fakeSender struct {
	calls   int
	lastCfg mail.SMTPConfig
	lastMsg mail.Message
}

func (s *fakeSender) Send(cfg mail.SMTPConfig, msg mail.Message) error {
	s.calls++
	s.lastCfg = cfg
	s.lastMsg = msg
	return nil
}

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

func configured() *settingsdom.WorkspaceSettings {
	return &settingsdom.WorkspaceSettings{
		SMTPHost:              strp("smtp.example.com"),
		SMTPPort:              intp(587),
		SMTPFromEmail:         strp("no-reply@example.com"),
		SMTPPasswordEncrypted: strp("enc:secret"),
		SMTPUseTLS:            true,
		SendUserCreatedEmail:  true,
	}
}

// --- tests -----------------------------------------------------------------

func TestUpdate_EncryptsPasswordAndDoesNotLeakIt(t *testing.T) {
	repo := &fakeRepo{ws: &settingsdom.WorkspaceSettings{}}
	svc := NewService(repo, fakeEnc{}, &fakeSender{}, "https://paca.example.com")

	view, err := svc.Update(context.Background(), UpdateInput{
		FromEmail: "no-reply@example.com", Host: "smtp.example.com", Port: 587,
		Username: "u", Password: strp("secret"), UseTLS: true, SendUserCreatedEmail: true,
	}, uuid.New())
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if repo.ws.SMTPPasswordEncrypted == nil || *repo.ws.SMTPPasswordEncrypted != "enc:secret" {
		t.Errorf("password not stored encrypted: %v", repo.ws.SMTPPasswordEncrypted)
	}
	if !view.PasswordSet || !view.Configured {
		t.Errorf("view should report PasswordSet and Configured, got %+v", view)
	}
}

func TestUpdate_NilPasswordKeepsExisting(t *testing.T) {
	repo := &fakeRepo{ws: configured()}
	svc := NewService(repo, fakeEnc{}, &fakeSender{}, "")
	if _, err := svc.Update(context.Background(), UpdateInput{
		FromEmail: "no-reply@example.com", Host: "smtp.example.com", Port: 587,
		Password: nil, SendUserCreatedEmail: true,
	}, uuid.New()); err != nil {
		t.Fatalf("update: %v", err)
	}
	if repo.ws.SMTPPasswordEncrypted == nil || *repo.ws.SMTPPasswordEncrypted != "enc:secret" {
		t.Errorf("nil password should keep existing, got %v", repo.ws.SMTPPasswordEncrypted)
	}
}

func TestNotifyUserCreated_DisabledIsNoOp(t *testing.T) {
	ws := configured()
	ws.SendUserCreatedEmail = false
	sender := &fakeSender{}
	svc := NewService(&fakeRepo{ws: ws}, fakeEnc{}, sender, "")

	sent, err := svc.NotifyUserCreated(context.Background(), "u@x.com", "user", "pw")
	if err != nil || sent {
		t.Fatalf("want (false,nil), got (%v,%v)", sent, err)
	}
	if sender.calls != 0 {
		t.Errorf("sender should not be called when disabled")
	}
}

func TestNotifyUserCreated_UnconfiguredIsNoOp(t *testing.T) {
	sender := &fakeSender{}
	svc := NewService(&fakeRepo{ws: &settingsdom.WorkspaceSettings{SendUserCreatedEmail: true}}, fakeEnc{}, sender, "")

	sent, err := svc.NotifyUserCreated(context.Background(), "u@x.com", "user", "pw")
	if err != nil || sent {
		t.Fatalf("want (false,nil), got (%v,%v)", sent, err)
	}
	if sender.calls != 0 {
		t.Errorf("sender should not be called when unconfigured")
	}
}

func TestNotifyUserCreated_SendsWithDecryptedPasswordAndCredentials(t *testing.T) {
	sender := &fakeSender{}
	svc := NewService(&fakeRepo{ws: configured()}, fakeEnc{}, sender, "https://paca.example.com")

	sent, err := svc.NotifyUserCreated(context.Background(), "u@x.com", "alice", "TempPass1")
	if err != nil || !sent {
		t.Fatalf("want (true,nil), got (%v,%v)", sent, err)
	}
	if sender.calls != 1 {
		t.Fatalf("expected one send, got %d", sender.calls)
	}
	if sender.lastCfg.Password != "secret" {
		t.Errorf("cfg password should be decrypted, got %q", sender.lastCfg.Password)
	}
	if sender.lastMsg.To != "u@x.com" {
		t.Errorf("wrong recipient: %q", sender.lastMsg.To)
	}
	if !strings.Contains(sender.lastMsg.HTML, "alice") || !strings.Contains(sender.lastMsg.HTML, "TempPass1") {
		t.Errorf("email body missing credentials:\n%s", sender.lastMsg.HTML)
	}
}

func TestNotifyUserCreated_EmptyRecipientIsNoOp(t *testing.T) {
	sender := &fakeSender{}
	svc := NewService(&fakeRepo{ws: configured()}, fakeEnc{}, sender, "")
	sent, err := svc.NotifyUserCreated(context.Background(), "", "user", "pw")
	if err != nil || sent || sender.calls != 0 {
		t.Fatalf("empty recipient should be a no-op, got (%v,%v,calls=%d)", sent, err, sender.calls)
	}
}

func TestSendTest_Unconfigured_ReturnsErr(t *testing.T) {
	svc := NewService(&fakeRepo{ws: &settingsdom.WorkspaceSettings{}}, fakeEnc{}, &fakeSender{}, "")
	if err := svc.SendTest(context.Background(), "admin@x.com"); err != mail.ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}
