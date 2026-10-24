package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	latency, err := ProbeLatency(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if latency < 5*time.Millisecond || latency > 2*time.Second {
		t.Fatalf("unexpected latency %v", latency)
	}
}

func TestProbeLatencyRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := ProbeLatency(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProbeLatencyEmptyURL(t *testing.T) {
	if _, err := ProbeLatency(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}
