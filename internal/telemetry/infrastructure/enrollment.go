package infrastructure

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/application"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/transport"
)

var _ application.EnrollmentRepository = (*Store)(nil)

const serverKeySetting = "wg_server_private_key"

// CreateEnrollableHost registers a host together with the hash of its enrollment secret, atomically.
func (s *Store) CreateEnrollableHost(ctx context.Context, name, secretHash string, now, expiresAt time.Time) (domain.Host, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: generate host id: %w", err)
	}
	host := domain.Host{ID: hex.EncodeToString(buf), Name: name, CreatedAt: now.UTC().Truncate(time.Millisecond)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO hosts (id, name, created_at) VALUES (?, ?, ?)`,
		host.ID, host.Name, host.CreatedAt.UnixMilli()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return domain.Host{}, apierror.Conflict("host name already exists")
		}
		return domain.Host{}, fmt.Errorf("telemetry: create host: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO host_enrollments (host_id, secret_hash, expires_at) VALUES (?, ?, ?)`,
		host.ID, secretHash, expiresAt.UnixMilli()); err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: store enrollment secret: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Host{}, fmt.Errorf("telemetry: commit: %w", err)
	}
	return host, nil
}

// Enroll consumes the secret, allocates the next free tunnel address and stores the peer in one
// transaction. The consume is a single conditional UPDATE, so a secret works at most once even
// under concurrent requests, and every failure of it is indistinguishable to the caller.
func (s *Store) Enroll(ctx context.Context, secretHash, publicKey string, now time.Time, subnet netip.Prefix) (domain.Peer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Peer{}, fmt.Errorf("telemetry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var hostID string
	err = tx.QueryRowContext(ctx,
		`UPDATE host_enrollments SET used_at = ?1
		 WHERE secret_hash = ?2 AND used_at IS NULL AND expires_at > ?1 RETURNING host_id`,
		now.UnixMilli(), secretHash).Scan(&hostID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Peer{}, apierror.Unauthorized("invalid or expired enrollment secret")
	}
	if err != nil {
		return domain.Peer{}, fmt.Errorf("telemetry: consume enrollment secret: %w", err)
	}

	used, err := usedTunnelIPs(ctx, tx)
	if err != nil {
		return domain.Peer{}, err
	}
	ip, ok := nextFreeAddr(subnet, used)
	if !ok {
		return domain.Peer{}, fmt.Errorf("telemetry: tunnel subnet %s is exhausted", subnet)
	}
	peer := domain.Peer{HostID: hostID, PublicKey: publicKey, TunnelIP: ip, EnrolledAt: now.UTC().Truncate(time.Millisecond)}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO host_peers (host_id, public_key, tunnel_ip, enrolled_at) VALUES (?, ?, ?, ?)`,
		peer.HostID, peer.PublicKey, peer.TunnelIP.String(), peer.EnrolledAt.UnixMilli()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return domain.Peer{}, apierror.Conflict("public key is already enrolled")
		}
		return domain.Peer{}, fmt.Errorf("telemetry: store peer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Peer{}, fmt.Errorf("telemetry: commit: %w", err)
	}
	return peer, nil
}

func usedTunnelIPs(ctx context.Context, tx *sql.Tx) (map[netip.Addr]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT tunnel_ip FROM host_peers`)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list tunnel addresses: %w", err)
	}
	defer rows.Close()
	used := map[netip.Addr]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("telemetry: scan tunnel address: %w", err)
		}
		if ip, err := netip.ParseAddr(raw); err == nil {
			used[ip] = true
		}
	}
	return used, rows.Err()
}

// nextFreeAddr returns the lowest host address of subnet that is not used, skipping the network
// address, the server (.1) and the broadcast address.
func nextFreeAddr(subnet netip.Prefix, used map[netip.Addr]bool) (netip.Addr, bool) {
	network := subnet.Masked().Addr()
	for ip := network.Next().Next(); subnet.Contains(ip); ip = ip.Next() { // start at .2
		if broadcast(subnet, ip) {
			break
		}
		if !used[ip] {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func broadcast(subnet netip.Prefix, ip netip.Addr) bool {
	next := ip.Next()
	return !next.IsValid() || !subnet.Contains(next)
}

// Peers lists every stored peer.
func (s *Store) Peers(ctx context.Context) ([]domain.Peer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT host_id, public_key, tunnel_ip, enrolled_at FROM host_peers ORDER BY tunnel_ip`)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list peers: %w", err)
	}
	defer rows.Close()
	var peers []domain.Peer
	for rows.Next() {
		var (
			p   domain.Peer
			ip  string
			ms  int64
			err error
		)
		if err = rows.Scan(&p.HostID, &p.PublicKey, &ip, &ms); err != nil {
			return nil, fmt.Errorf("telemetry: scan peer: %w", err)
		}
		if p.TunnelIP, err = netip.ParseAddr(ip); err != nil {
			return nil, fmt.Errorf("telemetry: stored tunnel address %q: %w", ip, err)
		}
		p.EnrolledAt = time.UnixMilli(ms).UTC()
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

// HostByTunnelIP resolves the host owning a tunnel address.
func (s *Store) HostByTunnelIP(ctx context.Context, ip netip.Addr) (string, bool, error) {
	var hostID string
	err := s.db.QueryRowContext(ctx, `SELECT host_id FROM host_peers WHERE tunnel_ip = ?`, ip.String()).Scan(&hostID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("telemetry: resolve tunnel address: %w", err)
	}
	return hostID, true, nil
}

// ServerKey returns the server's WireGuard private key, generating and storing one on first use.
func (s *Store) ServerKey(ctx context.Context) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, serverKeySetting).Scan(&key)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("telemetry: read server key: %w", err)
	}
	private, _, err := transport.GenerateKey()
	if err != nil {
		return "", err
	}
	// ON CONFLICT DO NOTHING then re-read: two starters racing converge on one stored key.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, serverKeySetting, private); err != nil {
		return "", fmt.Errorf("telemetry: store server key: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, serverKeySetting).Scan(&key); err != nil {
		return "", fmt.Errorf("telemetry: read server key: %w", err)
	}
	return key, nil
}
