package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

// VacuumResult reports a compaction.
type VacuumResult struct {
	Before, After int64
	// PreviousDB is the replaced file, kept only when asked.
	PreviousDB string
}

// Vacuum rewrites the stopped server's database compactly (Change 38, docs/SPEC.md §4f): VACUUM
// INTO a new file next to it, a quick check of the copy, then the same rename swap as Restore. The
// copy is built on disk, not in memory (temp_store is MEMORY), so it needs free space about the
// database's size. The replaced file is removed unless keep is set.
func Vacuum(ctx context.Context, dbPath string, keep bool, now time.Time) (VacuumResult, error) {
	info, err := os.Stat(dbPath)
	if err != nil {
		return VacuumResult{}, fmt.Errorf("backup: no database at %s: %w", dbPath, err)
	}
	if err := ensureUnlocked(dbPath); err != nil {
		return VacuumResult{}, err
	}
	stamp := now.UTC().Format("20060102T150405Z")
	compact := fmt.Sprintf("%s.vacuum-%s", dbPath, stamp)
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		return VacuumResult{}, err
	}
	_, err = sqlDB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if err == nil {
		_, err = sqlDB.ExecContext(ctx, `VACUUM INTO ?`, compact)
	}
	_ = sqlDB.Close()
	if err != nil {
		_ = os.Remove(compact)
		return VacuumResult{}, fmt.Errorf("backup: vacuum: %w", err)
	}
	if err := quickCheck(ctx, compact); err != nil {
		_ = os.Remove(compact)
		return VacuumResult{}, err
	}
	after, err := os.Stat(compact)
	if err != nil {
		return VacuumResult{}, err
	}
	res := VacuumResult{Before: info.Size(), After: after.Size()}
	previous := fmt.Sprintf("%s.pre-vacuum-%s", dbPath, stamp)
	if err := os.Rename(dbPath, previous); err != nil {
		_ = os.Remove(compact)
		return VacuumResult{}, fmt.Errorf("backup: set the old database aside: %w", err)
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := os.Chmod(compact, 0o600); err != nil {
		return VacuumResult{}, err
	}
	if err := os.Rename(compact, dbPath); err != nil {
		return VacuumResult{}, fmt.Errorf("backup: install the compacted database (the old one is %s): %w", previous, err)
	}
	if d, err := os.Open(filepath.Dir(dbPath)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	if keep {
		res.PreviousDB = previous
	} else if err := os.Remove(previous); err != nil {
		res.PreviousDB = previous
	}
	// Statistics for the planner on the compacted file.
	if sqlDB, err := db.Open(dbPath); err == nil {
		_ = db.Optimize(ctx, sqlDB)
		_ = sqlDB.Close()
	}
	return res, nil
}

func quickCheck(ctx context.Context, path string) error {
	sqlDB, err := db.Open(path)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	var result string
	if err := sqlDB.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("backup: check the compacted copy: %w", err)
	}
	if result != "ok" {
		return errors.New("backup: the compacted copy failed its check: " + result)
	}
	return nil
}
