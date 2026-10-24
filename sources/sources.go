// Package sources содержит провайдеров файлов для замера скорости.
//
// Все адреса — крупные файлы на зеркалах и CDN. Это важно: домашние страницы
// провайдеров отдают несколько килобайт HTML и не позволяют измерить
// пропускную способность.
package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SpeedSource описывает API-источник для замера скачивания и отдачи.
type SpeedSource interface {
	ID() string
	Name() string
	DownloadURL() string
	UploadURL() string
	UploadName() string
}

type source struct {
	id         string
	name       string
	download   string
	upload     string
	uploadName string
}

func (s source) ID() string          { return s.id }
func (s source) Name() string        { return s.name }
func (s source) DownloadURL() string { return s.download }
func (s source) UploadURL() string   { return s.upload }
func (s source) UploadName() string  { return s.uploadName }

// dynamicSource получает URL у API провайдера и кэширует его.
type dynamicSource struct {
	source
	mu      sync.Mutex
	url     string
	fetched time.Time
	resolve func(context.Context) (string, error)
}

const dynamicTTL = 10 * time.Minute

func (d *dynamicSource) DownloadURL() string {
	d.mu.Lock()
	if d.url != "" && time.Since(d.fetched) < dynamicTTL {
		url := d.url
		d.mu.Unlock()
		return url
	}
	resolve := d.resolve
	d.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url, err := resolve(ctx)
	if err != nil {
		return ""
	}
	d.mu.Lock()
	d.url = url
	d.fetched = time.Now()
	d.mu.Unlock()
	return url
}

// Адреса вынесены в переменные, чтобы тесты могли подставить httptest-сервер.
var (
	fastAPI    = "https://api.fast.com/netflix/speedtest/v2?https=true&token=YXNkZmFzdGVzdC5jb20%3D&urlCount=1"
	ooklaAPI   = "https://www.speedtest.net/api/js/servers?engine=js&limit=5"
	httpClient = &http.Client{Timeout: 8 * time.Second}
)

// Источники проверены на доступность и поддержку больших объёмов.
// Yandex идёт первым и остаётся источником по умолчанию.
var all = []SpeedSource{
	source{
		id:         "yandex",
		name:       "Yandex CDN — bigfile.png",
		download:   "https://cdnrphoszsa2sp7ilm7a.svc.cdn.yandex.net/bigfile.png",
		upload:     "https://cdnrrtcrwqtgkafu6ulm.svc.cdn.yandex.net/upload",
		uploadName: "Yandex CDN",
	},
	source{
		id:         "corbina",
		name:       "Beeline / Corbina — 100Mb",
		download:   "https://mirror.corbina.net/100Mb",
		upload:     "https://speed.cloudflare.com/__up",
		uploadName: "Cloudflare",
	},
	source{
		id:         "truenetwork",
		name:       "Truenetwork — Debian netinst ISO",
		download:   "https://mirror.truenetwork.ru/debian-cd/current/amd64/iso-cd/debian-13.7.0-amd64-netinst.iso",
		upload:     "https://speed.cloudflare.com/__up",
		uploadName: "Cloudflare",
	},
	source{
		id:         "cloudflare",
		name:       "Cloudflare CDN — 100 MB",
		download:   "https://speed.cloudflare.com/__down?bytes=100000000",
		upload:     "https://speed.cloudflare.com/__up",
		uploadName: "Cloudflare",
	},
	source{
		id:         "librespeed",
		name:       "LibreSpeed — public node",
		download:   "https://librespeed.org/backend/garbage.php?ckSize=100",
		upload:     "https://librespeed.org/backend/empty.php",
		uploadName: "LibreSpeed",
	},
	&dynamicSource{
		source: source{
			id:         "fast",
			name:       "Fast.com (Netflix CDN)",
			uploadName: "—",
		},
		resolve: resolveFast,
	},
	&dynamicSource{
		source: source{
			id:         "ookla",
			name:       "Ookla Speedtest — public server",
			uploadName: "—",
		},
		resolve: resolveOokla,
	},
}

// All возвращает копию списка источников.
func All() []SpeedSource {
	return append([]SpeedSource(nil), all...)
}

// Get находит источник по идентификатору.
func Get(id string) (SpeedSource, bool) {
	for _, s := range all {
		if s.ID() == id {
			return s, true
		}
	}
	return nil, false
}

func resolveFast(ctx context.Context) (string, error) {
	var v struct {
		Targets []struct {
			URL string `json:"url"`
		} `json:"targets"`
	}
	if err := getJSON(ctx, fastAPI, &v); err != nil {
		return "", fmt.Errorf("fast.com: %w", err)
	}
	for _, target := range v.Targets {
		if target.URL != "" {
			return target.URL, nil
		}
	}
	return "", errors.New("fast.com: пустой список целей")
}

func resolveOokla(ctx context.Context) (string, error) {
	var servers []struct {
		URL string `json:"url"`
	}
	if err := getJSON(ctx, ooklaAPI, &servers); err != nil {
		return "", fmt.Errorf("ookla: %w", err)
	}
	for _, srv := range servers {
		if srv.URL != "" {
			return ooklaDownloadURL(srv.URL), nil
		}
	}
	return "", errors.New("ookla: нет доступных серверов")
}

func ooklaDownloadURL(serverURL string) string {
	if strings.Contains(serverURL, "upload.php") {
		return strings.Replace(serverURL, "upload.php", "random4000x4000.jpg", 1)
	}
	return strings.TrimRight(serverURL, "/") + "/random4000x4000.jpg"
}

func getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) spidometr/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://fast.com/")
	req.Header.Set("Origin", "https://fast.com")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
