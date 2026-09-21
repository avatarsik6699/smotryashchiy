// Package transport wraps userspace WireGuard (wireguard-go + gVisor netstack): the server side
// owns the tunnel device and peer table, the agent side dials through it. No kernel module, no
// root and no wireguard-tools are involved (docs/SPEC.md §3, §4b).
package transport

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// IngestPort is the TCP port of the tunnel-only ingest listener on the server's tunnel address.
const IngestPort = 8443

const keyLen = 32

// GenerateKey returns a fresh X25519 key pair, both base64 (the format `wg` uses).
func GenerateKey() (private, public string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("transport: generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// PublicKey derives the base64 public key of a base64 private key.
func PublicKey(private string) (string, error) {
	raw, err := decodeKey(private)
	if err != nil {
		return "", err
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("transport: invalid private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// ValidatePublicKey reports whether s is a base64 32-byte WireGuard public key.
func ValidatePublicKey(s string) error {
	raw, err := decodeKey(s)
	if err != nil {
		return err
	}
	if _, err := ecdh.X25519().NewPublicKey(raw); err != nil {
		return errors.New("transport: invalid public key")
	}
	return nil
}

func decodeKey(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != keyLen {
		return nil, errors.New("transport: key must be 32 bytes, base64 encoded")
	}
	return raw, nil
}

// hexKey converts a base64 key to the hex form wireguard-go's IPC protocol expects.
func hexKey(s string) (string, error) {
	raw, err := decodeKey(s)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
