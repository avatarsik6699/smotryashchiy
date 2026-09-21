package infrastructure

import (
	"net/netip"
	"testing"
)

func TestNextFreeAddrSkipsNetworkServerAndBroadcast(t *testing.T) {
	subnet := netip.MustParsePrefix("10.99.0.0/29") // .0 network, .1 server, .2-.6 hosts, .7 broadcast
	used := map[netip.Addr]bool{}
	var got []string
	for {
		ip, ok := nextFreeAddr(subnet, used)
		if !ok {
			break
		}
		got = append(got, ip.String())
		used[ip] = true
	}
	want := []string{"10.99.0.2", "10.99.0.3", "10.99.0.4", "10.99.0.5", "10.99.0.6"}
	if len(got) != len(want) {
		t.Fatalf("allocated %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allocated %v, want %v", got, want)
		}
	}
}

func TestNextFreeAddrReusesTheLowestGap(t *testing.T) {
	subnet := netip.MustParsePrefix("10.99.0.0/24")
	used := map[netip.Addr]bool{netip.MustParseAddr("10.99.0.2"): true, netip.MustParseAddr("10.99.0.4"): true}
	if ip, _ := nextFreeAddr(subnet, used); ip.String() != "10.99.0.3" {
		t.Fatalf("got %s, want the lowest free address 10.99.0.3", ip)
	}
}
