package httpserver

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/avatarsik6699/smotryashchiy/internal/platform/apierror"
)

// TunnelPeer is the source address of a request that arrived on the tunnel listener: agents are
// identified by their tunnel IP, so an unparsable peer is an unknown agent (401), not a bad request.
func TunnelPeer(r *http.Request) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, apierror.Unauthorized("unknown tunnel peer")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, apierror.Unauthorized("unknown tunnel peer")
	}
	return ip.Unmap(), nil
}
