// Package domain holds site analytics' value types and validation (docs/SPEC.md §4i). This is a
// bounded context of its own: no cookies, no persistent visitor identifier, no PII stored — see
// Beacon.Normalize and the package docs for the exact construction.
package domain

import (
	"strconv"
	"strings"
	"time"
	// Change 35: site timezones resolve without the host's zoneinfo (the image ships none).
	_ "time/tzdata"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Limits of the site/beacon definitions.
const (
	MaxSites         = 20
	MaxNameBytes     = 80
	MaxDomainBytes   = 253
	MaxURLBytes      = 2048
	MaxReferrerBytes = 2048
	// Change 35: one UTM value, after normalization.
	MaxUTMBytes = 100
	// DefaultTimezone is a site's day when none is given (the tracked audience is Russian).
	DefaultTimezone = "Europe/Moscow"
	// InvalidUTM replaces a UTM value outside [a-z0-9._-] (free text can carry personal data).
	InvalidUTM = "(invalid)"
)

// Site is a website an operator tracks for visitor analytics.
type Site struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
	// Timezone is the IANA zone of the site's day (Change 35): reports and the visitor-hash salt
	// turn over at its local midnight.
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
}

// Location is the site's timezone, UTC when it cannot be loaded.
func (s Site) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil && s.Timezone != "" {
		return loc
	}
	return time.UTC
}

// NewSite is a site definition as submitted, before validation.
type NewSite struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`
	Timezone string `json:"timezone,omitempty"`
}

// Validate normalizes the definition (trimmed, lowercased domain) or returns a field-addressed error.
func (n NewSite) Validate() (NewSite, error) {
	n.Name = strings.TrimSpace(n.Name)
	n.Domain = strings.ToLower(strings.TrimSpace(n.Domain))
	if n.Name == "" || len(n.Name) > MaxNameBytes {
		return NewSite{}, apierror.Invalid("name must contain 1.." + strconv.Itoa(MaxNameBytes) + " bytes")
	}
	if n.Domain == "" || len(n.Domain) > MaxDomainBytes || strings.ContainsAny(n.Domain, " /\\@:") {
		return NewSite{}, apierror.Invalid("domain must be a bare hostname (no scheme, path or port)")
	}
	n.Timezone = strings.TrimSpace(n.Timezone)
	if n.Timezone == "" {
		n.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(n.Timezone); err != nil || n.Timezone == "Local" {
		return NewSite{}, apierror.Invalid("timezone must be an IANA zone such as Europe/Moscow")
	}
	return n, nil
}

// Beacon is one pageview as submitted by the tracking snippet, before validation/derivation.
// Version 2 (Change 35) adds what the server cannot see; every v2 field is optional, so the
// original {site,url,referrer} beacon is still a complete one.
type Beacon struct {
	Site     string `json:"site"`
	URL      string `json:"url"`
	Referrer string `json:"referrer"`
	V        int    `json:"v,omitempty"`
	UTM      UTM    `json:"utm"`
	// Paid reports a yclid/gclid in the landing URL; the value itself is never sent.
	Paid bool `json:"paid,omitempty"`
	// Width is the window's inner width in CSS pixels; only its group is stored.
	Width int `json:"w,omitempty"`
	// Hostname is the page's location.hostname; kept only when it belongs to the site.
	Hostname string `json:"h,omitempty"`
}

// UTM holds the five campaign parameters of a landing URL (Change 35).
type UTM struct {
	Source   string `json:"source,omitempty"`
	Medium   string `json:"medium,omitempty"`
	Campaign string `json:"campaign,omitempty"`
	Content  string `json:"content,omitempty"`
	Term     string `json:"term,omitempty"`
}

// Empty reports whether no parameter is set.
func (u UTM) Empty() bool { return u == UTM{} }

// Normalize caps every field's length (silent truncation, never an error: this public endpoint
// always answers 204 regardless of input — see docs/SPEC.md §4i) and reports whether Site/URL, the
// two required fields, are present at all.
func (b Beacon) Normalize() (Beacon, bool) {
	b.Site = strings.TrimSpace(b.Site)
	b.URL = capString(strings.TrimSpace(b.URL), MaxURLBytes)
	b.Referrer = capString(strings.TrimSpace(b.Referrer), MaxReferrerBytes)
	b.UTM = UTM{
		Source: utmValue(b.UTM.Source), Medium: utmValue(b.UTM.Medium), Campaign: utmValue(b.UTM.Campaign),
		Content: utmValue(b.UTM.Content), Term: utmValue(b.UTM.Term),
	}
	if b.Width < 0 || b.Width > 100000 {
		b.Width = 0
	}
	b.Hostname = capString(strings.ToLower(strings.TrimSpace(b.Hostname)), MaxDomainBytes)
	return b, b.Site != "" && b.URL != ""
}

// utmValue lowercases a UTM value; one outside [a-z0-9._-] or longer than MaxUTMBytes becomes
// InvalidUTM, so a free-text label never stores personal data (Change 35).
func utmValue(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return ""
	}
	if len(v) > MaxUTMBytes {
		return InvalidUTM
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return InvalidUTM
		}
	}
	return v
}

// ScreenGroup buckets a window width (Change 35); the exact width is never stored.
func ScreenGroup(width int) string {
	switch {
	case width <= 0:
		return ""
	case width < 576:
		return "mobile"
	case width < 992:
		return "tablet"
	case width < 1440:
		return "laptop"
	default:
		return "desktop"
	}
}

func capString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// Pageview is one stored, server-derived observation. No raw IP and no persistent visitor
// identifier is ever part of it (docs/SPEC.md §4i): VisitorHash is a daily-rotating salted hash.
type Pageview struct {
	SiteID         string
	TS             time.Time
	Path           string
	ReferrerDomain string
	VisitorHash    string
	Browser        string
	OS             string
	Device         string
	// Hostname is the page's host (Change 35): the site's domain or www. plus it.
	Hostname string
}

// VisitIdle ends a visit: the next pageview of the same daily visitor after it opens a new one.
const VisitIdle = 30 * time.Minute

// Visit describes a visit as its first pageview finds it (Change 35); the store keeps the
// timing, the exit page and the pageview count up to date.
type Visit struct {
	Source         string
	Channel        string
	UTM            UTM
	Paid           bool
	Country        string
	Region         string
	BrowserVersion string
	OSVersion      string
	Screen         string
}
