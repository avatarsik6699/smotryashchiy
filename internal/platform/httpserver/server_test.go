package httpserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// freePort finds a free TCP port on loopback by opening and immediately closing a listener.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func waitUp(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 200 * time.Millisecond, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	for {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not come up within %s: %v", url, timeout, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func selfSignedTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

func TestServePlainHTTPBehindAProxy(t *testing.T) {
	port := freePort(t)
	srv := New("127.0.0.1:" + strconv.Itoa(port))
	srv.Mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	waitUp(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/ping", 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v, want nil after a graceful Shutdown", err)
	}
}

func TestServeHTTPSServesTheAppOverTLS(t *testing.T) {
	port := freePort(t)
	srv := New("unused:0") // New's addr is ignored by ServeHTTPS (see its doc comment)
	srv.Mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	done := make(chan error, 1)
	go func() { done <- srv.ServeHTTPS("127.0.0.1:"+strconv.Itoa(port), selfSignedTLSConfig(t)) }()
	waitUp(t, "https://127.0.0.1:"+strconv.Itoa(port)+"/ping", 2*time.Second)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port) + "/ping")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("TLS listener: %v %v", resp, err)
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("HSTS = %q, want max-age=31536000 on the TLS listener", got)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("ServeHTTPS returned %v, want nil after a graceful Shutdown", err)
	}
	// The port must be free again.
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("port %d still bound after Shutdown: %v", port, err)
	}
	ln.Close()
}
