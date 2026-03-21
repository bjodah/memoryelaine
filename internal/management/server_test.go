package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"
	"memoryelaine/internal/recording"
)

func setupTestDeps(t *testing.T) ServerDeps {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	writerDB, err := database.OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	readerDB, err := database.OpenReader(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		t.Helper()
		if err := writerDB.Close(); err != nil {
			t.Errorf("closing writer db: %v", err)
		}
		if err := readerDB.Close(); err != nil {
			t.Errorf("closing reader db: %v", err)
		}
	})

	lw, err := database.NewLogWriter(writerDB, 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		t.Helper()
		if err := lw.Close(); err != nil {
			t.Errorf("closing log writer: %v", err)
		}
	})

	return ServerDeps{
		Reader:         database.NewLogReader(readerDB),
		LogWriter:      lw,
		RecordingState: recording.NewState(true),
		Auth: config.AuthConfig{
			Username: "admin",
			Password: "test123",
		},
		PreviewBytes: 65536,
	}
}

func TestBasicAuth_ValidCreds(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBasicAuth_InvalidCreds(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs", nil)
	req.SetBasicAuth("admin", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestBasicAuth_NoCreds(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHealth_NoAuth(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["db_connected"] != true {
		t.Errorf("expected db_connected true, got %v", body["db_connected"])
	}
	if body["recording"] != true {
		t.Errorf("expected recording true, got %v", body["recording"])
	}
}

func TestAPILogs_Empty(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["total"].(float64) != 0 {
		t.Errorf("expected 0 total, got %v", body["total"])
	}
}

func TestAPILogByID_NotFound(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs/999", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && err != io.EOF {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPILogByID_WrappedResponseShape(t *testing.T) {
	deps := setupTestDeps(t)

	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: [DONE]\n\n"
	entry := database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{"model":"gpt-4"}`,
		ReqBytes:        17,
		RespHeadersJSON: ptr(`{"Content-Type":["text/event-stream"]}`),
		RespBody:        &body,
		RespBytes:       int64(len(body)),
	}
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs/1", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Entry      json.RawMessage `json:"entry"`
		StreamView struct {
			AssembledAvailable bool   `json:"assembled_available"`
			Reason             string `json:"reason"`
		} `json:"stream_view"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Entry == nil {
		t.Fatal("expected entry in response")
	}
	if !result.StreamView.AssembledAvailable {
		t.Errorf("expected assembled_available=true, got false (reason=%s)", result.StreamView.Reason)
	}
	if result.StreamView.Reason != "supported" {
		t.Errorf("expected reason %q, got %q", "supported", result.StreamView.Reason)
	}
}

func TestAPILogByID_NonStreamedResponse(t *testing.T) {
	deps := setupTestDeps(t)

	respBody := `{"id":"chatcmpl-1","choices":[{"message":{"content":"hello"}}]}`
	entry := database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{}`,
		ReqBytes:        2,
		RespHeadersJSON: ptr(`{"Content-Type":["application/json"]}`),
		RespBody:        &respBody,
		RespBytes:       int64(len(respBody)),
	}
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs/1", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var result struct {
		StreamView struct {
			AssembledAvailable bool   `json:"assembled_available"`
			Reason             string `json:"reason"`
		} `json:"stream_view"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.StreamView.AssembledAvailable {
		t.Error("expected assembled_available=false for non-SSE response")
	}
	if result.StreamView.Reason != "not_sse" {
		t.Errorf("expected reason %q, got %q", "not_sse", result.StreamView.Reason)
	}
}

func TestAPILogByID_TruncatedResponse(t *testing.T) {
	deps := setupTestDeps(t)

	body := "data: partial...\n\n"
	entry := database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       &body,
		RespTruncated:  true,
		RespBytes:      100000,
	}
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs/1", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var result struct {
		StreamView struct {
			AssembledAvailable bool   `json:"assembled_available"`
			Reason             string `json:"reason"`
		} `json:"stream_view"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.StreamView.AssembledAvailable {
		t.Error("expected assembled_available=false for truncated response")
	}
	if result.StreamView.Reason != "truncated" {
		t.Errorf("expected reason %q, got %q", "truncated", result.StreamView.Reason)
	}
}

func TestAPILogByID_UnsupportedPath(t *testing.T) {
	deps := setupTestDeps(t)

	body := "data: {}\n\n"
	entry := database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/embeddings",
		UpstreamURL:    "https://example.com/v1/embeddings",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       &body,
		RespBytes:      int64(len(body)),
	}
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/logs/1", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var result struct {
		StreamView struct {
			AssembledAvailable bool   `json:"assembled_available"`
			Reason             string `json:"reason"`
		} `json:"stream_view"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.StreamView.AssembledAvailable {
		t.Error("expected assembled_available=false for unsupported path")
	}
	if result.StreamView.Reason != "unsupported_path" {
		t.Errorf("expected reason %q, got %q", "unsupported_path", result.StreamView.Reason)
	}
}

func TestLastRequestAndResponse_Empty(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/last-request", "/last-response"} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.SetBasicAuth("admin", "test123")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("closing response body: %v", err)
				}
			}()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s expected 404, got %d", path, resp.StatusCode)
			}
		}()
	}
}

func TestLastRequestAndResponse_ReturnLatestBodies(t *testing.T) {
	deps := setupTestDeps(t)

	entry := database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{"prompt":"hello"}`,
		ReqBytes:        18,
		RespHeadersJSON: ptr("{}"),
		RespBody:        ptr(`{"answer":"world"}`),
		RespBytes:       18,
	}
	deps.LogWriter.SetLastRequest(entry.ReqBody)
	deps.LogWriter.SetLastResponse(*entry.RespBody)
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	checkBody := func(path string, want string) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.SetBasicAuth("admin", "test123")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Fatalf("%s expected %q, got %q", path, want, string(body))
		}
	}

	checkBody("/last-request", entry.ReqBody)
	checkBody("/last-response", *entry.RespBody)
}

func TestAPIRecording_GetAndPut(t *testing.T) {
	deps := setupTestDeps(t)
	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/recording", nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var getBody struct {
		Recording bool `json:"recording"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&getBody); err != nil {
		t.Fatal(err)
	}
	if !getBody.Recording {
		t.Fatal("expected recording=true")
	}

	putReq, _ := http.NewRequest("PUT", srv.URL+"/api/recording", strings.NewReader(`{"recording":false}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.SetBasicAuth("admin", "test123")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := putResp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var putBody struct {
		Recording bool `json:"recording"`
	}
	if err := json.NewDecoder(putResp.Body).Decode(&putBody); err != nil {
		t.Fatal(err)
	}
	if putBody.Recording {
		t.Fatal("expected recording=false after PUT")
	}
	if deps.RecordingState.Enabled() {
		t.Fatal("expected shared recording state to be false")
	}

	healthResp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := healthResp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	}()

	var healthBody map[string]interface{}
	if err := json.NewDecoder(healthResp.Body).Decode(&healthBody); err != nil {
		t.Fatal(err)
	}
	if healthBody["recording"] != false {
		t.Fatalf("expected health recording=false, got %v", healthBody["recording"])
	}
}

func TestLastRequestAndResponse_StaleLabel(t *testing.T) {
	deps := setupTestDeps(t)
	deps.LogWriter.SetLastRequest(`{"prompt":"old"}`)
	deps.LogWriter.SetLastResponse(`{"answer":"old"}`)
	deps.LogWriter.MarkLastBodiesStale()

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	checkBody := func(path string, want string) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.SetBasicAuth("admin", "test123")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("closing response body: %v", err)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Fatalf("%s expected %q, got %q", path, want, string(body))
		}
	}

	checkBody("/last-request", "[STALE]\n\n"+`{"prompt":"old"}`)
	checkBody("/last-response", "[STALE]\n\n"+`{"answer":"old"}`)
}

func TestLastEndpoints_FallbackToDatabase(t *testing.T) {
	deps := setupTestDeps(t)
	entry := database.LogEntry{
		TsStart:         2,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{"prompt":"db"}`,
		ReqBytes:        15,
		RespHeadersJSON: ptr("{}"),
		RespBody:        ptr(`{"answer":"stored"}`),
		RespBytes:       19,
	}
	if !deps.LogWriter.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)

	mux := NewMux(deps)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for path, want := range map[string]string{
		"/last-request":  entry.ReqBody,
		"/last-response": *entry.RespBody,
	} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.SetBasicAuth("admin", "test123")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("closing response body: %v", err)
				}
			}()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != want {
				t.Fatalf("%s expected %q, got %q", path, want, string(body))
			}
		}()
	}
}

func ptr(s string) *string { return &s }

func intPtr(n int) *int { return &n }

// insertAndFlush enqueues a LogEntry, flushes the writer, and returns deps
// ready to use with NewMux.
func insertAndFlush(t *testing.T, deps ServerDeps, entries ...database.LogEntry) {
	t.Helper()
	for _, e := range entries {
		if !deps.LogWriter.Enqueue(e) {
			t.Fatal("enqueue failed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps.LogWriter.Run(ctx)
}

// doAuthGet performs an authenticated GET request and returns the response.
func doAuthGet(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.SetBasicAuth("admin", "test123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// ---------- Body endpoint tests ----------

func TestBodyEndpoint_RawRequest(t *testing.T) {
	deps := setupTestDeps(t)
	reqBody := `{"model":"gpt-4","prompt":"hello"}`
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        reqBody,
		ReqBytes:       int64(len(reqBody)),
		RespBody:       ptr(`{"ok":true}`),
		RespBytes:      11,
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=req&mode=raw")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Available {
		t.Fatal("expected available=true")
	}
	if body.Part != "req" {
		t.Errorf("expected part=req, got %s", body.Part)
	}
	if body.Mode != "raw" {
		t.Errorf("expected mode=raw, got %s", body.Mode)
	}
	if body.Content != reqBody {
		t.Errorf("expected content=%q, got %q", reqBody, body.Content)
	}
	if body.IncludedBytes != len(reqBody) {
		t.Errorf("expected included_bytes=%d, got %d", len(reqBody), body.IncludedBytes)
	}
}

func TestBodyEndpoint_RawResponse(t *testing.T) {
	deps := setupTestDeps(t)
	respBodyStr := `{"id":"chatcmpl-1","choices":[{"message":{"content":"world"}}]}`
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{}`,
		ReqBytes:        2,
		RespHeadersJSON: ptr(`{"Content-Type":["application/json"]}`),
		RespBody:        ptr(respBodyStr),
		RespBytes:       int64(len(respBodyStr)),
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=resp&mode=raw")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Available {
		t.Fatal("expected available=true")
	}
	if body.Part != "resp" {
		t.Errorf("expected part=resp, got %s", body.Part)
	}
	if body.Content != respBodyStr {
		t.Errorf("expected content=%q, got %q", respBodyStr, body.Content)
	}
}

func TestBodyEndpoint_AssembledResponse(t *testing.T) {
	deps := setupTestDeps(t)
	sseBody := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n"
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		ReqHeadersJSON:  "{}",
		ReqBody:         `{"model":"gpt-4"}`,
		ReqBytes:        17,
		RespHeadersJSON: ptr(`{"Content-Type":["text/event-stream"]}`),
		RespBody:        ptr(sseBody),
		RespBytes:       int64(len(sseBody)),
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=resp&mode=assembled")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Available {
		t.Fatalf("expected available=true, reason=%s", body.Reason)
	}
	if body.Mode != "assembled" {
		t.Errorf("expected mode=assembled, got %s", body.Mode)
	}
	if !strings.Contains(body.Content, "hello") || !strings.Contains(body.Content, "world") {
		t.Errorf("expected assembled content to contain 'hello' and 'world', got %q", body.Content)
	}
}

func TestBodyEndpoint_AssembledRequestInvalid(t *testing.T) {
	deps := setupTestDeps(t)
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(`{}`),
		RespBytes:      2,
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=req&mode=assembled")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatal(err)
	}
	if apiErr.Error != "invalid_combination" {
		t.Errorf("expected error=invalid_combination, got %s", apiErr.Error)
	}
}

func TestBodyEndpoint_Preview(t *testing.T) {
	deps := setupTestDeps(t)
	deps.PreviewBytes = 100 // small preview limit

	longBody := strings.Repeat("A", 200) // 200 bytes, well over preview limit
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/completions",
		UpstreamURL:    "https://example.com/v1/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(longBody),
		RespBytes:      int64(len(longBody)),
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=resp&mode=raw")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Truncated {
		t.Error("expected truncated=true for preview request")
	}
	if body.IncludedBytes != 100 {
		t.Errorf("expected included_bytes=100, got %d", body.IncludedBytes)
	}
	if len(body.Content) != 100 {
		t.Errorf("expected content length 100, got %d", len(body.Content))
	}
}

func TestBodyEndpoint_Full(t *testing.T) {
	deps := setupTestDeps(t)
	deps.PreviewBytes = 100 // small preview limit

	longBody := strings.Repeat("B", 200)
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/completions",
		UpstreamURL:    "https://example.com/v1/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(longBody),
		RespBytes:      int64(len(longBody)),
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=resp&mode=raw&full=true")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Truncated {
		t.Error("expected truncated=false when full=true")
	}
	if !body.Full {
		t.Error("expected full=true in response")
	}
	if body.Content != longBody {
		t.Errorf("expected full content (200 bytes), got %d bytes", len(body.Content))
	}
	if body.IncludedBytes != 200 {
		t.Errorf("expected included_bytes=200, got %d", body.IncludedBytes)
	}
}

func TestBodyEndpoint_NotFound(t *testing.T) {
	deps := setupTestDeps(t)
	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/999/body?part=resp&mode=raw")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestBodyEndpoint_EmptyBody(t *testing.T) {
	deps := setupTestDeps(t)
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(""),
		RespBytes:      0,
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=resp&mode=raw")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Available {
		t.Error("expected available=false for empty response body")
	}
	if body.Reason == "" {
		t.Error("expected a reason when body is not available")
	}
}

func TestBodyEndpoint_DefaultParams(t *testing.T) {
	deps := setupTestDeps(t)
	respBodyStr := `{"result":"ok"}`
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(respBodyStr),
		RespBytes:      int64(len(respBodyStr)),
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	// No query params — should default to part=resp, mode=raw
	resp := doAuthGet(t, srv, "/api/logs/1/body")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body BodyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Part != "resp" {
		t.Errorf("expected default part=resp, got %s", body.Part)
	}
	if body.Mode != "raw" {
		t.Errorf("expected default mode=raw, got %s", body.Mode)
	}
	if body.Content != respBodyStr {
		t.Errorf("expected content=%q, got %q", respBodyStr, body.Content)
	}
}

func TestBodyEndpoint_InvalidPart(t *testing.T) {
	deps := setupTestDeps(t)
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:        1,
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://example.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{}`,
		ReqBytes:       2,
		RespBody:       ptr(`{}`),
		RespBytes:      2,
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs/1/body?part=invalid")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatal(err)
	}
	if apiErr.Error != "invalid_part" {
		t.Errorf("expected error=invalid_part, got %s", apiErr.Error)
	}
}

// ---------- Query DSL wiring tests ----------

func TestAPILogs_QueryParam(t *testing.T) {
	deps := setupTestDeps(t)
	status200 := 200
	status404 := 404
	insertAndFlush(t, deps,
		database.LogEntry{
			TsStart:        1,
			ClientIP:       "127.0.0.1",
			RequestMethod:  "GET",
			RequestPath:    "/v1/models",
			UpstreamURL:    "https://example.com/v1/models",
			ReqHeadersJSON: "{}",
			ReqBody:        "",
			ReqBytes:       0,
			StatusCode:     &status200,
			RespBody:       ptr(`{"data":[]}`),
			RespBytes:      11,
		},
		database.LogEntry{
			TsStart:        2,
			ClientIP:       "127.0.0.1",
			RequestMethod:  "GET",
			RequestPath:    "/v1/missing",
			UpstreamURL:    "https://example.com/v1/missing",
			ReqHeadersJSON: "{}",
			ReqBody:        "",
			ReqBytes:       0,
			StatusCode:     &status404,
			RespBody:       ptr(`{"error":"not found"}`),
			RespBytes:      21,
		},
	)

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs?query=status:200")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result LogListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 {
		t.Fatalf("expected 1 result for status:200, got %d", result.Total)
	}
	if result.Data[0].RequestPath != "/v1/models" {
		t.Errorf("expected path /v1/models, got %s", result.Data[0].RequestPath)
	}
}

func TestAPILogs_QueryParamError(t *testing.T) {
	deps := setupTestDeps(t)
	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs?query=status:abc")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatal(err)
	}
	if apiErr.Error != "query_parse_error" {
		t.Errorf("expected error=query_parse_error, got %s", apiErr.Error)
	}
}

// ---------- Summary list shape tests ----------

func TestAPILogs_SummaryShape(t *testing.T) {
	deps := setupTestDeps(t)
	status200 := 200
	insertAndFlush(t, deps, database.LogEntry{
		TsStart:         1,
		ClientIP:        "127.0.0.1",
		RequestMethod:   "POST",
		RequestPath:     "/v1/chat/completions",
		UpstreamURL:     "https://example.com/v1/chat/completions",
		StatusCode:      &status200,
		ReqHeadersJSON:  `{"Authorization":["Bearer sk-test"]}`,
		RespHeadersJSON: ptr(`{"Content-Type":["application/json"]}`),
		ReqBody:         `{"model":"gpt-4"}`,
		ReqBytes:        17,
		RespBody:        ptr(`{"choices":[]}`),
		RespBytes:       14,
	})

	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	resp := doAuthGet(t, srv, "/api/logs")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Decode as raw JSON to inspect the actual fields present.
	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(envelope.Data))
	}

	// Parse the summary entry as a generic map to check field presence.
	var summaryMap map[string]interface{}
	if err := json.Unmarshal(envelope.Data[0], &summaryMap); err != nil {
		t.Fatal(err)
	}

	// Must have has_request_body and has_response_body fields.
	if _, ok := summaryMap["has_request_body"]; !ok {
		t.Error("summary is missing has_request_body field")
	}
	if _, ok := summaryMap["has_response_body"]; !ok {
		t.Error("summary is missing has_response_body field")
	}

	// Verify boolean values are correct.
	if summaryMap["has_request_body"] != true {
		t.Errorf("expected has_request_body=true, got %v", summaryMap["has_request_body"])
	}
	if summaryMap["has_response_body"] != true {
		t.Errorf("expected has_response_body=true, got %v", summaryMap["has_response_body"])
	}

	// Must NOT contain body content or header fields in the summary.
	for _, forbidden := range []string{"req_body", "resp_body", "req_headers", "resp_headers", "req_headers_json", "resp_headers_json", "upstream_url"} {
		if _, ok := summaryMap[forbidden]; ok {
			t.Errorf("summary should NOT contain %q field, but it does", forbidden)
		}
	}

	// Must contain expected summary fields.
	for _, required := range []string{"id", "ts_start", "request_method", "request_path", "status_code", "req_bytes", "resp_bytes"} {
		if _, ok := summaryMap[required]; !ok {
			t.Errorf("summary is missing expected field %q", required)
		}
	}
}
