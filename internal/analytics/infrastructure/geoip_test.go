package infrastructure

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Without a file (or with a path that is gone) geography is simply unknown (Change 35).
func TestGeoIPWithoutAFileIsUnknown(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing.mmdb")} {
		g := NewGeoIP(path, time.Now)
		if country, region := g.Locate("77.88.55.88"); country != "" || region != "" {
			t.Fatalf("%q: located %q/%q", path, country, region)
		}
		if country, _ := g.Locate("not an ip"); country != "" {
			t.Fatal("located a malformed address")
		}
	}
}

// SMOTRYASHCHIY_TEST_GEOIP names a real DB-IP City Lite file (it is 120 MB, not in the
// repository): lookups return the ISO country and the English region, and a replaced file is
// picked up without a restart.
func TestGeoIPReadsDBIPCityLite(t *testing.T) {
	src := os.Getenv("SMOTRYASHCHIY_TEST_GEOIP")
	if src == "" {
		t.Skip("SMOTRYASHCHIY_TEST_GEOIP not set")
	}
	path := filepath.Join(t.TempDir(), "city.mmdb")
	copyFile(t, src, path)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	g := NewGeoIP(path, func() time.Time { return now })
	defer func() { _ = g.Close() }()
	if country, region := g.Locate("217.118.83.1"); country != "RU" || region != "Kirov Oblast" {
		t.Fatalf("217.118.83.1 = %q/%q", country, region)
	}
	if country, _ := g.Locate("::ffff:8.8.8.8"); country != "US" {
		t.Fatalf("mapped IPv4 = %q", country)
	}
	// an update replaces the file; after geoRecheck the new one is opened
	next := filepath.Join(t.TempDir(), "next.mmdb")
	copyFile(t, src, next)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(next, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * geoRecheck)
	if country, _ := g.Locate("8.8.8.8"); country != "US" {
		t.Fatalf("after reload = %q", country)
	}
	g.mu.RLock()
	reloaded := g.modTime.Equal(later.Truncate(time.Second)) || g.modTime.Equal(later)
	g.mu.RUnlock()
	if !reloaded {
		t.Fatal("the replaced file was not reopened")
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
