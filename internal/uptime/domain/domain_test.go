package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

func mustInvalid(t *testing.T, n NewTarget, wantSubstr string) {
	t.Helper()
	_, err := n.Validate()
	apiErr, ok := err.(*apierror.Error)
	if !ok || apiErr.Kind != apierror.KindInvalid || !strings.Contains(apiErr.Message, wantSubstr) {
		t.Fatalf("%+v: err = %v, want Invalid containing %q", n, err, wantSubstr)
	}
}

func TestValidateAcceptsAndNormalizes(t *testing.T) {
	got, err := NewTarget{Name: "  api  ", Kind: "http", Address: " https://example.com:8443/health ", IntervalSeconds: 0}.Validate()
	if err != nil || got.Name != "api" || got.Address != "https://example.com:8443/health" || got.IntervalSeconds != DefaultInterval {
		t.Fatalf("%+v %v", got, err)
	}
	for _, n := range []NewTarget{
		{Name: "a", Kind: "tcp", Address: "db.internal:5432", IntervalSeconds: 30},
		{Name: "a", Kind: "tls", Address: "mail.example.com:465", IntervalSeconds: 3600},
		{Name: "a", Kind: "tcp", Address: "[::1]:80"},
		{Name: "a", Kind: "http", Address: "http://localhost"},
	} {
		if _, err := n.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", n, err)
		}
	}
}

func TestValidateRejectsWithFieldAddressedErrors(t *testing.T) {
	ok := NewTarget{Name: "a", Kind: "http", Address: "https://example.com"}
	mustInvalid(t, NewTarget{Name: " ", Kind: "http", Address: ok.Address}, "name")
	mustInvalid(t, NewTarget{Name: strings.Repeat("x", 81), Kind: "http", Address: ok.Address}, "name")
	mustInvalid(t, NewTarget{Name: "a", Kind: "udp", Address: "x:1"}, "kind")
	mustInvalid(t, NewTarget{Name: "a", Kind: "http", Address: ok.Address, IntervalSeconds: 5}, "interval_seconds")
	mustInvalid(t, NewTarget{Name: "a", Kind: "http", Address: ok.Address, IntervalSeconds: 7200}, "interval_seconds")
	for _, addr := range []string{"", "example.com", "ftp://example.com", "https://", "//example.com", "https://u:p@example.com", "https://example.com:99999", strings.Repeat("h", MaxURLBytes+1)} {
		mustInvalid(t, NewTarget{Name: "a", Kind: "http", Address: addr}, "target")
	}
	for _, addr := range []string{"", "example.com", ":80", "example.com:0", "example.com:70000", "example.com:abc", "a b:80", "http://example.com:80", "user@example.com:80"} {
		mustInvalid(t, NewTarget{Name: "a", Kind: "tcp", Address: addr}, "target")
	}
}

func TestLatencyPointMarshalsAsPairWithNullForFailure(t *testing.T) {
	ms := int64(42)
	raw, _ := json.Marshal([]LatencyPoint{{TS: 1000, Latency: &ms}, {TS: 2000, Latency: nil}})
	if string(raw) != `[[1000,42],[2000,null]]` {
		t.Fatalf("%s", raw)
	}
}

func TestResultKeepsNullsExplicit(t *testing.T) {
	raw, _ := json.Marshal(Result{TargetID: "t"})
	for _, want := range []string{`"latency_ms":null`, `"status_code":null`, `"cert_expires_at":null`, `"ok":false`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s lacks %s", raw, want)
		}
	}
}
