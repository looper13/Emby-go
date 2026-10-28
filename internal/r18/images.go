package r18

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

type imageTargetKey struct{}
type imageTarget struct {
	host string
	ips  []net.IPAddr
}
type imageTransport struct {
	base   http.RoundTripper
	lookup func(context.Context, string) ([]net.IPAddr, error)
}

var blockedImageNetworks = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
}

func publicImageIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range blockedImageNetworks {
		if prefix.Contains(a) {
			return false
		}
	}
	return true
}

// Validate every target, including redirects, and pin direct connections to
// these public addresses so a second DNS lookup cannot reach a private host.
func (t imageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if SafeHTTPURL(req.URL.String()) == "" {
		return nil, errors.New("invalid R18 image URL")
	}
	host := req.URL.Hostname()
	if net.ParseIP(host) == nil {
		var err error
		host, err = idna.Lookup.ToASCII(host)
		if err != nil {
			return nil, errors.New("invalid R18 image host")
		}
	}
	normalized := *req.URL
	port := req.URL.Port()
	normalized.Host = host
	if port != "" {
		normalized.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		normalized.Host = "[" + host + "]"
	}
	ips, err := t.lookup(req.Context(), host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("R18 image host has no public address")
	}
	for _, ip := range ips {
		if ip.Zone != "" || !publicImageIP(ip.IP) {
			return nil, errors.New("R18 image host is not public")
		}
	}
	req = req.Clone(context.WithValue(req.Context(), imageTargetKey{}, imageTarget{host, ips}))
	req.URL = &normalized
	return t.base.RoundTrip(req)
}

func NewImageClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 4
	transport.ResponseHeaderTimeout = 8 * time.Second
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if target, ok := ctx.Value(imageTargetKey{}).(imageTarget); ok && host == target.host {
			for _, ip := range target.ips {
				conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if e == nil {
					return conn, nil
				}
				err = e
			}
			return nil, err
		}
		// A proxy configured by the server administrator is trusted. Targets
		// still pass public-address validation before using that proxy.
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{
		Transport: imageTransport{transport, net.DefaultResolver.LookupIPAddr},
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || SafeHTTPURL(req.URL.String()) == "" {
				return errors.New("invalid R18 image redirect")
			}
			return nil
		},
	}
}

// Requests are deliberately independent of browser/Emby credentials.
func ImageRequest(ctx context.Context, raw string) (*http.Request, error) {
	if SafeHTTPURL(raw) == "" {
		return nil, errors.New("invalid R18 image URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err == nil {
		req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")
		req.Header.Set("User-Agent", "Emby-go/R18-Images")
	}
	return req, err
}
