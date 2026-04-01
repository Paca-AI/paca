package postgres

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/paca/api/internal/platform/authz"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type projectRoleTestRecord struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	ProjectID   *string
	RoleName    string `gorm:"column:role_name"`
	Permissions []byte `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (projectRoleTestRecord) TableName() string { return "project_roles" }

type projectMemberTestRecord struct {
	ID            string `gorm:"primaryKey;type:uuid"`
	ProjectID     string `gorm:"type:uuid;column:project_id"`
	UserID        string `gorm:"type:uuid;column:user_id"`
	ProjectRoleID string `gorm:"type:uuid;column:project_role_id"`
}

func (projectMemberTestRecord) TableName() string { return "project_members" }

func openAuthzStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "authz-store-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&userRecord{}, &globalRoleRecord{}, &projectRoleTestRecord{}, &projectMemberTestRecord{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestAuthzPermissionStore_ListGlobalPermissions(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)

	roleID := uuid.New().String()
	if err := db.Create(&globalRoleRecord{
		ID:          roleID,
		Name:        "ADMIN",
		Permissions: []byte(`{"users.delete":true,"global_roles.write":true}`),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed role: %v", err)
	}

	// Seed user whose role_id points directly to the role (new schema).
	userID := uuid.New()
	user := &userRecord{ID: userID.String(), Username: "alice", PasswordHash: "hash", FullName: "Alice", RoleID: roleID}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	perms, err := store.ListGlobalPermissions(context.Background(), userID)
	if err != nil {
		t.Fatalf("list global permissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d (%v)", len(perms), perms)
	}
}

func TestAuthzPermissionStore_ListProjectPermissions(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)

	userID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()

	user := &userRecord{ID: userID.String(), Username: "alice", PasswordHash: "hash", FullName: "Alice", RoleID: uuid.New().String()}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := db.Create(&projectRoleTestRecord{
		ID:          roleID.String(),
		ProjectID:   nil,
		RoleName:    "PROJECT_MANAGER",
		Permissions: []byte(`{"tasks.write":true,"tasks.read":true}`),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed project role: %v", err)
	}

	if err := db.Create(&projectMemberTestRecord{
		ID:            uuid.New().String(),
		ProjectID:     projectID.String(),
		UserID:        userID.String(),
		ProjectRoleID: roleID.String(),
	}).Error; err != nil {
		t.Fatalf("seed project member: %v", err)
	}

	perms, err := store.ListProjectPermissions(context.Background(), userID, projectID)
	if err != nil {
		t.Fatalf("list project permissions: %v", err)
	}

	foundRead := false
	foundWrite := false
	for _, p := range perms {
		if p == authz.PermissionTasksRead {
			foundRead = true
		}
		if p == authz.PermissionTasksWrite {
			foundWrite = true
		}
	}
	if !foundRead || !foundWrite {
		t.Fatalf("expected tasks.read and tasks.write, got %v", perms)
	}
}
