package transport

// T1 spike (docs/changes/04): can userspace WireGuard (wireguard-go + gVisor netstack) carry HTTP
// between a server and an agent inside one process pair, with no root and no kernel module?

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

func newKey(t *testing.T) (priv, pub string) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(k.Bytes()), hex.EncodeToString(k.PublicKey().Bytes())
}

func startDevice(t *testing.T, addr string) (*device.Device, *netstack.Net) {
	t.Helper()
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr(addr)}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "wg "))
	t.Cleanup(dev.Close)
	return dev, tnet
}

func TestSpikeHTTPOverUserspaceWireGuard(t *testing.T) {
	serverPriv, serverPub := newKey(t)
	agentPriv, agentPub := newKey(t)

	server, serverNet := startDevice(t, "10.99.0.1")
	if err := server.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=0\npublic_key=%s\nallowed_ip=10.99.0.2/32\n", serverPriv, agentPub)); err != nil {
		t.Fatal(err)
	}
	if err := server.Up(); err != nil {
		t.Fatal(err)
	}
	dump, err := server.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	var port string
	for _, line := range strings.Split(dump, "\n") {
		if v, ok := strings.CutPrefix(line, "listen_port="); ok {
			port = v
		}
	}
	if port == "" || port == "0" {
		t.Fatalf("server has no UDP listen port:\n%s", dump)
	}

	ln, err := serverNet.ListenTCP(&net.TCPAddr{Port: 8443})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		fmt.Fprintf(w, "hello from %s, you are %s", "10.99.0.1", host)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })

	agent, agentNet := startDevice(t, "10.99.0.2")
	if err := agent.IpcSet(fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=127.0.0.1:%s\nallowed_ip=10.99.0.1/32\npersistent_keepalive_interval=1\n", agentPriv, serverPub, port)); err != nil {
		t.Fatal(err)
	}
	if err := agent.Up(); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return agentNet.DialContext(ctx, network, addr)
		},
	}}
	resp, err := client.Get("http://10.99.0.1:8443/")
	if err != nil {
		t.Fatalf("HTTP over tunnel failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "hello from 10.99.0.1, you are 10.99.0.2" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}
