//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"memoryelaine/internal/database"
	"memoryelaine/internal/proxy"
)

// helper to set up a full proxy stack for testing
func setupIntegrationProxy(t *testing.T, upstreamHandler http.Handler, maxCapture int) (
	proxyURL string,
	reader *database.LogReader,
	writer *database.LogWriter,
	cleanup func(),
) {
	t.Helper()

	upstream := httptest.NewServer(upstreamHandler)
	upstreamURL, _ := url.Parse(upstream.URL)

	dbPath := filepath.Join(t.TempDir(), "integration.db")

	writerDB, err := database.OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	readerDB, err := database.OpenReader(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	lw, err := database.NewLogWriter(writerDB, 1000)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lw.Run(ctx)
	}()

	timeout := 5 * time.Second
	rpPlain := proxy.NewPlainReverseProxy(upstreamURL, timeout)
	rpCapture := proxy.NewReverseProxy(upstreamURL, timeout, maxCapture)

	logPaths := map[string]struct{}{
		"/v1/chat/completions": {},
		"/v1/completions":      {},
		"/test":                {},
	}

	handler := proxy.Handler(rpPlain, rpCapture, logPaths, maxCapture, lw, upstreamURL)
	proxySrv := httptest.NewServer(handler)

	lr := database.NewLogReader(readerDB)

	cleanupFn := func() {
		proxySrv.Close()
		cancel()
		wg.Wait()
		lw.Close()
		writerDB.Close()
		readerDB.Close()
		upstream.Close()
	}

	return proxySrv.URL, lr, lw, cleanupFn
}

// Acceptance Test 1: Zero-Latency Streaming
func TestAcceptance_ZeroLatencyStreaming(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream does not support Flusher")
		}
		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, "data: chunk %d\n\n", i)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})

	proxyURL, reader, _, cleanup := setupIntegrationProxy(t, upstream, 8*1024*1024)
	defer cleanup()

	resp, err := http.Post(proxyURL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"gpt-4"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read chunks and verify they arrive in a timely fashion
	buf := make([]byte, 4096)
	var allData strings.Builder
	chunkCount := 0
	start := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			allData.Write(buf[:n])
			chunkCount++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	elapsed := time.Since(start)
	// 10 chunks × 50ms = ~500ms; should finish well under 2s (with no artificial buffering)
	if elapsed > 2*time.Second {
		t.Errorf("streaming took too long: %v (expected ~500ms)", elapsed)
	}

	if !strings.Contains(allData.String(), "chunk 9") {
		t.Error("did not receive all 10 chunks")
	}

	// Verify DB has the entry
	time.Sleep(200 * time.Millisecond)
	entries, err := reader.Query(database.DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 DB entry, got %d", len(entries))
	}
	if entries[0].RespBody == nil || !strings.Contains(*entries[0].RespBody, "chunk 9") {
		t.Error("DB entry should contain captured SSE data")
	}
}

// Acceptance Test 2: Truncation
func TestAcceptance_Truncation(t *testing.T) {
	bodySize := 15 * 1024 * 1024 // 15 MB
	data := strings.Repeat("X", bodySize)
	dataHash := sha256.Sum256([]byte(data))

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	})

	maxCapture := 8 * 1024 * 1024 // 8 MB
	proxyURL, reader, _, cleanup := setupIntegrationProxy(t, upstream, maxCapture)
	defer cleanup()

	resp, err := http.Get(proxyURL + "/test")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Client must receive ALL 15MB
	if len(body) != bodySize {
		t.Errorf("client received %d bytes, expected %d", len(body), bodySize)
	}
	clientHash := sha256.Sum256(body)
	if clientHash != dataHash {
		t.Error("client data SHA-256 mismatch")
	}

	// Verify DB
	time.Sleep(300 * time.Millisecond)
	entries, err := reader.Query(database.DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if !e.RespTruncated {
		t.Error("expected resp_truncated = true")
	}
	if e.RespBytes != int64(bodySize) {
		t.Errorf("expected resp_bytes = %d, got %d", bodySize, e.RespBytes)
	}
	if e.RespBody == nil || len(*e.RespBody) != maxCapture {
		got := 0
		if e.RespBody != nil {
			got = len(*e.RespBody)
		}
		t.Errorf("expected resp_body length = %d, got %d", maxCapture, got)
	}
}

// Acceptance Test 3: Concurrency (no database locked errors)
func TestAcceptance_Concurrency(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[]}`))
	})

	proxyURL, reader, _, cleanup := setupIntegrationProxy(t, upstream, 1024)
	defer cleanup()

	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	// 100 proxy requests in parallel
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(proxyURL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
			if err != nil {
				errCh <- err
				return
			}
			io.ReadAll(resp.Body)
			resp.Body.Close()
		}()
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reader.Query(database.DefaultQueryFilter())
			if err != nil {
				errCh <- fmt.Errorf("read error: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if strings.Contains(err.Error(), "database is locked") {
			t.Fatalf("got database locked error: %v", err)
		}
	}
}

// Acceptance Test 4: Fail-Open
func TestAcceptance_FailOpen(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	dbPath := filepath.Join(t.TempDir(), "failopen.db")

	writerDB, _ := database.OpenWriter(dbPath)
	readerDB, _ := database.OpenReader(dbPath)
	lw, _ := database.NewLogWriter(writerDB, 1000)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lw.Run(ctx)
	}()

	upstreamSrv := httptest.NewServer(upstream)
	upstreamURL, _ := url.Parse(upstreamSrv.URL)

	rpPlain := proxy.NewPlainReverseProxy(upstreamURL, 5*time.Second)
	rpCapture := proxy.NewReverseProxy(upstreamURL, 5*time.Second, 1024)
	logPaths := map[string]struct{}{"/test": {}}
	handler := proxy.Handler(rpPlain, rpCapture, logPaths, 1024, lw, upstreamURL)
	proxySrv := httptest.NewServer(handler)

	// Make DB read-only to trigger write failures
	writerDB.Close()
	os.Chmod(dbPath, 0444)
	os.Chmod(dbPath+"-wal", 0444)
	os.Chmod(dbPath+"-shm", 0444)

	// Send requests — proxy should still return 200
	for i := 0; i < 5; i++ {
		resp, err := http.Post(proxySrv.URL+"/test", "text/plain", strings.NewReader("test"))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
		if string(body) != "ok" {
			t.Errorf("request %d: expected 'ok', got %q", i, string(body))
		}
	}

	proxySrv.Close()
	cancel()
	wg.Wait()
	lw.Close()
	readerDB.Close()
	upstreamSrv.Close()
}

// Acceptance Test 5: Redaction
func TestAcceptance_Redaction(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Set-Cookie", "session=secret123")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	proxyURL, reader, _, cleanup := setupIntegrationProxy(t, upstream, 1024)
	defer cleanup()

	req, _ := http.NewRequest("POST", proxyURL+"/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer sk-test123456")
	req.Header.Set("Cookie", "session=mysecret")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)

	entries, err := reader.Query(database.DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]

	// Request headers must NOT contain Authorization or Cookie
	if strings.Contains(e.ReqHeadersJSON, "sk-test123456") {
		t.Error("req_headers_json contains Authorization value — should be redacted")
	}
	if strings.Contains(e.ReqHeadersJSON, "mysecret") {
		t.Error("req_headers_json contains Cookie value — should be redacted")
	}
	if strings.Contains(e.ReqHeadersJSON, "Authorization") {
		t.Error("req_headers_json contains Authorization key — should be redacted")
	}

	// Response headers must NOT contain Set-Cookie
	if e.RespHeadersJSON != nil && strings.Contains(*e.RespHeadersJSON, "secret123") {
		t.Error("resp_headers_json contains Set-Cookie value — should be redacted")
	}

	// Content-Type should still be present
	if !strings.Contains(e.ReqHeadersJSON, "Content-Type") {
		t.Error("req_headers_json should contain Content-Type")
	}
}
