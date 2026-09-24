package projectsvc

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/secret"
)

func strPtr(s string) *string { return &s }

func seedJevProject(repo *fakeProjectRepo) uuid.UUID {
	id := uuid.New()
	repo.projects[id] = &projectdom.Project{
		ID:              id,
		Name:            "Jev",
		JevAPIKeySecret: "old-key",
		JevBaseURL:      "https://old.example",
		JevModel:        "old-model",
	}
	return id
}

func TestUpdateJevConfig_NilFieldsLeaveValuesUnchanged(t *testing.T) {
	repo := newFakeProjectRepo()
	id := seedJevProject(repo)
	svc := New(repo, nil, nil)

	p, err := svc.UpdateJevConfig(context.Background(), id, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.JevAPIKeySecret != "old-key" || p.JevBaseURL != "https://old.example" || p.JevModel != "old-model" {
		t.Errorf("expected unchanged config, got %+v", p)
	}
}

func TestUpdateJevConfig_TrimsAndStoresPlaintextWithoutEncryptor(t *testing.T) {
	repo := newFakeProjectRepo()
	id := seedJevProject(repo)
	svc := New(repo, nil, nil)

	p, err := svc.UpdateJevConfig(context.Background(), id, strPtr("  new-key  "), strPtr(" https://new.example "), strPtr(" m2 "))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.JevAPIKeySecret != "new-key" || p.JevBaseURL != "https://new.example" || p.JevModel != "m2" {
		t.Errorf("unexpected returned config: %+v", p)
	}
	stored := repo.projects[id]
	if stored.JevAPIKeySecret != "new-key" || stored.JevBaseURL != "https://new.example" || stored.JevModel != "m2" {
		t.Errorf("unexpected stored config: %+v", stored)
	}
}

func TestUpdateJevConfig_EmptyKeyClearsConfiguration(t *testing.T) {
	repo := newFakeProjectRepo()
	id := seedJevProject(repo)
	enc, err := secret.NewEncryptor(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	svc := New(repo, nil, nil).WithEncryptor(enc)

	p, err := svc.UpdateJevConfig(context.Background(), id, strPtr("   "), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.JevConfigured() {
		t.Errorf("an empty key must clear Jev, even with an encryptor configured; stored %q", p.JevAPIKeySecret)
	}
}

func TestUpdateJevConfig_EncryptsKeyAtRest(t *testing.T) {
	repo := newFakeProjectRepo()
	id := seedJevProject(repo)
	enc, err := secret.NewEncryptor(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	svc := New(repo, nil, nil).WithEncryptor(enc)

	if _, err := svc.UpdateJevConfig(context.Background(), id, strPtr("plain-secret"), nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored := repo.projects[id].JevAPIKeySecret
	if stored == "plain-secret" || stored == "" {
		t.Fatalf("expected the key to be encrypted at rest, got %q", stored)
	}
	decrypted, err := enc.Decrypt(stored)
	if err != nil || decrypted != "plain-secret" {
		t.Errorf("stored key does not decrypt back: %q, %v", decrypted, err)
	}
}

func TestUpdateJevConfig_ProjectNotFound(t *testing.T) {
	svc := New(newFakeProjectRepo(), nil, nil)
	_, err := svc.UpdateJevConfig(context.Background(), uuid.New(), strPtr("k"), nil, nil)
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
