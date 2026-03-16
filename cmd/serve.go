package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"
	"memoryelaine/internal/management"
	"memoryelaine/internal/proxy"
	"memoryelaine/internal/recording"

	"github.com/spf13/cobra"
)

func logClose(resource string, closer interface{ Close() error }) {
	if err := closer.Close(); err != nil {
		slog.Error("close failed", "resource", resource, "error", err)
	}
}

func shutdownServer(ctx context.Context, name string, srv *http.Server) {
	if err := srv.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		slog.Error("shutdown failed", "server", name, "error", err)
	}
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy and management servers",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	// 1. Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	// 2. Set up structured logging
	level, err := config.ParseLogLevel(cfg.Logging.Level)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("config loaded",
		"proxy_addr", cfg.Proxy.ListenAddr,
		"management_addr", cfg.Management.ListenAddr,
		"upstream", cfg.Proxy.UpstreamBaseURL,
		"log_level", cfg.Logging.Level,
	)

	// 3. Open databases
	writerDB, err := database.OpenWriter(cfg.Database.Path)
	if err != nil {
		return err
	}
	readerDB, err := database.OpenReader(cfg.Database.Path)
	if err != nil {
		logClose("writer database", writerDB)
		return err
	}

	// 4. Create LogWriter and start background worker
	logWriter, err := database.NewLogWriter(writerDB, 1000)
	if err != nil {
		logClose("writer database", writerDB)
		logClose("reader database", readerDB)
		return err
	}

	writerCtx, writerCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		logWriter.Run(writerCtx)
	}()

	// 5. Parse upstream URL
	upstream, err := url.Parse(cfg.Proxy.UpstreamBaseURL)
	if err != nil {
		writerCancel()
		logClose("writer database", writerDB)
		logClose("reader database", readerDB)
		return err
	}

	// 6. Build reverse proxies
	timeout := time.Duration(cfg.Proxy.TimeoutMinutes) * time.Minute
	rpPlain := proxy.NewPlainReverseProxy(upstream, timeout)
	rpCapture := proxy.NewReverseProxy(upstream, timeout, cfg.Logging.MaxCaptureBytes)
	recordingState := recording.NewState(true)

	// 7. Build log path set
	logPathSet := make(map[string]struct{}, len(cfg.Proxy.LogPaths))
	for _, p := range cfg.Proxy.LogPaths {
		logPathSet[p] = struct{}{}
	}

	// 8. Build proxy handler
	proxyHandler := proxy.Handler(rpPlain, rpCapture, logPathSet, cfg.Logging.MaxCaptureBytes, logWriter, recordingState, upstream)

	// 9. Build management mux
	logReader := database.NewLogReader(readerDB)
	mgmtMux := management.NewMux(management.ServerDeps{
		Reader:         logReader,
		LogWriter:      logWriter,
		RecordingState: recordingState,
		Auth:           cfg.Management.Auth,
	})

	// 10. Start proxy server
	proxyServer := &http.Server{
		Addr:    cfg.Proxy.ListenAddr,
		Handler: proxyHandler,
	}

	mgmtServer := &http.Server{
		Addr:    cfg.Management.ListenAddr,
		Handler: mgmtMux,
	}

	go func() {
		slog.Info("proxy server starting", "addr", cfg.Proxy.ListenAddr)
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server error", "error", err)
		}
	}()

	go func() {
		slog.Info("management server starting", "addr", cfg.Management.ListenAddr)
		if err := mgmtServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("management server error", "error", err)
		}
	}()

	// 11. Block on signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	slog.Info("shutting down")

	// 12. Graceful shutdown
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shutdownServer(shutCtx, "proxy", proxyServer)
	shutdownServer(shutCtx, "management", mgmtServer)
	writerCancel()
	wg.Wait()
	logClose("log writer", logWriter)
	logClose("writer database", writerDB)
	logClose("reader database", readerDB)

	slog.Info("shutdown complete")
	return nil
}
