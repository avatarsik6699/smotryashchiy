// Package domain holds site analytics' value types and validation (docs/SPEC.md §4i). This is a
// bounded context of its own: no cookies, no persistent visitor identifier, no PII stored — see
// Beacon.Normalize and the package docs for the exact construction.
package domain

import (
	"strconv"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// Limits of the site/beacon definitions.
const (
	MaxSites         = 20
	MaxNameBytes     = 80
	MaxDomainBytes   = 253
	MaxURLBytes      = 2048
	MaxReferrerBytes = 2048
)

// Site is a website an operator tracks for visitor analytics.
type Site struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

// NewSite is a site definition as submitted, before validation.
type NewSite struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
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
	return n, nil
}

// Beacon is one pageview as submitted by the tracking snippet, before validation/derivation.
type Beacon struct {
	Site     string `json:"site"`
	URL      string `json:"url"`
	Referrer string `json:"referrer"`
}

// Normalize caps every field's length (silent truncation, never an error: this public endpoint
// always answers 204 regardless of input — see docs/SPEC.md §4i) and reports whether Site/URL, the
// two required fields, are present at all.
func (b Beacon) Normalize() (Beacon, bool) {
	b.Site = strings.TrimSpace(b.Site)
	b.URL = capString(strings.TrimSpace(b.URL), MaxURLBytes)
	b.Referrer = capString(strings.TrimSpace(b.Referrer), MaxReferrerBytes)
	return b, b.Site != "" && b.URL != ""
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
}

// Stats summarizes a site's pageviews over a range.
type Stats struct {
	Pageviews    int           `json:"pageviews"`
	Visitors     int           `json:"visitors"`
	TopPages     []PathCount   `json:"top_pages"`
	TopReferrers []DomainCount `json:"top_referrers"`
}

type PathCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

type DomainCount struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}
