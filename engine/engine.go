// Package engine реализует многопоточные замеры скачивания и отдачи.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// readBufferSize — фиксированный буфер на поток: гигабайты не оседают в ОЗУ.
	readBufferSize = 64 * 1024
	// uploadChunkSize — объём одного POST; на медленном канале запрос
	// обрывается по контексту, уже отправленные байты учитываются.
	uploadChunkSize = 16 << 20
	retryDelay      = 250 * time.Millisecond
	userAgent       = "github.com/als-creator/spidometr/1.0"
)

var errEmptyResponse = errors.New("источник вернул пустой ответ")

// Stats — итоги одной фазы замера.
type Stats struct {
	Bytes    int64
	Requests int64
	Errors   int64
	FirstErr error
}

type counters struct {
	bytes    atomic.Int64
	requests atomic.Int64
	errors   atomic.Int64

	mu       sync.Mutex
	firstErr error
}

func (c *counters) recordErr(err error) {
	c.errors.Add(1)
	c.mu.Lock()
	if c.firstErr == nil {
		c.firstErr = err
	}
	c.mu.Unlock()
}

func (c *counters) snapshot() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{
		Bytes:    c.bytes.Load(),
		Requests: c.requests.Load(),
		Errors:   c.errors.Load(),
		FirstErr: c.firstErr,
	}
}

// RunDownload запускает workers, которые скачивают файл до отмены ctx.
// После EOF каждый поток начинает загрузку заново.
// onBytes может быть nil; он вызывается из воркеров при каждом прочитанном
// или отправленном фрагменте.
func RunDownload(ctx context.Context, rawURL string, workers int, onBytes func(int)) Stats {
	return run(ctx, workers, func(ctx context.Context, client *http.Client, c *counters) {
		downloadWorker(ctx, client, rawURL, onBytes, c)
	})
}

// RunUpload запускает workers, которые отправляют данные до отмены ctx.
func RunUpload(ctx context.Context, rawURL string, workers int, onBytes func(int)) Stats {
	return run(ctx, workers, func(ctx context.Context, client *http.Client, c *counters) {
		uploadWorker(ctx, client, rawURL, onBytes, c)
	})
}

func run(ctx context.Context, workers int, work func(context.Context, *http.Client, *counters)) Stats {
	client := newClient()
	c := &counters{}

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			work(ctx, client, c)
		}()
	}
	wg.Wait()
	return c.snapshot()
}

func downloadWorker(ctx context.Context, client *http.Client, rawURL string, onBytes func(int), c *counters) {
	buf := make([]byte, readBufferSize)

	for ctx.Err() == nil {
		n, err := downloadOnce(ctx, client, rawURL, onBytes, c, buf)
		c.requests.Add(1)

		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.recordErr(err)
			if !sleep(ctx, retryDelay) {
				return
			}
			continue
		}
		if n == 0 {
			c.recordErr(errEmptyResponse)
			if !sleep(ctx, retryDelay) {
				return
			}
		}
	}
}

// downloadOnce скачивает файл до EOF и возвращает число прочитанных байт.
func downloadOnce(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	onBytes func(int),
	c *counters,
	buf []byte,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, withCacheBuster(rawURL), nil)
	if err != nil {
		return 0, fmt.Errorf("запрос: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("соединение: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("источник ответил %s", resp.Status)
	}

	var total int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			c.bytes.Add(int64(n))
			report(onBytes, n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, fmt.Errorf("чтение: %w", readErr)
		}
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func uploadWorker(ctx context.Context, client *http.Client, rawURL string, onBytes func(int), c *counters) {
	payload := make([]byte, 32*1024)
	for i := range payload {
		payload[i] = byte(i)
	}

	for ctx.Err() == nil {
		n, err := uploadOnce(ctx, client, rawURL, onBytes, c, payload)
		c.requests.Add(1)

		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.recordErr(err)
			if !sleep(ctx, retryDelay) {
				return
			}
			continue
		}
		if n == 0 {
			c.recordErr(errEmptyResponse)
			if !sleep(ctx, retryDelay) {
				return
			}
		}
	}
}

// uploadOnce отправляет один чанк и считает байты, реально прочитанные
// транспортом из тела запроса.
func uploadOnce(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	onBytes func(int),
	c *counters,
	payload []byte,
) (int64, error) {
	body := &countingReader{
		reader: io.LimitReader(&repeatReader{buf: payload}, uploadChunkSize),
		onRead: func(n int) {
			c.bytes.Add(int64(n))
			report(onBytes, n)
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, body)
	if err != nil {
		return 0, fmt.Errorf("запрос: %w", err)
	}
	req.ContentLength = uploadChunkSize
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return body.n, fmt.Errorf("соединение: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return body.n, fmt.Errorf("upload API ответил %s", resp.Status)
	}
	return body.n, nil
}

type countingReader struct {
	reader io.Reader
	onRead func(int)
	n      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.n += int64(n)
		r.onRead(n)
	}
	return n, err
}

// repeatReader бесконечно повторяет буфер, не выделяя память под каждый запрос.
type repeatReader struct {
	buf []byte
}

func (r *repeatReader) Read(p []byte) (int, error) {
	return copy(p, r.buf), nil
}

func report(onBytes func(int), n int) {
	if onBytes != nil {
		onBytes(n)
	}
}

func newClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 64
	transport.DisableCompression = true
	transport.ForceAttemptHTTP2 = true
	transport.ResponseHeaderTimeout = 15 * time.Second

	return &http.Client{Transport: transport}
}

// ProbeLatency измеряет задержку HTTP-запроса до источника: время до первого
// байта ответа, медиана из трёх попыток. Это не ICMP, но работает без root.
func ProbeLatency(ctx context.Context, rawURL string) (time.Duration, error) {
	if rawURL == "" {
		return 0, errors.New("пустой URL источника")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	client := newClient()
	var samples []time.Duration
	var firstErr error
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, withCacheBuster(rawURL), nil)
		if err != nil {
			return 0, fmt.Errorf("запрос: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Range", "bytes=0-0")

		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("соединение: %w", err)
			}
			continue
		}
		_, readErr := resp.Body.Read(make([]byte, 1))
		elapsed := time.Since(start)
		resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			if firstErr == nil {
				firstErr = fmt.Errorf("источник ответил %s", resp.Status)
			}
			continue
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if firstErr == nil {
				firstErr = fmt.Errorf("чтение: %w", readErr)
			}
			continue
		}
		samples = append(samples, elapsed)
	}
	if len(samples) == 0 {
		if firstErr == nil {
			firstErr = errors.New("не удалось измерить задержку")
		}
		return 0, firstErr
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2], nil
}

// withCacheBuster добавляет уникальный параметр, чтобы CDN не отдал ответ из кэша.
func withCacheBuster(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := u.Query()
	query.Set("r", strconv.FormatInt(time.Now().UnixNano(), 10))
	u.RawQuery = query.Encode()
	return u.String()
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
