package main

import (
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/agent/collect"
)

func TestDocumentCollectors(t *testing.T) {
	got, err := documentCollectors([]string{"backup=/var/lib/x/backup.json"}, "/var/lib/x/summary.json")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, c := range got {
		f := c.(*collect.FileDocument)
		kinds[f.Kind] = f.Path
	}
	if kinds["backup"] != "/var/lib/x/backup.json" || kinds["business"] != "/var/lib/x/summary.json" || len(kinds) != 2 {
		t.Fatalf("collectors = %v", kinds)
	}

	none, err := documentCollectors(nil, "")
	if err != nil || len(none) != 0 {
		t.Fatalf("no flags: %v, %v", none, err)
	}
}

func TestDocumentCollectorsRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		flags    []string
		business string
		want     string
	}{
		"no path":        {flags: []string{"backup="}, want: "KIND=PATH"},
		"no separator":   {flags: []string{"backup"}, want: "KIND=PATH"},
		"bad kind":       {flags: []string{"Backup=/x"}, want: "KIND=PATH"},
		"duplicate":      {flags: []string{"backup=/a", "backup=/b"}, want: "twice"},
		"alias clash":    {flags: []string{"business=/a"}, business: "/b", want: "twice"},
		"too many kinds": {flags: []string{"a=/1", "b=/2", "c=/3", "d=/4", "e=/5"}, want: "at most 4"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := documentCollectors(tc.flags, tc.business)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
