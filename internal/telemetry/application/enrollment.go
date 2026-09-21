package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
	"github.com/avatarsik6699/smotryashchiy/internal/telemetry/domain"
	"github.com/avatarsik6699/smotryashchiy/internal/transport"
)

// EnrollmentTTL is how long a freshly created enrollment secret stays valid.
const EnrollmentTTL = time.Hour

// EnrollmentRepository is the persistence port for enrollment. Enroll must consume the secret,
// allocate the tunnel address and store the peer in one atomic step.
type EnrollmentRepository interface {
	// CreateEnrollableHost registers a host and stores the hash of its one-time enrollment secret.
	CreateEnrollableHost(ctx context.Context, name, secretHash string, now, expiresAt time.Time) (domain.Host, error)
	// Enroll atomically consumes an unused, unexpired secret and stores the peer with the next free
	// address of subnet. A wrong, expired or used secret yields apierror.Unauthorized; a public key
	// that is already enrolled yields apierror.Conflict and leaves the secret unused.
	Enroll(ctx context.Context, secretHash, publicKey string, now time.Time, subnet netip.Prefix) (domain.Peer, error)
	// Peers lists every stored peer (start-up replay into the tunnel).
	Peers(ctx context.Context) ([]domain.Peer, error)
	// HostByTunnelIP resolves the host owning a tunnel address; ok is false when none does.
	HostByTunnelIP(ctx context.Context, ip netip.Addr) (hostID string, ok bool, err error)
}

// PeerAuthorizer makes the tunnel accept a peer (implemented by transport.Server).
type PeerAuthorizer interface {
	AddPeer(publicKey string, ip netip.Addr) error
}

// ServerInfo is what an agent needs to reach the server, returned by enrollment.
type ServerInfo struct {
	PublicKey string
	Endpoint  string // UDP host:port
	TunnelIP  netip.Addr
}

// Enrollment is the enrollment result handed to the agent.
type Enrollment struct {
	HostID   string
	TunnelIP netip.Addr
	Server   ServerInfo
}

// EnrollmentService creates hosts with one-time secrets and enrolls agents.
type EnrollmentService struct {
	repo   EnrollmentRepository
	tunnel PeerAuthorizer
	subnet netip.Prefix
	server ServerInfo
	now    func() time.Time
}

// NewEnrollmentService wires the service. tunnel may be nil for commands that only create hosts.
func NewEnrollmentService(repo EnrollmentRepository, tunnel PeerAuthorizer, subnet netip.Prefix, server ServerInfo, now func() time.Time) *EnrollmentService {
	return &EnrollmentService{repo: repo, tunnel: tunnel, subnet: subnet, server: server, now: now}
}

// CreateHost registers a host and returns its one-time enrollment secret, which is shown once and
// never stored (only its SHA-256 is).
func (s *EnrollmentService) CreateHost(ctx context.Context, name string) (domain.Host, string, time.Time, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return domain.Host{}, "", time.Time{}, apierror.Invalid("host name must contain 1..80 bytes")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return domain.Host{}, "", time.Time{}, fmt.Errorf("telemetry: generate enrollment secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	expires := now.Add(EnrollmentTTL)
	host, err := s.repo.CreateEnrollableHost(ctx, name, HashSecret(secret), now, expires)
	if err != nil {
		return domain.Host{}, "", time.Time{}, err
	}
	return host, secret, expires, nil
}

// Enroll exchanges a one-time secret and the agent's public key for a tunnel address, and makes
// the tunnel accept the new peer.
func (s *EnrollmentService) Enroll(ctx context.Context, secret, publicKey string) (Enrollment, error) {
	if err := transport.ValidatePublicKey(publicKey); err != nil {
		return Enrollment{}, apierror.Invalid("public_key must be a base64 WireGuard public key")
	}
	peer, err := s.repo.Enroll(ctx, HashSecret(secret), publicKey, s.now(), s.subnet)
	if err != nil {
		return Enrollment{}, err
	}
	if s.tunnel != nil {
		if err := s.tunnel.AddPeer(peer.PublicKey, peer.TunnelIP); err != nil {
			return Enrollment{}, err
		}
	}
	return Enrollment{HostID: peer.HostID, TunnelIP: peer.TunnelIP, Server: s.server}, nil
}

// RestorePeers re-adds every stored peer to the tunnel (server start-up).
func (s *EnrollmentService) RestorePeers(ctx context.Context) (int, error) {
	peers, err := s.repo.Peers(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range peers {
		if err := s.tunnel.AddPeer(p.PublicKey, p.TunnelIP); err != nil {
			return 0, err
		}
	}
	return len(peers), nil
}

// HostForTunnelIP identifies the host behind a tunnel source address.
func (s *EnrollmentService) HostForTunnelIP(ctx context.Context, ip netip.Addr) (string, bool, error) {
	return s.repo.HostByTunnelIP(ctx, ip)
}

// HashSecret is the stored form of an enrollment secret.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
