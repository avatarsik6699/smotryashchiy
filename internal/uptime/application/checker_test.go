package application

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
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avatarsik6699/smotryashchiy/internal/uptime/domain"
)

var fixed = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func checker(pool *x509.CertPool) *Checker {
	return &Checker{Timeout: 2 * time.Second, RootCAs: pool, Now: func() time.Time { return fixed }}
}

func target(kind domain.Kind, addr string) domain.Target {
	return domain.Target{ID: "t1", Name: "t", Kind: kind, Address: addr, IntervalSeconds: 60}
}

func TestHTTPOKRecordsStatusAndLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) }))
	defer srv.Close()
	res := checker(nil).Check(context.Background(), target(domain.KindHTTP, srv.URL))
	if !res.OK || res.Error != "" || res.StatusCode == nil || *res.StatusCode != 200 || res.LatencyMS == nil || *res.LatencyMS < 0 || res.CertExpiresAt != nil {
		t.Fatalf("%+v", res)
	}
	if !res.TS.Equal(fixed) || res.TargetID != "t1" {
		t.Fatalf("ts/target = %v %q", res.TS, res.TargetID)
	}
}

func TestHTTPBadStatusIsAFailureButKeepsTheRealLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	res := checker(nil).Check(context.Background(), target(domain.KindHTTP, srv.URL))
	if res.OK || res.Error != "unexpected status 500" || res.StatusCode == nil || *res.StatusCode != 500 || res.LatencyMS == nil {
		t.Fatalf("%+v", res)
	}
}

func TestHTTPFollowsRedirectsButNotForever(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/end", http.StatusFound) })
	mux.HandleFunc("/end", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if res := checker(nil).Check(context.Background(), target(domain.KindHTTP, srv.URL+"/start")); !res.OK {
		t.Fatalf("redirect to a good page failed: %+v", res)
	}
	res := checker(nil).Check(context.Background(), target(domain.KindHTTP, srv.URL+"/loop"))
	if res.OK || !strings.Contains(res.Error, "redirects") || res.LatencyMS != nil {
		t.Fatalf("loop: %+v", res)
	}
}

func TestHTTPTimeoutIsReportedAsTimeoutWithoutLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	c := checker(nil)
	c.Timeout = 300 * time.Millisecond
	res := c.Check(context.Background(), target(domain.KindHTTP, srv.URL))
	if res.OK || !strings.HasPrefix(res.Error, "timeout after") || res.LatencyMS != nil {
		t.Fatalf("%+v", res)
	}
}

func TestHTTPRefusedConnectionHasNoLatencyNotZero(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	res := checker(nil).Check(context.Background(), target(domain.KindHTTP, "http://"+addr))
	if res.OK || res.LatencyMS != nil || res.StatusCode != nil || !strings.Contains(res.Error, "refused") {
		t.Fatalf("%+v", res)
	}
	if strings.Contains(res.Error, "http://") {
		t.Fatalf("the URL wrapper must be stripped: %q", res.Error)
	}
}

func TestHTTPSVerifiesTheCertificateAndReportsExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	res := checker(pool).Check(context.Background(), target(domain.KindHTTP, srv.URL))
	if !res.OK || res.CertExpiresAt == nil || !res.CertExpiresAt.Equal(srv.Certificate().NotAfter.UTC()) {
		t.Fatalf("trusted cert: %+v", res)
	}
	untrusted := checker(nil).Check(context.Background(), target(domain.KindHTTP, srv.URL))
	if untrusted.OK || !strings.Contains(untrusted.Error, "certificate") {
		t.Fatalf("an untrusted certificate must fail verification: %+v", untrusted)
	}
}

func TestTCPOpenAndClosed(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	up := checker(nil).Check(context.Background(), target(domain.KindTCP, ln.Addr().String()))
	if !up.OK || up.LatencyMS == nil || up.StatusCode != nil {
		t.Fatalf("%+v", up)
	}
	addr := ln.Addr().String()
	ln.Close()
	down := checker(nil).Check(context.Background(), target(domain.KindTCP, addr))
	if down.OK || down.LatencyMS != nil || down.Error == "" {
		t.Fatalf("%+v", down)
	}
}

func selfSigned(t *testing.T, notBefore, notAfter time.Time, host string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: host},
		NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{host}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, leaf
}

func tlsListener(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

func TestTLSTargetReportsDaysAndFailsOnAnExpiredCertificate(t *testing.T) {
	good, goodLeaf := selfSigned(t, fixed.Add(-time.Hour), fixed.Add(9*24*time.Hour), "localhost")
	pool := x509.NewCertPool()
	pool.AddCert(goodLeaf)
	res := checker(pool).Check(context.Background(), target(domain.KindTLS, tlsListener(t, good)))
	if !res.OK || res.CertExpiresAt == nil || !res.CertExpiresAt.Equal(goodLeaf.NotAfter.UTC()) || res.LatencyMS == nil {
		t.Fatalf("%+v", res)
	}

	real := time.Now()
	expired, expiredLeaf := selfSigned(t, real.Add(-48*time.Hour), real.Add(-24*time.Hour), "localhost")
	pool2 := x509.NewCertPool()
	pool2.AddCert(expiredLeaf)
	bad := checker(pool2).Check(context.Background(), target(domain.KindTLS, tlsListener(t, expired)))
	if bad.OK || !strings.Contains(bad.Error, "expired") || bad.LatencyMS != nil {
		t.Fatalf("an expired certificate must fail: %+v", bad)
	}
}

func TestTLSTargetOnAPlainPortFails(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("HTTP/1.1 400 nope\r\n\r\n"))
			c.Close()
		}
	}()
	res := checker(nil).Check(context.Background(), target(domain.KindTLS, ln.Addr().String()))
	if res.OK || res.Error == "" {
		t.Fatalf("%+v", res)
	}
}

func TestErrorTextIsBoundedAndUnknownKindIsAFailure(t *testing.T) {
	if got := trimError(strings.Repeat("x", 1000)); len(got) > maxErrorLen+4 {
		t.Fatalf("len = %d", len(got))
	}
	res := checker(nil).Check(context.Background(), target("udp", "x:1"))
	if res.OK || !strings.Contains(res.Error, "unknown target kind") {
		t.Fatalf("%+v", res)
	}
}
