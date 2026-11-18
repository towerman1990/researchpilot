package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

func publicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001::/23", "2002::/16", "fec0::/10", "::/96", "192.88.99.0/24"} {
		if netip.MustParsePrefix(cidr).Contains(address) {
			return false
		}
	}
	return true
}

func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid_public_url")
	}
	if p := u.Port(); p != "" && p != "443" && p != "80" {
		return nil, errors.New("port_forbidden")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return nil, errors.New("private_address_forbidden")
	}
	if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return nil, errors.New("private_address_forbidden")
	}
	return u, nil
}

func outboundClient(timeout time.Duration, redirect bool) *http.Client {
	lookup := ipLookup(net.DefaultResolver.LookupIP)
	if raw := os.Getenv("OUTBOUND_DOH_URL"); raw != "" {
		custom, err := dohResolver(raw)
		if err != nil {
			lookup = func(context.Context, string, string) ([]net.IP, error) { return nil, err }
		} else {
			lookup = custom
		}
	}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          20, IdleConnTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolvePublic(ctx, host, lookup)
			if err != nil {
				return nil, err
			}
			dialer := net.Dialer{Timeout: 10 * time.Second}
			var last error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !redirect || len(via) > 3 {
			return errors.New("redirect_forbidden")
		}
		_, err := validateURL(req.URL.String())
		return err
	}}
}

func readLimited(response *http.Response, max int64) ([]byte, error) {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream_http_%d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		return nil, errors.New("upstream_read_failed")
	}
	if int64(len(raw)) > max {
		return nil, errors.New("response_too_large")
	}
	return raw, nil
}

type ipLookup func(context.Context, string, string) ([]net.IP, error)

func resolvePublic(ctx context.Context, host string, lookup ipLookup) ([]net.IP, error) {
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("dns_resolution_failed")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("private_address_forbidden")
		}
	}
	return ips, nil
}
