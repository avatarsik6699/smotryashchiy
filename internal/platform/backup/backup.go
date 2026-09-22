// Package backup creates and restores consistent snapshots of the SQLite database
// (docs/SPEC.md §4f). A bundle is a .tar.gz holding manifest.json and smotryashchiy.db.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

const (
	manifestName = "manifest.json"
	dbName       = "smotryashchiy.db"
	// FormatVersion is bumped when the bundle layout changes incompatibly.
	FormatVersion = 1

	maxManifestBytes = 1 << 20
	maxDBBytes       = 64 << 30 // sanity cap against a decompression bomb
)

// Manifest describes a bundle.
type Manifest struct {
	Format     int       `json:"format"`
	CreatedAt  time.Time `json:"created_at"`
	Release    string    `json:"release"`
	Migrations int       `json:"migrations"`
	DBSHA256   string    `json:"db_sha256"`
	DBBytes    int64     `json:"db_bytes"`
}

// Create writes a consistent snapshot of the open database to w as a bundle. It works on a database
// that a running server is writing to (VACUUM INTO reads one consistent transaction). workDir must be
// a writable directory (the database directory is the right choice: it is writable even when the root
// filesystem is read-only); the temporary snapshot is removed before returning.
func Create(ctx context.Context, src *sql.DB, release, workDir string, w io.Writer, now time.Time) (Manifest, error) {
	work, err := os.MkdirTemp(workDir, ".backup-work-")
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: create work dir: %w", err)
	}
	defer os.RemoveAll(work)
	snapshot := filepath.Join(work, dbName)
	if _, err := src.ExecContext(ctx, "VACUUM INTO "+quoteSQL(snapshot)); err != nil {
		return Manifest{}, fmt.Errorf("backup: snapshot the database: %w", err)
	}
	m, err := inspect(ctx, snapshot)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: verify the snapshot: %w", err)
	}
	m.Format, m.CreatedAt, m.Release = FormatVersion, now.UTC().Truncate(time.Second), release
	if err := writeBundle(w, m, snapshot); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// CreateFile is Create into a new file at outPath. The bundle holds secrets, so the file is created with
// mode 0600 and an existing file is never overwritten; a failed backup leaves nothing behind.
func CreateFile(ctx context.Context, src *sql.DB, release, workDir, outPath string, now time.Time) (Manifest, error) {
	out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: create %s: %w", outPath, err)
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(outPath)
		}
	}()
	m, err := Create(ctx, src, release, workDir, out, now)
	if err != nil {
		return Manifest{}, err
	}
	if err := out.Sync(); err != nil {
		return Manifest{}, fmt.Errorf("backup: sync: %w", err)
	}
	if err := out.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close: %w", err)
	}
	ok = true
	return m, nil
}

func absDir(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// quoteSQL quotes a string literal for SQLite (VACUUM INTO does not take a bound parameter).
func quoteSQL(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// inspect opens a database file read-only, runs the integrity check and returns its manifest fields.
func inspect(ctx context.Context, path string) (Manifest, error) {
	sum, size, err := hashFile(path)
	if err != nil {
		return Manifest{}, err
	}
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(0)")
	if err != nil {
		return Manifest{}, err
	}
	defer conn.Close()
	var verdict string
	if err := conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&verdict); err != nil {
		return Manifest{}, fmt.Errorf("integrity check: %w", err)
	}
	if verdict != "ok" {
		return Manifest{}, fmt.Errorf("integrity check failed: %s", verdict)
	}
	n, err := db.AppliedMigrations(conn)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{Migrations: n, DBSHA256: sum, DBBytes: size}, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeBundle(w io.Writer, m Manifest, snapshot string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	manifest, _ := json.MarshalIndent(m, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(manifest)), ModTime: m.CreatedAt}); err != nil {
		return fmt.Errorf("backup: write manifest header: %w", err)
	}
	if _, err := tw.Write(manifest); err != nil {
		return fmt.Errorf("backup: write manifest: %w", err)
	}
	f, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{Name: dbName, Mode: 0o600, Size: m.DBBytes, ModTime: m.CreatedAt}); err != nil {
		return fmt.Errorf("backup: write database header: %w", err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("backup: write database: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: finish archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("backup: finish compression: %w", err)
	}
	return nil
}

// RestoreOptions control a restore.
type RestoreOptions struct {
	// Force allows replacing an existing non-empty database (it is kept aside, never deleted).
	Force bool
	// MaxMigrations is the migration count of the running binary; a newer bundle is refused.
	MaxMigrations int
	Now           time.Time
}

// RestoreResult reports what a restore did.
type RestoreResult struct {
	Manifest Manifest
	// PreviousDB is where the replaced database was kept, empty when there was none.
	PreviousDB string
}

// Restore verifies the bundle read from `bundle` and atomically replaces the database at dbPath with its snapshot.
func Restore(ctx context.Context, bundle io.Reader, dbPath string, opts RestoreOptions) (RestoreResult, error) {
	dir := filepath.Dir(absDir(dbPath))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return RestoreResult{}, fmt.Errorf("backup: create data dir: %w", err)
	}
	work, err := os.MkdirTemp(dir, ".restore-work-")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: create work dir: %w", err)
	}
	defer os.RemoveAll(work)

	m, staged, err := extract(bundle, work)
	if err != nil {
		return RestoreResult{}, err
	}
	if m.Format != FormatVersion {
		return RestoreResult{}, fmt.Errorf("backup: unsupported bundle format %d (this binary reads %d)", m.Format, FormatVersion)
	}
	got, err := inspect(ctx, staged)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: bundle database is not usable: %w", err)
	}
	if got.DBSHA256 != m.DBSHA256 {
		return RestoreResult{}, errors.New("backup: the database in the bundle does not match its manifest hash (corrupted or tampered)")
	}
	if got.Migrations > opts.MaxMigrations {
		return RestoreResult{}, fmt.Errorf("backup: the bundle has %d migrations but this binary knows %d; upgrade the binary first", got.Migrations, opts.MaxMigrations)
	}

	res := RestoreResult{Manifest: m}
	if info, err := os.Stat(dbPath); err == nil && info.Size() > 0 {
		if !opts.Force {
			return RestoreResult{}, fmt.Errorf("backup: %s already holds a database; use --force to replace it (the old one is kept)", dbPath)
		}
		if err := ensureUnlocked(dbPath); err != nil {
			return RestoreResult{}, err
		}
		res.PreviousDB = fmt.Sprintf("%s.pre-restore-%s", dbPath, opts.Now.UTC().Format("20060102T150405Z"))
		if err := os.Rename(dbPath, res.PreviousDB); err != nil {
			return RestoreResult{}, fmt.Errorf("backup: keep the previous database: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
	}
	// Stale WAL/SHM files of the replaced database would corrupt the restored one.
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := os.Chmod(staged, 0o600); err != nil {
		return RestoreResult{}, err
	}
	if err := os.Rename(staged, dbPath); err != nil {
		return RestoreResult{}, fmt.Errorf("backup: install the restored database: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return res, nil
}

// ensureUnlocked fails when another process (a running server) has the database open.
func ensureUnlocked(path string) error {
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)&_pragma=locking_mode(EXCLUSIVE)")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("backup: the database is in use (is the server still running? stop it first): %w", err)
	}
	_, _ = conn.Exec("ROLLBACK")
	return nil
}

// extract reads exactly manifest.json and smotryashchiy.db from the bundle into dir; any other entry,
// path or an oversized member is an error.
func extract(bundle io.Reader, dir string) (Manifest, string, error) {
	gz, err := gzip.NewReader(bundle)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("backup: the input is not a gzip bundle: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var (
		m         Manifest
		haveMan   bool
		haveDB    bool
		stagedDB  = filepath.Join(dir, dbName)
		extracted int
	)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, "", fmt.Errorf("backup: read bundle: %w", err)
		}
		extracted++
		switch h.Name {
		case manifestName:
			raw, err := io.ReadAll(io.LimitReader(tr, maxManifestBytes+1))
			if err != nil || len(raw) > maxManifestBytes {
				return Manifest{}, "", errors.New("backup: manifest is unreadable or too large")
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				return Manifest{}, "", fmt.Errorf("backup: manifest is not valid JSON: %w", err)
			}
			haveMan = true
		case dbName:
			out, err := os.OpenFile(stagedDB, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return Manifest{}, "", err
			}
			n, err := io.Copy(out, io.LimitReader(tr, maxDBBytes+1))
			cerr := out.Close()
			if err != nil || cerr != nil || n > maxDBBytes {
				return Manifest{}, "", errors.New("backup: the database in the bundle is unreadable or too large")
			}
			haveDB = true
		default:
			return Manifest{}, "", fmt.Errorf("backup: unexpected entry %q in the bundle", h.Name)
		}
		if extracted > 2 {
			return Manifest{}, "", errors.New("backup: the bundle has too many entries")
		}
	}
	if !haveMan || !haveDB {
		return Manifest{}, "", errors.New("backup: the bundle must contain manifest.json and smotryashchiy.db")
	}
	return m, stagedDB, nil
}
