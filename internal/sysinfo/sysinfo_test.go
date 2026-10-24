package sysinfo

import (
	"strings"
	"testing"
)

func TestSanitizeProxyStripsCredentials(t *testing.T) {
	got := sanitizeProxy("http://user:secret@proxy.example:3128")
	if strings.Contains(got, "user") || strings.Contains(got, "secret") {
		t.Fatalf("credentials leaked: %q", got)
	}
	if got != "http://proxy.example:3128" {
		t.Fatalf("sanitizeProxy = %q", got)
	}
}

func TestProxyFromEnv(t *testing.T) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "socks5://user:pass@127.0.0.1:1080")
	got := proxyFromEnv()
	if got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyFromEnv = %q", got)
	}
}

func TestCollectFillsOS(t *testing.T) {
	info := Collect()
	if info.OS == "" {
		t.Fatal("empty OS")
	}
}
