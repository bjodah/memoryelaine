package management

import (
	"encoding/json"
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
	t.Cleanup(func() { writerDB.Close(); readerDB.Close() })

	lw, err := database.NewLogWriter(writerDB, 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lw.Close() })

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
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
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

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
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
	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
