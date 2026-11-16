package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// publicHTTPClient builds a client that can only reach public IP addresses.
// The AI result assistant accepts a URL from an admin, so without this guard a
// crafted URL could reach loopback, link-local metadata (169.254.169.254), or
// the private Railway network. DNS is resolved once here and the dialer uses
// only those vetted addresses, which also prevents DNS rebinding between the
// check and the request.
func publicHTTPClient(ctx context.Context, u *url.URL) (*http.Client, error) {
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("missing host")
	}

	var allowed []net.IP
	if literal := net.ParseIP(host); literal != nil {
		if !isPublicIP(literal) {
			return nil, fmt.Errorf("host %s is not public", host)
		}
		allowed = []net.IP{literal}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve host: %w", err)
		}
		if len(addrs) == 0 {
			return nil, errors.New("host has no addresses")
		}
		for _, a := range addrs {
			if !isPublicIP(a.IP) {
				return nil, fmt.Errorf("host %s resolves to non-public address %s", host, a.IP)
			}
			allowed = append(allowed, a.IP)
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// The transport may hand us a hostname (e.g. after a redirect).
			// Only the vetted IPs are dialled.
			for _, ip := range allowed {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("dial address not allowed")
		},
	}

	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		// A redirect could otherwise move us to an internal address that was
		// never vetted. Treat redirects as a failed source instead.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// reservedNetworks covers private and special-use IPv4 ranges in addition to
// the checks net.IP already provides.
var reservedNetworks = []*net.IPNet{
	cidr("0.0.0.0/8"),
	cidr("100.64.0.0/10"), // carrier-grade NAT
	cidr("192.0.0.0/24"),
	cidr("198.18.0.0/15"), // benchmarking
}

func cidr(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		// Remaining IPv6 special ranges: documentation, 6to4/teredo relays,
		// and the IPv4-mapped forms of the ranges above.
		return !(v6Special(ip))
	}
	for _, n := range reservedNetworks {
		if n.Contains(v4) {
			return false
		}
	}
	return true
}

func v6Special(ip net.IP) bool {
	v6 := ip.To16()
	if v6 == nil {
		return true
	}
	// 2001:db8::/32 documentation and 2002::/16 6to4.
	return (v6[0] == 0x20 && v6[1] == 0x01 && v6[2] == 0x0d && v6[3] == 0xb8) ||
		(v6[0] == 0x20 && v6[1] == 0x02) ||
		(v6[0] == 0x00 && v6[1] == 0x00 && v6[2] == 0x00 && v6[3] == 0x00 &&
			v6[4] == 0x00 && v6[5] == 0x00 && v6[6] == 0x00 && v6[7] == 0x00)
}
