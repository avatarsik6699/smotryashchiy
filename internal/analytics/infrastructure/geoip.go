package infrastructure

import (
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/application"
)

// geoRecheck is how often a lookup looks at the file's modification time: the monthly update
// swaps the file in place and the server picks it up without a restart (Change 35).
const geoRecheck = time.Minute

// GeoIP places a visitor by IP from a local MMDB file (DB-IP City Lite, docs/SPEC.md §4i): the
// ISO country code and the English name of the first subdivision. DB-IP Lite has no ISO 3166-2
// codes, so the region is a name. No file, an unreadable file or no record means "" — analytics
// keeps working without geography. The IP is never stored or logged.
type GeoIP struct {
	path string
	now  func() time.Time

	mu        sync.RWMutex
	reader    *maxminddb.Reader
	modTime   time.Time
	checkedAt time.Time
}

var _ application.Geo = (*GeoIP)(nil)

type geoRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names struct {
			English string `maxminddb:"en"`
		} `maxminddb:"names"`
	} `maxminddb:"subdivisions,maxsize:8"`
}

// NewGeoIP opens path lazily; an empty path disables geography.
func NewGeoIP(path string, now func() time.Time) *GeoIP {
	return &GeoIP{path: path, now: now}
}

// Locate implements application.Geo. The whole lookup runs under the read lock: Close unmaps the
// file at once, so a reader is replaced only while no lookup uses it.
func (g *GeoIP) Locate(ip string) (country, region string) {
	if g == nil || g.path == "" {
		return "", ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "", ""
	}
	g.refresh()
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.reader == nil {
		return "", ""
	}
	var rec geoRecord
	if err := g.reader.Lookup(addr.Unmap()).Decode(&rec); err != nil {
		return "", ""
	}
	if len(rec.Subdivisions) > 0 {
		region = rec.Subdivisions[0].Names.English
	}
	return rec.Country.ISOCode, region
}

// refresh reopens the file when its modification time changed, at most once per geoRecheck.
func (g *GeoIP) refresh() {
	now := g.now()
	g.mu.RLock()
	fresh := now.Sub(g.checkedAt) < geoRecheck
	g.mu.RUnlock()
	if fresh {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.checkedAt) < geoRecheck {
		return
	}
	g.checkedAt = now
	info, err := os.Stat(g.path)
	if err != nil {
		if g.reader != nil {
			slog.Warn("analytics geoip file unavailable, keeping the open one", "err", err)
		}
		return
	}
	if g.reader != nil && info.ModTime().Equal(g.modTime) {
		return
	}
	next, err := maxminddb.Open(g.path)
	if err != nil {
		slog.Error("analytics geoip open failed", "err", err)
		return
	}
	if g.reader != nil {
		_ = g.reader.Close()
	}
	g.reader, g.modTime = next, info.ModTime()
}

// Close releases the open file.
func (g *GeoIP) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reader == nil {
		return nil
	}
	err := g.reader.Close()
	g.reader = nil
	return err
}
