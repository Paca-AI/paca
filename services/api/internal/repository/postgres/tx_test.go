package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tx-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("CREATE TABLE tx_values (v TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	db := openTxTestDB(t)

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO tx_values(v) VALUES (?)", "ok").Error
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var count int64
	if err := db.Table("tx_values").Where("v = ?", "ok").Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected committed row, got %d", count)
	}
}

func TestWithTx_RollsBackAndWrapsError(t *testing.T) {
	db := openTxTestDB(t)
	fnErr := errors.New("boom")

	err := WithTx(context.Background(), db, func(tx *gorm.DB) error {
		if e := tx.Exec("INSERT INTO tx_values(v) VALUES (?)", "rollback").Error; e != nil {
			return e
		}
		return fnErr
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tx: boom") {
		t.Fatalf("expected wrapped tx error, got %v", err)
	}

	var count int64
	if err := db.Table("tx_values").Where("v = ?", "rollback").Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected rollback, found %d rows", count)
	}
}
