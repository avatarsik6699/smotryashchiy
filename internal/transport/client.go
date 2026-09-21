package transport

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// keepalive keeps NAT mappings open and makes the handshake happen without waiting for traffic.
const keepalive = 15

// handshakeTimeout bounds how long NewClient waits for the first handshake.
const handshakeTimeout = 15 * time.Second

// Client is the agent side of the tunnel.
type Client struct {
	dev      *device.Device
	net      *netstack.Net
	serverIP netip.Addr
}

// ClientConfig describes one agent's tunnel.
type ClientConfig struct {
	PrivateKey      string // agent, base64
	ServerPublicKey string // base64
	Endpoint        string // server UDP host:port
	TunnelIP        netip.Addr
	ServerTunnelIP  netip.Addr
}

// NewClient brings up the agent's tunnel device.
func NewClient(cfg ClientConfig) (*Client, error) {
	priv, err := hexKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	serverPub, err := hexKey(cfg.ServerPublicKey)
	if err != nil {
		return nil, err
	}
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{cfg.TunnelIP}, nil, mtu)
	if err != nil {
		return nil, fmt.Errorf("transport: create netstack: %w", err)
	}
	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "wireguard "))
	uapi := fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=%s\nallowed_ip=%s/32\npersistent_keepalive_interval=%d\n",
		priv, serverPub, cfg.Endpoint, cfg.ServerTunnelIP, keepalive)
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("transport: configure device: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("transport: bring device up: %w", err)
	}
	c := &Client{dev: dev, net: tnet, serverIP: cfg.ServerTunnelIP}
	if err := c.waitHandshake(handshakeTimeout); err != nil {
		dev.Close()
		return nil, err
	}
	return c, nil
}

// waitHandshake blocks until the keepalive-triggered handshake completes. Sending data earlier
// makes wireguard-go start a second, concurrent handshake; the server drops it as flooding and
// the client discards the first exchange's state, which stalls the connection for the 5 s rekey
// timeout (see docs/KNOWN_GOTCHAS.md).
func (c *Client) waitHandshake(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dump, err := c.dev.IpcGet()
		if err != nil {
			return fmt.Errorf("transport: read device state: %w", err)
		}
		for _, line := range strings.Split(dump, "\n") {
			if v, ok := strings.CutPrefix(line, "last_handshake_time_sec="); ok && v != "0" {
				return nil
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("transport: no handshake with the server within %s (check the endpoint, the UDP port and that the host is enrolled)", timeout)
}

// HTTPClient returns an http.Client whose connections travel through the tunnel.
func (c *Client) HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c.net.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}}
}

// ServerIP is the server's address inside the tunnel.
func (c *Client) ServerIP() netip.Addr { return c.serverIP }

// Close shuts the tunnel down.
func (c *Client) Close() { c.dev.Close() }
