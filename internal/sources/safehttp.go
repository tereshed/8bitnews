package sources

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned (wrapped) when a request was refused
// because its destination resolved to an address this package's SSRF
// protection does not allow to be dialed, or because its scheme is not
// http/https.
//
// The URLs Checker and WaybackClient dial come from research dossiers that
// PolyMaths agents write after reading untrusted pages found on the
// internet, and the hub runs in the cloud next to its own database and
// internal services. Without this check, a crafted or redirected URL could
// make the hub fetch a cloud metadata endpoint (169.254.169.254), a
// loopback/internal service (localhost, the postgres container, ...), or
// any other address not meant to be reachable from the public internet.
var ErrBlockedAddress = errors.New("sources: refusing to connect to a blocked address")

// maxRedirects bounds how many redirects Checker and WaybackClient follow,
// so a redirect loop (or a chain designed to exhaust resources) cannot run
// unbounded.
const maxRedirects = 5

// maxDialTimeout bounds the TCP connect step of every outbound request, so
// a slow/firewalled internal address cannot stall a check far longer than
// its own request timeout would suggest.
const maxDialTimeout = 10 * time.Second

// newSafeHTTPClient builds an http.Client hardened against SSRF: schemes
// other than http/https are refused by CheckRedirect (the initial request's
// scheme is validated by callers before this client ever sees it), the
// dialer's Control refuses to connect to loopback/private/link-local/CGNAT/
// multicast/unspecified addresses (checked against the real IP the socket
// is about to connect to, which also defeats DNS rebinding and redirects
// aimed at internal addresses — Control runs again for every redirect,
// since each one is a fresh dial), and redirects are capped.
//
// allowPrivate lifts the IP restriction. It exists only for tests that
// point these clients at httptest servers on 127.0.0.1/::1
// (HUB_SOURCE_ALLOW_PRIVATE=true); production must always leave it false.
func newSafeHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: maxDialTimeout}
	if !allowPrivate {
		dialer.Control = controlBlockPrivate
	}
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if !isAllowedScheme(req.URL) {
				return fmt.Errorf("%w: redirected to scheme %q", ErrBlockedAddress, req.URL.Scheme)
			}
			return nil
		},
	}
}

// isAllowedScheme reports whether u uses http or https; every other scheme
// (file, ftp, gopher, custom internal ones, ...) is refused.
func isAllowedScheme(u *url.URL) bool {
	return u.Scheme == "http" || u.Scheme == "https"
}

// controlBlockPrivate is a net.Dialer.Control that refuses to let the
// dialer finish connecting to an address isBlockedIP considers internal.
// Control receives the address actually being dialed after DNS resolution,
// so this check happens against the real destination IP, not the
// pre-resolution hostname.
func controlBlockPrivate(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlockedAddress, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: non-IP address %q", ErrBlockedAddress, address)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	return nil
}

// cgnatBlock is 100.64.0.0/10, the shared address space RFC 6598 reserves
// for carrier-grade NAT. net.IP has no IsPrivate-style helper for it.
var cgnatBlock = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

// isBlockedIP reports whether ip is loopback, private (RFC 1918 and the
// fc00::/7 IPv6 ULA range, via net.IP.IsPrivate), link-local (unicast or
// multicast, including 169.254.0.0/16 and fe80::/10), CGNAT, multicast, or
// unspecified. IPv4-mapped IPv6 addresses (::ffff:a.b.c.d) are unwrapped to
// their IPv4 form first so they cannot slip past the IPv4 checks.
func isBlockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsMulticast(),
		ip.IsUnspecified():
		return true
	case cgnatBlock.Contains(ip):
		return true
	}
	return false
}
