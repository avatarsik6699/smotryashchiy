package transport

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const mtu = 1420

// Server is the in-process WireGuard endpoint. Peers are added at runtime; each peer's AllowedIPs
// is exactly its own /32, so WireGuard's cryptokey routing guarantees that a packet accepted from a
// peer carries that peer's tunnel address as its source.
type Server struct {
	dev      *device.Device
	net      *netstack.Net
	ip       netip.Addr
	pub      string
	mu       sync.Mutex
	udpPort  int
	closeOne sync.Once
}

// NewServer starts the tunnel device with the given base64 private key on UDP listenPort (0 picks
// a free port) and tunnel address ip.
func NewServer(privateKey string, listenPort int, ip netip.Addr) (*Server, error) {
	priv, err := hexKey(privateKey)
	if err != nil {
		return nil, err
	}
	pub, err := PublicKey(privateKey)
	if err != nil {
		return nil, err
	}
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{ip}, nil, mtu)
	if err != nil {
		return nil, fmt.Errorf("transport: create netstack: %w", err)
	}
	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "wireguard "))
	if err := dev.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=%d\n", priv, listenPort)); err != nil {
		dev.Close()
		return nil, fmt.Errorf("transport: configure device: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("transport: bring device up: %w", err)
	}
	s := &Server{dev: dev, net: tnet, ip: ip, pub: pub}
	dump, err := dev.IpcGet()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("transport: read device state: %w", err)
	}
	for _, line := range strings.Split(dump, "\n") {
		if v, ok := strings.CutPrefix(line, "listen_port="); ok {
			s.udpPort, _ = strconv.Atoi(v)
		}
	}
	if s.udpPort == 0 {
		dev.Close()
		return nil, fmt.Errorf("transport: device did not bind a UDP port")
	}
	return s, nil
}

// PublicKey is the server's base64 WireGuard public key.
func (s *Server) PublicKey() string { return s.pub }

// UDPPort is the UDP port the device is bound to.
func (s *Server) UDPPort() int { return s.udpPort }

// TunnelIP is the server's address inside the tunnel.
func (s *Server) TunnelIP() netip.Addr { return s.ip }

// AddPeer authorizes publicKey (base64) to use exactly the address ip inside the tunnel. It is
// idempotent, so start-up can replay every stored peer.
func (s *Server) AddPeer(publicKey string, ip netip.Addr) error {
	pub, err := hexKey(publicKey)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.dev.IpcSet(fmt.Sprintf("public_key=%s\nreplace_allowed_ips=true\nallowed_ip=%s/32\n", pub, ip)); err != nil {
		return fmt.Errorf("transport: add peer: %w", err)
	}
	return nil
}

// Listen accepts TCP connections on port at the server's tunnel address only; it is unreachable
// from any other network.
func (s *Server) Listen(port int) (net.Listener, error) {
	ln, err := s.net.ListenTCP(&net.TCPAddr{IP: s.ip.AsSlice(), Port: port})
	if err != nil {
		return nil, fmt.Errorf("transport: listen in tunnel: %w", err)
	}
	return ln, nil
}

// Close shuts the tunnel down; it is safe to call more than once.
func (s *Server) Close() { s.closeOne.Do(s.dev.Close) }
