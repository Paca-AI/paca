package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

type fakeMFASvc struct {
	MFAService
	begun, confirmed bool
	cidrs            []string
}

func (f *fakeMFASvc) Begin(context.Context, *userdom.User) (string, string, error) {
	f.begun = true
	return "SECRET", "otpauth://totp/x", nil
}
func (f *fakeMFASvc) Confirm(context.Context, *userdom.User, string) ([]string, error) {
	f.confirmed = true
	return []string{"aaaaa-bbbbb"}, nil
}
func (f *fakeMFASvc) SetUserCIDRs(_ context.Context, _ uuid.UUID, c []string) error {
	f.cidrs = c
	return nil
}
func (f *fakeMFASvc) GetUserCIDRs(context.Context, uuid.UUID) ([]string, error) { return f.cidrs, nil }
func (f *fakeMFASvc) GetPolicy(context.Context) (*mfadom.Policy, error)         { return &mfadom.Policy{}, nil }

type fakeUsers struct {
	userdom.Repository
	u *userdom.User
}

func (f fakeUsers) FindByUsername(context.Context, string) (*userdom.User, error) {
	if f.u == nil {
		return nil, userdom.ErrNotFound
	}
	return f.u, nil
}

func newMFAHandler(t *testing.T) (*MFAHandler, *fakeMFASvc) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	svc := &fakeMFASvc{}
	return NewMFAHandler(svc, fakeUsers{u: &userdom.User{ID: uuid.New(), Username: "root", PasswordHash: string(hash)}}), svc
}

func post(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestMFASetup_RequiresCorrectPassword(t *testing.T) {
	h, svc := newMFAHandler(t)

	rec := post(h.SetupBegin, `{"username":"root","password":"wrongpass1"}`)
	if rec.Code != http.StatusUnauthorized || svc.begun {
		t.Fatalf("wrong password: status %d begun=%v", rec.Code, svc.begun)
	}
	rec = post(h.SetupBegin, `{"username":"root","password":"secret123"}`)
	if rec.Code != http.StatusOK || !svc.begun || !strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("correct password: status %d body %s", rec.Code, rec.Body)
	}
}

func TestMFASetupConfirm_NeedsCodeAndPassword(t *testing.T) {
	h, svc := newMFAHandler(t)

	if rec := post(h.SetupConfirm, `{"username":"root","password":"secret123"}`); rec.Code != http.StatusBadRequest || svc.confirmed {
		t.Fatalf("missing code: status %d", rec.Code)
	}
	if rec := post(h.SetupConfirm, `{"username":"root","password":"nope12345","code":"123456"}`); rec.Code != http.StatusUnauthorized || svc.confirmed {
		t.Fatalf("wrong password: status %d", rec.Code)
	}
	rec := post(h.SetupConfirm, `{"username":"root","password":"secret123","code":"123456"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aaaaa-bbbbb") {
		t.Fatalf("confirm: status %d body %s", rec.Code, rec.Body)
	}
}
