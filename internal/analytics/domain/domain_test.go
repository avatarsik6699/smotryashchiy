package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewSiteValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      NewSite
		wantErr bool
	}{
		{"valid", NewSite{Name: "Infraege", Domain: "infraege.ru"}, false},
		{"trims and lowercases domain", NewSite{Name: " Infraege ", Domain: " INFRAEGE.RU "}, false},
		{"empty name", NewSite{Name: "", Domain: "infraege.ru"}, true},
		{"empty domain", NewSite{Name: "Infraege", Domain: ""}, true},
		{"domain with scheme", NewSite{Name: "Infraege", Domain: "https://infraege.ru"}, true},
		{"domain with path", NewSite{Name: "Infraege", Domain: "infraege.ru/path"}, true},
		{"domain with port", NewSite{Name: "Infraege", Domain: "infraege.ru:8080"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.in.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err = %v, wantErr %v", err, c.wantErr)
			}
			if err == nil && (got.Name == "" || got.Domain == "") {
				t.Fatalf("normalized empty: %+v", got)
			}
		})
	}
}

func TestNewSiteValidateNormalizesDomainCase(t *testing.T) {
	got, err := NewSite{Name: "x", Domain: "INFRAEGE.RU"}.Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.Domain != "infraege.ru" {
		t.Fatalf("domain = %q, want lowercased", got.Domain)
	}
}

func TestBeaconNormalize(t *testing.T) {
	b, ok := Beacon{Site: " site1 ", URL: " /path "}.Normalize()
	if !ok {
		t.Fatal("want ok=true for a beacon with site and url")
	}
	if b.Site != "site1" || b.URL != "/path" {
		t.Fatalf("normalized = %+v", b)
	}

	missingSite := Beacon{URL: "/path"}
	if _, ok := missingSite.Normalize(); ok {
		t.Fatal("want ok=false when site is missing")
	}
	missingURL := Beacon{Site: "site1"}
	if _, ok := missingURL.Normalize(); ok {
		t.Fatal("want ok=false when url is missing")
	}
}

func TestBeaconNormalizeTruncatesOverlongFields(t *testing.T) {
	long := make([]byte, MaxReferrerBytes+100)
	for i := range long {
		long[i] = 'a'
	}
	b, ok := Beacon{Site: "s", URL: "/p", Referrer: string(long)}.Normalize()
	if !ok {
		t.Fatal("want ok=true")
	}
	if len(b.Referrer) != MaxReferrerBytes {
		t.Fatalf("Referrer length = %d, want %d", len(b.Referrer), MaxReferrerBytes)
	}
}

// Change 35: a site's day is an IANA zone, Europe/Moscow unless given.
func TestNewSiteTimezone(t *testing.T) {
	got, err := NewSite{Name: "x", Domain: "kodumka.ru"}.Validate()
	if err != nil || got.Timezone != DefaultTimezone {
		t.Fatalf("default = %q, %v", got.Timezone, err)
	}
	if got, err := (NewSite{Name: "x", Domain: "kodumka.ru", Timezone: "Asia/Yekaterinburg"}).Validate(); err != nil || got.Timezone != "Asia/Yekaterinburg" {
		t.Fatalf("explicit = %q, %v", got.Timezone, err)
	}
	for _, bad := range []string{"Mars/Olympus", "Local", "+03:00"} {
		if _, err := (NewSite{Name: "x", Domain: "kodumka.ru", Timezone: bad}).Validate(); err == nil {
			t.Errorf("timezone %q accepted", bad)
		}
	}
	if loc := (Site{Timezone: "nonsense"}).Location(); loc != time.UTC {
		t.Fatalf("unknown zone = %v, want UTC", loc)
	}
}

// Change 35: UTM values are lowercased and kept only in a safe alphabet; widths become groups.
func TestBeaconV2Normalize(t *testing.T) {
	b, ok := Beacon{Site: "s", URL: "/", UTM: UTM{Source: " VK ", Medium: "cpc", Campaign: "ЕГЭ 16", Content: strings.Repeat("a", 101), Term: "python-range_1.0"},
		Width: -5, Hostname: " WWW.Kodumka.RU "}.Normalize()
	if !ok {
		t.Fatal("valid beacon rejected")
	}
	want := UTM{Source: "vk", Medium: "cpc", Campaign: InvalidUTM, Content: InvalidUTM, Term: "python-range_1.0"}
	if b.UTM != want || b.Width != 0 || b.Hostname != "www.kodumka.ru" {
		t.Fatalf("normalized = %+v", b)
	}
	for width, group := range map[int]string{0: "", 390: "mobile", 575: "mobile", 576: "tablet", 991: "tablet", 992: "laptop", 1439: "laptop", 1440: "desktop"} {
		if got := ScreenGroup(width); got != group {
			t.Errorf("ScreenGroup(%d) = %q, want %q", width, got, group)
		}
	}
}

// The wire names of a v2 beacon (Change 35); a pre-v2 body with title/screen/language still decodes.
func TestBeaconWireNames(t *testing.T) {
	var b Beacon
	body := `{"v":2,"site":"s","url":"/ege","referrer":"https://vk.com","utm":{"source":"vk","term":"x"},"paid":true,"w":390,"h":"kodumka.ru"}`
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	if b.V != 2 || b.UTM.Source != "vk" || b.UTM.Term != "x" || !b.Paid || b.Width != 390 || b.Hostname != "kodumka.ru" {
		t.Fatalf("decoded = %+v", b)
	}
	var old Beacon
	if err := json.Unmarshal([]byte(`{"site":"s","url":"/","referrer":"","title":"T","screen":"1x1","language":"ru"}`), &old); err != nil || old.V != 0 || !old.UTM.Empty() {
		t.Fatalf("old beacon = %+v, %v", old, err)
	}
}
