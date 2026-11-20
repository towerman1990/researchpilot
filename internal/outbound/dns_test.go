package outbound

import (
	"testing"
)

func TestDoHRequiresTrustedPublicIPBootstrap(t *testing.T) {
	for _, raw := range []string{"http://1.1.1.1/dns-query", "https://localhost/resolve", "https://dns.example/resolve", "https://127.0.0.1/resolve", "https://[::1]/resolve", "https://1.1.1.1:444/resolve", "https://user:password@1.1.1.1/resolve"} {
		if _, err := DoHResolver(raw); err == nil {
			t.Fatal("unsafe DNS bootstrap", raw)
		}
	}
	if _, err := DoHResolver("https://1.1.1.1/dns-query"); err != nil {
		t.Fatal(err)
	}
}
