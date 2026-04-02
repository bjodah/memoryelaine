package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Proxy      ProxyConfig      `mapstructure:"proxy"`
	Management ManagementConfig `mapstructure:"management"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Logging    LoggingConfig    `mapstructure:"logging"`
}

type ProxyConfig struct {
	ListenAddr      string   `mapstructure:"listen_addr"`
	UpstreamBaseURL string   `mapstructure:"upstream_base_url"`
	TimeoutMinutes  int      `mapstructure:"timeout_minutes"`
	LogPaths        []string `mapstructure:"log_paths"`
}

type ManagementConfig struct {
	ListenAddr   string     `mapstructure:"listen_addr"`
	Auth         AuthConfig `mapstructure:"auth"`
	PreviewBytes int        `mapstructure:"preview_bytes"`
}

type AuthConfig struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type LoggingConfig struct {
	MaxCaptureBytes int    `mapstructure:"max_capture_bytes"`
	Level           string `mapstructure:"level"`
}

// Load reads configuration using viper.
// Lookup order: cfgPath flag → ./config.yaml → $HOME/.config/memoryelaine/config.yaml
func Load(cfgPath string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("proxy.listen_addr", "0.0.0.0:13844")
	v.SetDefault("proxy.upstream_base_url", "https://api.openai.com")
	v.SetDefault("proxy.timeout_minutes", 23)
	v.SetDefault("proxy.log_paths", []string{"/v1/chat/completions", "/v1/completions"})
	v.SetDefault("management.listen_addr", "0.0.0.0:13845")
	v.SetDefault("management.auth.username", "admin")
	v.SetDefault("management.auth.password", "changeme")
	v.SetDefault("management.preview_bytes", 65536)
	v.SetDefault("database.path", "./memoryelaine.db")
	v.SetDefault("logging.max_capture_bytes", 8388608)
	v.SetDefault("logging.level", "info")

	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "memoryelaine"))
		}
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		slog.Warn("no config file found, using defaults")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.expandPaths(); err != nil {
		return nil, fmt.Errorf("expanding config paths: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) expandPaths() error {
	path, err := expandUserPath(c.Database.Path)
	if err != nil {
		return err
	}
	c.Database.Path = path
	return nil
}

func expandUserPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}

	return filepath.Join(home, path[2:]), nil
}

func (c *Config) validate() error {
	u, err := url.Parse(c.Proxy.UpstreamBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("upstream_base_url must be a valid http/https URL, got %q", c.Proxy.UpstreamBaseURL)
	}

	if c.Proxy.ListenAddr == c.Management.ListenAddr {
		return fmt.Errorf("proxy and management listen addresses must not collide: both are %q", c.Proxy.ListenAddr)
	}

	if c.Logging.MaxCaptureBytes <= 0 {
		return fmt.Errorf("max_capture_bytes must be > 0, got %d", c.Logging.MaxCaptureBytes)
	}

	if _, err := ParseLogLevel(c.Logging.Level); err != nil {
		return err
	}

	if len(c.Proxy.LogPaths) == 0 {
		return fmt.Errorf("log_paths must not be empty")
	}

	if c.Management.PreviewBytes <= 0 {
		return fmt.Errorf("management.preview_bytes must be > 0, got %d", c.Management.PreviewBytes)
	}

	if c.Management.Auth.Username == "admin" && c.Management.Auth.Password == "changeme" {
		slog.Warn("management auth is using default credentials — change in production")
	}

	return nil
}

func ParseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging.level must be one of debug, info, warn, error, got %q", level)
	}
}
