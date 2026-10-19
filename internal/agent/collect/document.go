package collect

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// MaxDocumentBytes mirrors the server's limit for one document (docs/SPEC.md §4.1).
const MaxDocumentBytes = 512 << 10

// Document is a whole JSON object the agent reports in place of the previous one of its kind.
type Document struct {
	Kind string
	Body json.RawMessage
}

// DocumentCollector yields a document when it has changed since it was last yielded, else nil.
type DocumentCollector interface {
	Name() string
	Collect() (*Document, error)
}

// FileDocument reads a JSON object another program on the host keeps up to date, such as the
// tracked site's hourly business summary (Change 27). A missing file is not an error: the
// producer may not have run yet.
type FileDocument struct {
	Kind string
	Path string
	last [sha256.Size]byte
	sent bool
}

// Name identifies the collector in logs.
func (f *FileDocument) Name() string { return "document:" + f.Kind }

// Collect returns the file's object when its content changed since the last call that returned it.
func (f *FileDocument) Collect() (*Document, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSpace(data)
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("%s: %d bytes, limit is %d", f.Path, len(data), MaxDocumentBytes)
	}
	if !bytes.HasPrefix(data, []byte("{")) || !json.Valid(data) {
		return nil, fmt.Errorf("%s: not a JSON object", f.Path)
	}
	sum := sha256.Sum256(data)
	if f.sent && sum == f.last {
		return nil, nil
	}
	f.last, f.sent = sum, true
	return &Document{Kind: f.Kind, Body: json.RawMessage(data)}, nil
}
