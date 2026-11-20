package outbound

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// An administrator-supplied public IP-literal HTTPS endpoint avoids intercepted
// system DNS. TLS verification and public-address checks remain enabled.
func DoHResolver(raw string) (IPLookup, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !PublicIP(net.ParseIP(endpoint.Hostname())) || (endpoint.Port() != "" && endpoint.Port() != "443") {
		return nil, errors.New("invalid_outbound_dns_config")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("dns_redirect_forbidden") }}
	return func(ctx context.Context, network, host string) ([]net.IP, error) {
		if literal := net.ParseIP(host); literal != nil {
			return []net.IP{literal}, nil
		}
		// The optional DoH mode currently uses A records for IPv4 container egress.
		// IP literals still pass the ordinary validation and address-pinning path.
		u := *endpoint
		q := u.Query()
		q.Set("name", strings.TrimSuffix(host, "."))
		q.Set("type", "A")
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return nil, errors.New("dns_resolution_failed")
		}
		req.Header.Set("Accept", "application/dns-json")
		res, err := client.Do(req)
		if err != nil {
			return nil, errors.New("dns_resolution_failed")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, errors.New("dns_resolution_failed")
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 32769))
		if err != nil || len(raw) > 32768 {
			return nil, errors.New("dns_response_invalid")
		}
		var answer struct {
			Status int
			Answer []struct {
				Type int
				Data string
			}
		}
		if json.Unmarshal(raw, &answer) != nil || answer.Status != 0 {
			return nil, errors.New("dns_resolution_failed")
		}
		var ips []net.IP
		for _, rr := range answer.Answer {
			if rr.Type == 1 {
				if ip := net.ParseIP(rr.Data); ip != nil && ip.To4() != nil {
					ips = append(ips, ip)
				}
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("dns_resolution_failed")
		}
		return ips, nil
	}, nil
}
