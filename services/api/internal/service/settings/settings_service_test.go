package settingssvc_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	settingssvc "github.com/Paca-AI/api/internal/service/settings"
)

// ---------------------------------------------------------------------------
// Minimal fake avatar service — mirrors project_service_test.go's
// fakeAvatarService: CompleteAvatarUpload always returns nextKeys,
// DeleteAvatarObjects records what it was asked to delete.
// ---------------------------------------------------------------------------

type fakeAvatarService struct {
	mu          sync.Mutex
	nextKeys    *attachmentdom.AvatarKeys
	completeErr error
	deletedKeys []string
}

func (f *fakeAvatarService) InitiateAvatarUpload(context.Context, attachmentdom.AvatarUploadInput) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{FileID: uuid.New(), UploadURL: "https://fake/upload"}, nil
}

func (f *fakeAvatarService) CompleteAvatarUpload(context.Context, attachmentdom.AvatarCompleteInput) (*attachmentdom.AvatarKeys, error) {
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return f.nextKeys, nil
}

func (f *fakeAvatarService) ResolveAvatarURL(context.Context, *string) (*string, error) {
	return nil, nil
}

func (f *fakeAvatarService) DeleteAvatarObjects(_ context.Context, keys ...*string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if k != nil && *k != "" {
			f.deletedKeys = append(f.deletedKeys, *k)
		}
	}
}

// ---------------------------------------------------------------------------
// Fake settings repository — a single row, "Get" hands back a copy (like a
// real DB round-trip would) so mutating the returned value never leaks into
// stored state without going through WithLock. WithLock holds r.mu for the
// whole callback, mirroring how the real repository holds the Postgres row
// lock (SELECT ... FOR UPDATE) until its transaction commits — see
// TestWithLock_SerializesConcurrentCallers below.
// ---------------------------------------------------------------------------

type fakeSettingsRepo struct {
	mu     sync.Mutex
	ws     *settingsdom.WorkspaceSettings
	getErr error
}

func newFakeSettingsRepo(ws *settingsdom.WorkspaceSettings) *fakeSettingsRepo {
	return &fakeSettingsRepo{ws: ws}
}

func (r *fakeSettingsRepo) Get(context.Context) (*settingsdom.WorkspaceSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	cp := *r.ws
	return &cp, nil
}

func (r *fakeSettingsRepo) WithLock(_ context.Context, fn func(*settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error)) (*settingsdom.WorkspaceSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	cp := *r.ws
	updated, err := fn(&cp)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return &cp, nil
	}
	stored := *updated
	r.ws = &stored
	return updated, nil
}

// verify *settingssvc.Service satisfies the domain interface.
var _ settingsdom.Service = (*settingssvc.Service)(nil)

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestGet_ReturnsRepoValue(t *testing.T) {
	light := "#5a9e1c"
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{PrimaryColorLight: &light})
	svc := settingssvc.New(repo)

	ws, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ws.PrimaryColorLight == nil || *ws.PrimaryColorLight != light {
		t.Errorf("expected PrimaryColorLight %q, got %v", light, ws.PrimaryColorLight)
	}
}

// ---------------------------------------------------------------------------
// Image upload (logo/favicon)
// ---------------------------------------------------------------------------

func TestInitiateImageUpload_NoAvatarService_ReturnsError(t *testing.T) {
	svc := settingssvc.New(newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})) // WithAvatarService never called
	_, err := svc.InitiateImageUpload(context.Background(), settingsdom.SlotLogo, "logo.png", "image/png", 1024, uuid.New())
	if !errors.Is(err, settingssvc.ErrAvatarServiceRequired) {
		t.Fatalf("expected ErrAvatarServiceRequired, got %v", err)
	}
}

func TestCompleteImageUpload_Logo_SwapsKeysAndDeletesOld_LeavesFaviconUntouched(t *testing.T) {
	ctx := context.Background()
	oldLogoKey, oldLogoThumbKey := "avatars/workspace_logo/.../old-full.png", "avatars/workspace_logo/.../old-thumb.png"
	faviconKey, faviconThumbKey := "avatars/workspace_favicon/.../full.png", "avatars/workspace_favicon/.../thumb.png"
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{
		LogoKey: &oldLogoKey, LogoThumbKey: &oldLogoThumbKey,
		FaviconKey: &faviconKey, FaviconThumbKey: &faviconThumbKey,
	})
	avatarSvc := &fakeAvatarService{
		nextKeys: &attachmentdom.AvatarKeys{Key: "avatars/workspace_logo/.../new-full.png", ThumbKey: "avatars/workspace_logo/.../new-thumb.png"},
	}
	svc := settingssvc.New(repo).WithAvatarService(avatarSvc)

	ws, err := svc.CompleteImageUpload(ctx, settingsdom.SlotLogo, uuid.New())
	if err != nil {
		t.Fatalf("CompleteImageUpload: %v", err)
	}
	if ws.LogoKey == nil || *ws.LogoKey != avatarSvc.nextKeys.Key {
		t.Errorf("expected LogoKey %q, got %v", avatarSvc.nextKeys.Key, ws.LogoKey)
	}
	if ws.LogoThumbKey == nil || *ws.LogoThumbKey != avatarSvc.nextKeys.ThumbKey {
		t.Errorf("expected LogoThumbKey %q, got %v", avatarSvc.nextKeys.ThumbKey, ws.LogoThumbKey)
	}
	// The favicon slot must be untouched by a logo upload.
	if ws.FaviconKey == nil || *ws.FaviconKey != faviconKey {
		t.Errorf("expected FaviconKey unchanged (%q), got %v", faviconKey, ws.FaviconKey)
	}
	if ws.FaviconThumbKey == nil || *ws.FaviconThumbKey != faviconThumbKey {
		t.Errorf("expected FaviconThumbKey unchanged (%q), got %v", faviconThumbKey, ws.FaviconThumbKey)
	}

	stored, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get after complete: %v", err)
	}
	if stored.LogoKey == nil || *stored.LogoKey != avatarSvc.nextKeys.Key {
		t.Errorf("persisted LogoKey not updated, got %v", stored.LogoKey)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 2 {
		t.Fatalf("expected the two old logo keys to be deleted, got %v", avatarSvc.deletedKeys)
	}
	deleted := map[string]bool{avatarSvc.deletedKeys[0]: true, avatarSvc.deletedKeys[1]: true}
	if !deleted[oldLogoKey] || !deleted[oldLogoThumbKey] {
		t.Errorf("expected old logo keys %q/%q to be deleted, got %v", oldLogoKey, oldLogoThumbKey, avatarSvc.deletedKeys)
	}
}

func TestRemoveImage_NoExistingImage_NoOps(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	avatarSvc := &fakeAvatarService{}
	svc := settingssvc.New(repo).WithAvatarService(avatarSvc)

	if _, err := svc.RemoveImage(ctx, settingsdom.SlotFavicon); err != nil {
		t.Fatalf("RemoveImage: %v", err)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 0 {
		t.Errorf("expected no delete calls when favicon has no image, got %v", avatarSvc.deletedKeys)
	}
}

func TestRemoveImage_ClearsKeysAndDeletesObjects(t *testing.T) {
	ctx := context.Background()
	key, thumbKey := "avatars/workspace_favicon/.../full.png", "avatars/workspace_favicon/.../thumb.png"
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{FaviconKey: &key, FaviconThumbKey: &thumbKey})
	avatarSvc := &fakeAvatarService{}
	svc := settingssvc.New(repo).WithAvatarService(avatarSvc)

	ws, err := svc.RemoveImage(ctx, settingsdom.SlotFavicon)
	if err != nil {
		t.Fatalf("RemoveImage: %v", err)
	}
	if ws.FaviconKey != nil || ws.FaviconThumbKey != nil {
		t.Errorf("expected favicon keys cleared, got %v / %v", ws.FaviconKey, ws.FaviconThumbKey)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 2 {
		t.Errorf("expected both keys deleted, got %v", avatarSvc.deletedKeys)
	}
}

// ---------------------------------------------------------------------------
// UpdateSettings
// ---------------------------------------------------------------------------

func TestUpdateSettings_ValidHex_Persists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	svc := settingssvc.New(repo)
	light, dark := "#5a9e1c", "#9ed957"
	updatedBy := uuid.New()

	ws, err := svc.UpdateSettings(ctx, nil, &light, &dark, updatedBy)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if ws.PrimaryColorLight == nil || *ws.PrimaryColorLight != light {
		t.Errorf("expected PrimaryColorLight %q, got %v", light, ws.PrimaryColorLight)
	}
	if ws.PrimaryColorDark == nil || *ws.PrimaryColorDark != dark {
		t.Errorf("expected PrimaryColorDark %q, got %v", dark, ws.PrimaryColorDark)
	}
	if ws.UpdatedBy == nil || *ws.UpdatedBy != updatedBy {
		t.Errorf("expected UpdatedBy %v, got %v", updatedBy, ws.UpdatedBy)
	}
	if ws.UpdatedAt.IsZero() || time.Since(ws.UpdatedAt) > time.Minute {
		t.Errorf("expected UpdatedAt to be set to roughly now, got %v", ws.UpdatedAt)
	}

	stored, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if stored.PrimaryColorLight == nil || *stored.PrimaryColorLight != light {
		t.Errorf("persisted PrimaryColorLight not updated, got %v", stored.PrimaryColorLight)
	}
}

func TestUpdateSettings_InvalidHex_ReturnsErrInvalidColor(t *testing.T) {
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	svc := settingssvc.New(repo)
	bad := "not-a-color"

	_, err := svc.UpdateSettings(context.Background(), nil, &bad, nil, uuid.New())
	if !errors.Is(err, settingsdom.ErrInvalidColor) {
		t.Fatalf("expected ErrInvalidColor, got %v", err)
	}
}

func TestUpdateSettings_EmptyString_ClearsOverride(t *testing.T) {
	ctx := context.Background()
	existing := "#5a9e1c"
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{PrimaryColorLight: &existing})
	svc := settingssvc.New(repo)
	empty := ""

	ws, err := svc.UpdateSettings(ctx, nil, &empty, nil, uuid.New())
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if ws.PrimaryColorLight != nil {
		t.Errorf("expected PrimaryColorLight cleared (nil), got %v", *ws.PrimaryColorLight)
	}
}

func TestUpdateSettings_BrandName_TrimsAndPersists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	svc := settingssvc.New(repo)
	title := "  My Workspace  "

	ws, err := svc.UpdateSettings(ctx, &title, nil, nil, uuid.New())
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if ws.BrandName == nil || *ws.BrandName != "My Workspace" {
		t.Errorf("expected trimmed BrandName %q, got %v", "My Workspace", ws.BrandName)
	}

	stored, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if stored.BrandName == nil || *stored.BrandName != "My Workspace" {
		t.Errorf("persisted BrandName not updated, got %v", stored.BrandName)
	}
}

func TestUpdateSettings_BrandName_EmptyClearsOverride(t *testing.T) {
	ctx := context.Background()
	existing := "My Workspace"
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{BrandName: &existing})
	svc := settingssvc.New(repo)
	empty := ""

	ws, err := svc.UpdateSettings(ctx, &empty, nil, nil, uuid.New())
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if ws.BrandName != nil {
		t.Errorf("expected BrandName cleared (nil), got %v", *ws.BrandName)
	}
}

func TestUpdateSettings_BrandName_TooLong_ReturnsErrBrandNameTooLong(t *testing.T) {
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	svc := settingssvc.New(repo)
	tooLong := strings.Repeat("a", 101)

	_, err := svc.UpdateSettings(context.Background(), &tooLong, nil, nil, uuid.New())
	if !errors.Is(err, settingsdom.ErrBrandNameTooLong) {
		t.Fatalf("expected ErrBrandNameTooLong, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent mutations
// ---------------------------------------------------------------------------

// TestWithLock_SerializesConcurrentCallers runs a logo upload and a
// brand-name/color update against the same row concurrently, many times
// over. Before CompleteImageUpload/RemoveImage/UpdateSettings were rewritten
// to go through Repository.WithLock, each did an unlocked Get-then-Update:
// whichever call's Update landed second would overwrite the row with its own
// stale in-memory copy, silently discarding the first call's change. This
// asserts that after every concurrent round, both writes are visible.
func TestWithLock_SerializesConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	repo := newFakeSettingsRepo(&settingsdom.WorkspaceSettings{})
	avatarSvc := &fakeAvatarService{
		nextKeys: &attachmentdom.AvatarKeys{Key: "avatars/workspace_logo/.../full.png", ThumbKey: "avatars/workspace_logo/.../thumb.png"},
	}
	svc := settingssvc.New(repo).WithAvatarService(avatarSvc)

	const rounds = 100
	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.CompleteImageUpload(ctx, settingsdom.SlotLogo, uuid.New()); err != nil {
				t.Errorf("round %d: CompleteImageUpload: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			brandName, light := "My Workspace", "#5a9e1c"
			if _, err := svc.UpdateSettings(ctx, &brandName, &light, nil, uuid.New()); err != nil {
				t.Errorf("round %d: UpdateSettings: %v", i, err)
			}
		}()
		wg.Wait()

		ws, err := repo.Get(ctx)
		if err != nil {
			t.Fatalf("round %d: Get: %v", i, err)
		}
		if ws.LogoKey == nil {
			t.Fatalf("round %d: logo upload was lost (LogoKey nil after concurrent UpdateSettings)", i)
		}
		if ws.BrandName == nil {
			t.Fatalf("round %d: brand name update was lost (BrandName nil after concurrent CompleteImageUpload)", i)
		}
	}
}
