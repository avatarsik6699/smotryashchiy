// Package spool is the agent's durable, bounded FIFO of unsent batches (docs/SPEC.md §4c). A batch
// stays on disk until the server has acknowledged it, so an outage or a restart never loses data.
package spool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default bounds (docs/SPEC.md §4c).
const (
	DefaultMaxBatches = 5000
	DefaultMaxBytes   = 64 << 20
)

const (
	fileSuffix = ".json"
	tmpPrefix  = ".tmp-"
)

// Entry is one queued batch together with the Idempotency-Key it must always be sent with.
type Entry struct {
	Key   string
	Batch []byte

	name string
}

type record struct {
	Key   string          `json:"key"`
	Batch json.RawMessage `json:"batch"`
}

type item struct {
	name string
	size int64
}

// Spool is a directory of one file per batch, named by a monotonically increasing sequence so that
// lexical order is send order. It is safe for concurrent use.
type Spool struct {
	dir        string
	maxBatches int
	maxBytes   int64

	mu      sync.Mutex
	items   []item // oldest first
	bytes   int64
	lastSeq uint64
	corrupt int
}

// Open creates dir (mode 0700) if needed and loads the existing queue. Leftovers of interrupted
// writes are removed. maxBatches/maxBytes <= 0 select the defaults.
func Open(dir string, maxBatches int, maxBytes int64) (*Spool, error) {
	if maxBatches <= 0 {
		maxBatches = DefaultMaxBatches
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("spool: create %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("spool: read %s: %w", dir, err)
	}
	s := &Spool{dir: dir, maxBatches: maxBatches, maxBytes: maxBytes}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, tmpPrefix):
			_ = os.Remove(filepath.Join(dir, name))
		case strings.HasSuffix(name, fileSuffix) && !e.IsDir():
			info, err := e.Info()
			if err != nil {
				continue
			}
			s.items = append(s.items, item{name: name, size: info.Size()})
			s.bytes += info.Size()
			if seq, err := strconv.ParseUint(strings.TrimSuffix(name, fileSuffix), 10, 64); err == nil && seq > s.lastSeq {
				s.lastSeq = seq
			}
		}
	}
	sort.Slice(s.items, func(i, j int) bool { return s.items[i].name < s.items[j].name })
	return s, nil
}

// Add appends a batch and returns how many of the oldest batches were dropped to respect the bounds.
func (s *Spool) Add(key string, batch []byte) (dropped int, err error) {
	if key == "" {
		return 0, errors.New("spool: empty idempotency key")
	}
	body, err := json.Marshal(record{Key: key, Batch: batch})
	if err != nil {
		return 0, fmt.Errorf("spool: encode batch: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := uint64(time.Now().UnixNano())
	if seq <= s.lastSeq {
		seq = s.lastSeq + 1
	}
	name := fmt.Sprintf("%020d%s", seq, fileSuffix)
	if err := writeAtomic(filepath.Join(s.dir, name), body); err != nil {
		return 0, err
	}
	s.lastSeq = seq
	s.items = append(s.items, item{name: name, size: int64(len(body))})
	s.bytes += int64(len(body))
	for len(s.items) > 1 && (len(s.items) > s.maxBatches || s.bytes > s.maxBytes) {
		oldest := s.items[0]
		if err := os.Remove(filepath.Join(s.dir, oldest.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return dropped, fmt.Errorf("spool: drop oldest batch: %w", err)
		}
		s.items, s.bytes = s.items[1:], s.bytes-oldest.size
		dropped++
	}
	return dropped, nil
}

// Peek returns the oldest batch without removing it; ok is false when the queue is empty. A file
// that cannot be decoded is discarded (and counted) rather than blocking the queue forever.
func (s *Spool) Peek() (e Entry, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.items) > 0 {
		it := s.items[0]
		raw, rerr := os.ReadFile(filepath.Join(s.dir, it.name))
		var rec record
		if rerr == nil {
			rerr = json.Unmarshal(raw, &rec)
			if rerr == nil && rec.Key == "" {
				rerr = errors.New("missing key")
			}
		}
		if rerr != nil {
			if err := os.Remove(filepath.Join(s.dir, it.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return Entry{}, false, fmt.Errorf("spool: discard unreadable batch %s: %w", it.name, err)
			}
			s.items, s.bytes = s.items[1:], s.bytes-it.size
			s.corrupt++
			continue
		}
		return Entry{Key: rec.Key, Batch: rec.Batch, name: it.name}, true, nil
	}
	return Entry{}, false, nil
}

// Remove deletes an acknowledged (or permanently rejected) entry returned by Peek.
func (s *Spool) Remove(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if it.name != e.name {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, it.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("spool: remove batch: %w", err)
		}
		s.items = append(s.items[:i], s.items[i+1:]...)
		s.bytes -= it.size
		return nil
	}
	return nil
}

// Len is the number of queued batches.
func (s *Spool) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Corrupt is how many unreadable files were discarded since Open.
func (s *Spool) Corrupt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.corrupt
}

// writeAtomic writes path via a temporary file, fsync and rename, mode 0600.
func writeAtomic(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*")
	if err != nil {
		return fmt.Errorf("spool: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("spool: chmod: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("spool: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("spool: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("spool: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("spool: rename: %w", err)
	}
	return nil
}
