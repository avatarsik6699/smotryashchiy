package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/db"
)

var ctx = context.Background()

// restoreFile opens a bundle file and restores it (the production callers pass a file or stdin).
func restoreFile(ctx context.Context, bundlePath, dbPath string, opts RestoreOptions) (RestoreResult, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return RestoreResult{}, err
	}
	defer f.Close()
	return Restore(ctx, f, dbPath, opts)
}

var clock = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

func newDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	return conn
}

func seed(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO settings (key, value) VALUES ('admin_password_hash', 'hash-1'), ('wg_server_private_key', 'key-1')`,
		`INSERT INTO hosts (id, name, created_at) VALUES ('h1', 'vps-1', 1000), ('h2', 'vps-2', 2000)`,
		`INSERT INTO metrics (host_id, name, labels_json, ts, value, schema_version) VALUES ('h1', 'cpu.usage_percent', '{}', 5000, 0, '1.0')`,
		`INSERT INTO uptime_targets (id, name, kind, address, interval_seconds, created_at) VALUES ('t1', 'site', 'http', 'https://example.com', 60, 3000)`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func count(t *testing.T, conn *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateSnapshotsARunningDatabaseConsistently(t *testing.T) {
	dir := t.TempDir()
	primary := newDB(t, filepath.Join(dir, "live.db"))
	seed(t, primary)

	// A second connection (like a running server) keeps writing while the snapshot is taken.
	writer, err := db.Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = writer.Exec(`INSERT INTO metrics (host_id, name, labels_json, ts, value, schema_version) VALUES ('h1', 'load.avg_1m', '{}', ?, 1, '1.0')`, 10_000+i)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	out := filepath.Join(dir, "backup.tar.gz")
	m, err := CreateFile(ctx, primary, "rel-1", dir, out, clock)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != FormatVersion || m.Release != "rel-1" || m.Migrations != db.MigrationCount() || m.DBSHA256 == "" || m.DBBytes <= 0 || !m.CreatedAt.Equal(clock) {
		t.Fatalf("manifest = %+v", m)
	}

	// The snapshot is a usable database with the seeded data and a consistent, non-torn set of writes.
	restored := filepath.Join(dir, "restored.db")
	if _, err := restoreFile(ctx, out, restored, RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock}); err != nil {
		t.Fatal(err)
	}
	conn := newDB(t, restored)
	if count(t, conn, "hosts") != 2 || count(t, conn, "uptime_targets") != 1 {
		t.Fatal("seeded rows are missing from the snapshot")
	}
	var verdict string
	_ = conn.QueryRow("PRAGMA integrity_check").Scan(&verdict)
	if verdict != "ok" {
		t.Fatalf("integrity_check = %s", verdict)
	}
}

func TestStreamingCreateAndRestoreNeedNoFilesForTheBundle(t *testing.T) {
	dir := t.TempDir()
	conn := newDB(t, filepath.Join(dir, "a.db"))
	seed(t, conn)
	var buf bytes.Buffer
	m, err := Create(ctx, conn, "r", dir, &buf, clock)
	if err != nil || m.DBBytes <= 0 {
		t.Fatalf("%+v %v", m, err)
	}
	target := filepath.Join(dir, "s", "smotryashchiy.db")
	if _, err := Restore(ctx, &buf, target, RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock}); err != nil {
		t.Fatal(err)
	}
	if count(t, newDB(t, target), "hosts") != 2 {
		t.Fatal("streamed round trip lost rows")
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 3 { // a.db (+wal/shm) and s/
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".backup-work-") {
				t.Fatal("work directory left behind")
			}
		}
	}
}

func TestBundleIsOwnerOnlyAndNeverOverwritesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	conn := newDB(t, filepath.Join(dir, "a.db"))
	seed(t, conn)
	out := filepath.Join(dir, "b.tar.gz")
	if _, err := CreateFile(ctx, conn, "r", dir, out, clock); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(out); info.Mode().Perm() != 0o600 {
		t.Fatalf("bundle mode = %v, want 0600 (it contains secrets)", info.Mode().Perm())
	}
	before, _ := os.ReadFile(out)
	if _, err := CreateFile(ctx, conn, "r", dir, out, clock.Add(time.Hour)); err == nil {
		t.Fatal("an existing backup must never be overwritten")
	}
	if after, _ := os.ReadFile(out); !bytes.Equal(before, after) {
		t.Fatal("the existing file was modified by the refused backup")
	}
	if entries, _ := os.ReadDir(dir); func() bool {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".backup-work-") {
				return true
			}
		}
		return false
	}() {
		t.Fatal("the temporary work directory was left behind")
	}
}

func TestRoundTripRestoresEveryTable(t *testing.T) {
	dir := t.TempDir()
	conn := newDB(t, filepath.Join(dir, "a.db"))
	seed(t, conn)
	out := filepath.Join(dir, "b.tar.gz")
	if _, err := CreateFile(ctx, conn, "r", dir, out, clock); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "fresh", "smotryashchiy.db")
	res, err := restoreFile(ctx, out, target, RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock})
	if err != nil || res.PreviousDB != "" {
		t.Fatalf("restore: %+v %v", res, err)
	}
	restored := newDB(t, target)
	var hash, key string
	_ = restored.QueryRow(`SELECT value FROM settings WHERE key = 'admin_password_hash'`).Scan(&hash)
	_ = restored.QueryRow(`SELECT value FROM settings WHERE key = 'wg_server_private_key'`).Scan(&key)
	var zero float64 = -1
	_ = restored.QueryRow(`SELECT value FROM metrics WHERE name = 'cpu.usage_percent'`).Scan(&zero)
	if hash != "hash-1" || key != "key-1" || zero != 0 {
		t.Fatalf("settings/metrics differ: hash=%q key=%q zero=%v (a stored zero must survive)", hash, key, zero)
	}
	if count(t, restored, "hosts") != 2 || count(t, restored, "uptime_targets") != 1 {
		t.Fatal("rows differ after the round trip")
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Fatalf("restored database mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRestoreRefusesAnExistingDatabaseUnlessForcedAndKeepsTheOldOne(t *testing.T) {
	dir := t.TempDir()
	src := newDB(t, filepath.Join(dir, "a.db"))
	seed(t, src)
	out := filepath.Join(dir, "b.tar.gz")
	if _, err := CreateFile(ctx, src, "r", dir, out, clock); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live.db")
	old := newDB(t, live)
	_, _ = old.Exec(`INSERT INTO hosts (id, name, created_at) VALUES ('old', 'old-host', 1)`)
	old.Close() // no server running

	opts := RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock}
	if _, err := restoreFile(ctx, out, live, opts); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("without --force: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("the refused restore must leave the database alone")
	}
	opts.Force = true
	res, err := restoreFile(ctx, out, live, opts)
	if err != nil || res.PreviousDB == "" {
		t.Fatalf("forced restore: %+v %v", res, err)
	}
	kept := newDB(t, res.PreviousDB)
	var n int
	_ = kept.QueryRow(`SELECT COUNT(*) FROM hosts WHERE id = 'old'`).Scan(&n)
	if n != 1 {
		t.Fatal("the replaced database was not kept intact")
	}
	if count(t, newDB(t, live), "hosts") != 2 {
		t.Fatal("the live path does not hold the restored data")
	}
}

func TestRestoreRefusesWhileAnotherConnectionHasTheDatabaseOpen(t *testing.T) {
	dir := t.TempDir()
	src := newDB(t, filepath.Join(dir, "a.db"))
	seed(t, src)
	out := filepath.Join(dir, "b.tar.gz")
	if _, err := CreateFile(ctx, src, "r", dir, out, clock); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live.db")
	server := newDB(t, live) // stands in for the running server
	// A server that has just written holds the WAL open.
	if _, err := server.Exec(`INSERT INTO hosts (id, name, created_at) VALUES ('busy', 'busy', 1)`); err != nil {
		t.Fatal(err)
	}
	tx, err := server.Begin() // an open write transaction, as in the middle of an ingest
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO hosts (id, name, created_at) VALUES ('mid', 'mid', 2)`); err != nil {
		t.Fatal(err)
	}
	_, err = restoreFile(ctx, out, live, RestoreOptions{Force: true, MaxMigrations: db.MigrationCount(), Now: clock})
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("err = %v, want a refusal because the server holds the database", err)
	}
	if _, statErr := os.Stat(live); statErr != nil {
		t.Fatal("the database must not be moved when the restore is refused")
	}
}

func TestAnIdleServerConnectionAlsoBlocksARestoreButAClosedOneDoesNot(t *testing.T) {
	live := filepath.Join(t.TempDir(), "live.db")
	server := newDB(t, live)
	if _, err := server.Exec(`INSERT INTO hosts (id, name, created_at) VALUES ('a', 'a', 1)`); err != nil {
		t.Fatal(err)
	}
	// No transaction is open: this is a running server between requests.
	if err := ensureUnlocked(live); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("idle open connection: %v, want the in-use refusal", err)
	}
	server.Close()
	if err := ensureUnlocked(live); err != nil {
		t.Fatalf("after the server stopped: %v", err)
	}
}

// craft writes a bundle with arbitrary manifest fields and entries.
func craft(t *testing.T, path string, entries map[string][]byte, order []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range order {
		body := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(body)
	}
	tw.Close()
	gz.Close()
	f.Close()
}

func readBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, _ := os.Open(path)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name], _ = io.ReadAll(tr)
	}
}

func goodBundle(t *testing.T, dir string) (string, map[string][]byte) {
	t.Helper()
	src := newDB(t, filepath.Join(dir, "g.db"))
	seed(t, src)
	out := filepath.Join(dir, "good.tar.gz")
	if _, err := CreateFile(ctx, src, "r", dir, out, clock); err != nil {
		t.Fatal(err)
	}
	return out, readBundle(t, out)
}

func TestRestoreRejectsATamperedDatabase(t *testing.T) {
	dir := t.TempDir()
	_, parts := goodBundle(t, dir)
	tampered := append([]byte(nil), parts[dbName]...)
	tampered[len(tampered)-10] ^= 0xFF
	bad := filepath.Join(dir, "bad.tar.gz")
	craft(t, bad, map[string][]byte{manifestName: parts[manifestName], dbName: tampered}, []string{manifestName, dbName})
	_, err := restoreFile(ctx, bad, filepath.Join(dir, "x.db"), RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock})
	if err == nil {
		t.Fatal("a database that does not match its manifest hash must be refused")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "x.db")); statErr == nil {
		t.Fatal("nothing may be installed from a bad bundle")
	}
}

func TestRestoreRefusesANewerSchemaAndAnUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	good, _ := goodBundle(t, dir)
	if _, err := restoreFile(ctx, good, filepath.Join(dir, "old.db"), RestoreOptions{MaxMigrations: db.MigrationCount() - 1, Now: clock}); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("newer schema: %v", err)
	}
	parts := readBundle(t, good)
	future := strings.Replace(string(parts[manifestName]), `"format": 1`, `"format": 2`, 1)
	bad := filepath.Join(dir, "future.tar.gz")
	craft(t, bad, map[string][]byte{manifestName: []byte(future), dbName: parts[dbName]}, []string{manifestName, dbName})
	if _, err := restoreFile(ctx, bad, filepath.Join(dir, "f.db"), RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock}); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("unknown format: %v", err)
	}
}

func TestRestoreRejectsMalformedBundles(t *testing.T) {
	dir := t.TempDir()
	_, parts := goodBundle(t, dir)
	opts := RestoreOptions{MaxMigrations: db.MigrationCount(), Now: clock}
	cases := map[string]struct {
		entries map[string][]byte
		order   []string
	}{
		"path traversal":   {map[string][]byte{"../evil": []byte("x"), manifestName: parts[manifestName], dbName: parts[dbName]}, []string{"../evil", manifestName, dbName}},
		"extra entry":      {map[string][]byte{manifestName: parts[manifestName], dbName: parts[dbName], "extra.txt": []byte("x")}, []string{manifestName, dbName, "extra.txt"}},
		"missing database": {map[string][]byte{manifestName: parts[manifestName]}, []string{manifestName}},
		"missing manifest": {map[string][]byte{dbName: parts[dbName]}, []string{dbName}},
		"bad manifest":     {map[string][]byte{manifestName: []byte("{not json"), dbName: parts[dbName]}, []string{manifestName, dbName}},
		"not a database":   {map[string][]byte{manifestName: parts[manifestName], dbName: []byte("this is not sqlite at all")}, []string{manifestName, dbName}},
	}
	for name, c := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".tar.gz")
		craft(t, path, c.entries, c.order)
		target := filepath.Join(dir, "t-"+strings.ReplaceAll(name, " ", "-")+".db")
		if _, err := restoreFile(ctx, path, target, opts); err == nil {
			t.Errorf("%s: restore succeeded", name)
		}
		if _, err := os.Stat(target); err == nil {
			t.Errorf("%s: a database was installed", name)
		}
	}
	junk := filepath.Join(dir, "junk")
	_ = os.WriteFile(junk, []byte("not gzip"), 0o600)
	if _, err := restoreFile(ctx, junk, filepath.Join(dir, "j.db"), opts); err == nil {
		t.Error("a non-bundle file was accepted")
	}
	if _, err := restoreFile(ctx, filepath.Join(dir, "absent.tar.gz"), filepath.Join(dir, "a.db"), opts); err == nil {
		t.Error("a missing file was accepted")
	}
	if entries, _ := os.ReadDir(dir); func() bool {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".restore-work-") {
				return true
			}
		}
		return false
	}() {
		t.Error("restore work directories were left behind")
	}
	_ = fmt.Sprint()
}
