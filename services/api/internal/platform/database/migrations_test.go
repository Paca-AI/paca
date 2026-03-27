package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrations-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestRunMigrations_Success(t *testing.T) {
	db := openSQLite(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "0001_create.sql"), []byte("CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT);"), 0o644); err != nil {
		t.Fatalf("write migration 1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0002_seed.sql"), []byte("INSERT INTO users(name) VALUES('alice');"), 0o644); err != nil {
		t.Fatalf("write migration 2: %v", err)
	}

	if err := RunMigrations(db, dir); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}

	var count int64
	if err := db.Table("users").Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 seeded user, got %d", count)
	}
}

func TestRunMigrations_MissingDir(t *testing.T) {
	db := openSQLite(t)
	err := RunMigrations(db, filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing migration dir")
	}
	if !strings.Contains(err.Error(), "migrations: read dir") {
		t.Fatalf("expected read dir error, got %v", err)
	}
}

func TestRunMigrations_InvalidSQL(t *testing.T) {
	db := openSQLite(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "0001_bad.sql"), []byte("this is not sql"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	err := RunMigrations(db, dir)
	if err == nil {
		t.Fatal("expected SQL execution error")
	}
	if !strings.Contains(err.Error(), "migrations: exec") {
		t.Fatalf("expected exec wrapper error, got %v", err)
	}
}
