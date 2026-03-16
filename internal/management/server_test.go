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
			AssembledBody      string `json:"assembled_body"`
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
	if result.StreamView.AssembledBody != "Hello" {
		t.Errorf("expected assembled_body %q, got %q", "Hello", result.StreamView.AssembledBody)
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
