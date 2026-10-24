// Package sysinfo собирает локальные сведения о машине и окружении.
package sysinfo

import (
	"bufio"
	"net/url"
	"os"
	"runtime"
	"strings"
)

// Info — параметры, которые полезно показать рядом с сетевыми данными.
type Info struct {
	Hostname string
	OS       string
	Terminal string
	Proxy    string
}

// Collect читает сведения, доступные без дополнительных привилегий.
func Collect() Info {
	return Info{
		Hostname: hostname(),
		OS:       osName(),
		Terminal: os.Getenv("TERM"),
		Proxy:    proxyFromEnv(),
	}
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

func osName() string {
	if runtime.GOOS == "linux" {
		if name := linuxPrettyName("/etc/os-release"); name != "" {
			return name
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func linuxPrettyName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		value := strings.TrimPrefix(line, "PRETTY_NAME=")
		value = strings.Trim(value, `"'`)
		return value
	}
	return ""
}

// proxyFromEnv возвращает первый заданный прокси без логина и пароля.
func proxyFromEnv() string {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		return sanitizeProxy(value)
	}
	return ""
}

func sanitizeProxy(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "настроен"
	}
	u.User = nil
	return u.String()
}
