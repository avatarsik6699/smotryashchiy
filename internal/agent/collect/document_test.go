package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Change 27: a document is yielded once per content, a missing file is quiet, a bad one is an error.
func TestFileDocumentYieldsOnChangeOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.json")
	f := &FileDocument{Kind: "business", Path: path}
	if doc, err := f.Collect(); doc != nil || err != nil {
		t.Fatalf("missing file: %v %v", doc, err)
	}
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"schema":"kodumka.business/1"}` + "\n")
	doc, err := f.Collect()
	if err != nil || doc == nil || doc.Kind != "business" || string(doc.Body) != `{"schema":"kodumka.business/1"}` {
		t.Fatalf("first read: %+v %v", doc, err)
	}
	if doc, err := f.Collect(); doc != nil || err != nil {
		t.Fatalf("unchanged file yielded again: %+v %v", doc, err)
	}
	write(`{"schema":"kodumka.business/1","n":2}`)
	if doc, _ := f.Collect(); doc == nil {
		t.Fatal("changed file not yielded")
	}
	for name, body := range map[string]string{
		"array":     `[]`,
		"truncated": `{"a":`,
		"too large": `{"x":"` + strings.Repeat("a", MaxDocumentBytes) + `"}`,
	} {
		write(body)
		if _, err := f.Collect(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
