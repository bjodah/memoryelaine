package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := `
proxy:
  listen_addr: "127.0.0.1:9000"
  upstream_base_url: "https://example.com"
  timeout_minutes: 5
  log_paths:
    - "/v1/chat/completions"
management:
  listen_addr: "127.0.0.1:9080"
  auth:
    username: "user"
    password: "secret"
database:
  path: "/tmp/test.db"
logging:
  max_capture_bytes: 1024
`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Proxy.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("expected listen_addr 127.0.0.1:9000, got %s", cfg.Proxy.ListenAddr)
	}
	if cfg.Proxy.UpstreamBaseURL != "https://example.com" {
		t.Errorf("expected upstream_base_url https://example.com, got %s", cfg.Proxy.UpstreamBaseURL)
	}
	if cfg.Logging.MaxCaptureBytes != 1024 {
		t.Errorf("expected max_capture_bytes 1024, got %d", cfg.Logging.MaxCaptureBytes)
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Load with a non-existent path to exercise defaults
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nonexistent.yaml")

	_, err := Load(cfgPath)
	// Should fail because the file doesn't exist and we specified an explicit path
	if err == nil {
		t.Fatal("expected error for missing explicit config path")
	}
}

func TestLoad_DefaultsNoExplicitPath(t *testing.T) {
	// Change to a dir with no config.yaml to trigger defaults
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Proxy.ListenAddr != "0.0.0.0:8000" {
		t.Errorf("expected default listen_addr, got %s", cfg.Proxy.ListenAddr)
	}
	if cfg.Logging.MaxCaptureBytes != 8388608 {
		t.Errorf("expected default max_capture_bytes, got %d", cfg.Logging.MaxCaptureBytes)
	}
}

func TestValidate_BadURL(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := `
proxy:
  listen_addr: ":8000"
  upstream_base_url: "not-a-url"
  log_paths: ["/test"]
management:
  listen_addr: ":8080"
logging:
  max_capture_bytes: 1024
`
	os.WriteFile(cfgPath, []byte(data), 0644)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for bad URL")
	}
}

func TestValidate_PortCollision(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := `
proxy:
  listen_addr: ":8000"
  upstream_base_url: "https://example.com"
  log_paths: ["/test"]
management:
  listen_addr: ":8000"
logging:
  max_capture_bytes: 1024
`
	os.WriteFile(cfgPath, []byte(data), 0644)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for port collision")
	}
}

func TestValidate_ZeroCaptureBytes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	data := `
proxy:
  listen_addr: ":8000"
  upstream_base_url: "https://example.com"
  log_paths: ["/test"]
management:
  listen_addr: ":8080"
logging:
  max_capture_bytes: 0
`
	os.WriteFile(cfgPath, []byte(data), 0644)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for zero capture bytes")
	}
}
