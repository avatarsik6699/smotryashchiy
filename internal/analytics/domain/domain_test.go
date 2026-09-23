package domain

import "testing"

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
