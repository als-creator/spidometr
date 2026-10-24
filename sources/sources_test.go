package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAllSourcesAreUsable(t *testing.T) {
	seen := make(map[string]bool)

	for _, src := range All() {
		if src.ID() == "" || src.Name() == "" {
			t.Errorf("пустые ID или имя у источника %+v", src)
		}
		if seen[src.ID()] {
			t.Errorf("повторяющийся ID %q", src.ID())
		}
		seen[src.ID()] = true

		if dynamic, ok := src.(*dynamicSource); ok {
			if dynamic.resolve == nil {
				t.Errorf("%s: динамический источник без resolver", src.ID())
			}
			continue
		}
		for label, raw := range map[string]string{
			"download": src.DownloadURL(),
			"upload":   src.UploadURL(),
		} {
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				t.Errorf("%s: некорректный %s URL %q", src.ID(), label, raw)
			}
		}
		if src.UploadURL() != "" && src.UploadName() == "" {
			t.Errorf("%s: upload URL без имени API", src.ID())
		}
	}
}

func TestGet(t *testing.T) {
	src, ok := Get("yandex")
	if !ok || src.ID() != "yandex" {
		t.Fatalf("Get(yandex) = %v, %v", src, ok)
	}
	if _, ok := Get("missing"); ok {
		t.Fatal("Get(missing) вернул источник")
	}
}

func TestOoklaDownloadURL(t *testing.T) {
	got := ooklaDownloadURL("http://speed.example:8080/speedtest/upload.php")
	want := "http://speed.example:8080/speedtest/random4000x4000.jpg"
	if got != want {
		t.Fatalf("ooklaDownloadURL = %q, want %q", got, want)
	}
}

func TestResolveFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"targets":[{"url":"https://fast.example/video.mp4"}]}`))
	}))
	defer srv.Close()

	old := fastAPI
	defer func() { fastAPI = old }()
	fastAPI = srv.URL

	got, err := resolveFast(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://fast.example/video.mp4" {
		t.Fatalf("resolveFast = %q", got)
	}
}

func TestResolveOokla(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"url":"http://speed.example/speedtest/upload.php"}]`))
	}))
	defer srv.Close()

	old := ooklaAPI
	defer func() { ooklaAPI = old }()
	ooklaAPI = srv.URL

	got, err := resolveOokla(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://speed.example/speedtest/random4000x4000.jpg" {
		t.Fatalf("resolveOokla = %q", got)
	}
}

func TestDynamicSourceCaches(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"targets":[{"url":"https://fast.example/video.mp4"}]}`))
	}))
	defer srv.Close()

	old := fastAPI
	defer func() { fastAPI = old }()
	fastAPI = srv.URL

	src := &dynamicSource{resolve: resolveFast}
	if got := src.DownloadURL(); got != "https://fast.example/video.mp4" {
		t.Fatalf("first DownloadURL = %q", got)
	}
	if got := src.DownloadURL(); got != "https://fast.example/video.mp4" {
		t.Fatalf("second DownloadURL = %q", got)
	}
	if hits != 1 {
		t.Fatalf("resolver called %d times, want 1", hits)
	}
}
