package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T, dir string, maxBatches int, maxBytes int64) *Spool {
	t.Helper()
	s, err := Open(dir, maxBatches, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFIFOOrderAndRemoveOnlyAfterAck(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "spool"), 0, 0)
	for _, k := range []string{"a", "b", "c"} {
		if _, err := s.Add(k, []byte(`{"n":"`+k+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	first, ok, err := s.Peek()
	if err != nil || !ok || first.Key != "a" || string(first.Batch) != `{"n":"a"}` {
		t.Fatalf("peek = %+v %v %v", first, ok, err)
	}
	if again, _, _ := s.Peek(); again.Key != "a" || s.Len() != 3 {
		t.Fatal("Peek must not consume the entry")
	}
	if err := s.Remove(first); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"b", "c"} {
		e, ok, _ := s.Peek()
		if !ok || e.Key != want {
			t.Fatalf("got %q, want %q", e.Key, want)
		}
		_ = s.Remove(e)
	}
	if _, ok, _ := s.Peek(); ok || s.Len() != 0 {
		t.Fatal("queue should be empty")
	}
}

func TestSurvivesRestartInOrderAndKeepsSequenceMonotonic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s := open(t, dir, 0, 0)
	_, _ = s.Add("k1", []byte(`{}`))
	_, _ = s.Add("k2", []byte(`{}`))
	s2 := open(t, dir, 0, 0)
	if s2.Len() != 2 {
		t.Fatalf("restored %d batches, want 2", s2.Len())
	}
	_, _ = s2.Add("k3", []byte(`{}`))
	var got []string
	for {
		e, ok, _ := s2.Peek()
		if !ok {
			break
		}
		got = append(got, e.Key)
		_ = s2.Remove(e)
	}
	if strings.Join(got, ",") != "k1,k2,k3" {
		t.Fatalf("order after restart = %v", got)
	}
}

func TestBoundsDropOldestAndCount(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "spool"), 3, 0)
	total := 0
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		d, err := s.Add(k, []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		total += d
	}
	if total != 2 || s.Len() != 3 {
		t.Fatalf("dropped %d, len %d; want 2 dropped and 3 kept", total, s.Len())
	}
	if e, _, _ := s.Peek(); e.Key != "c" {
		t.Fatalf("oldest kept = %q, want c", e.Key)
	}

	sb := open(t, filepath.Join(t.TempDir(), "bytes"), 100, 300)
	big := []byte(`{"pad":"` + strings.Repeat("x", 150) + `"}`)
	dropped := 0
	for _, k := range []string{"a", "b", "c", "d"} {
		d, _ := sb.Add(k, big)
		dropped += d
	}
	if dropped == 0 || sb.Len() >= 4 {
		t.Fatalf("byte bound not enforced: dropped %d, len %d", dropped, sb.Len())
	}
	if last, _ := sb.Add("z", big); sb.Len() < 1 {
		t.Fatalf("the newest batch must always be kept (dropped %d)", last)
	}
}

func TestFilesAreOwnerOnlyAndTempLeftoversAreCleaned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s := open(t, dir, 0, 0)
	_, _ = s.Add("k", []byte(`{}`))
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", info.Mode().Perm())
	}
	if err := os.WriteFile(filepath.Join(dir, tmpPrefix+"crash"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, dir, 0, 0)
	if s2.Len() != 1 {
		t.Fatalf("len = %d; a leftover temp file must not count as a batch", s2.Len())
	}
	if _, err := os.Stat(filepath.Join(dir, tmpPrefix+"crash")); !os.IsNotExist(err) {
		t.Fatal("interrupted write leftover was not removed")
	}
}

func TestCorruptFileIsDiscardedInsteadOfBlockingTheQueue(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s := open(t, dir, 0, 0)
	_, _ = s.Add("good1", []byte(`{}`))
	entries, _ := os.ReadDir(dir)
	if err := os.WriteFile(filepath.Join(dir, entries[0].Name()), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Add("good2", []byte(`{}`))
	e, ok, err := s.Peek()
	if err != nil || !ok || e.Key != "good2" || s.Corrupt() != 1 {
		t.Fatalf("peek = %+v %v %v corrupt=%d; want good2 after discarding the corrupt head", e, ok, err, s.Corrupt())
	}
}

func TestAddRejectsEmptyKey(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "spool"), 0, 0)
	if _, err := s.Add("", []byte(`{}`)); err == nil {
		t.Fatal("empty key must be rejected: a keyless batch could not be deduplicated")
	}
}
