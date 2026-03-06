package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"
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
		Reader:    database.NewLogReader(readerDB),
		LogWriter: lw,
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
