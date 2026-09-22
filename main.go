package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/xsaveopt/twitter-rss/internal/config"
	"github.com/xsaveopt/twitter-rss/internal/server"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.FromEnv(version)
	if err != nil {
		log.Error("configuration error", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, version, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.Addr, "nitter", strings.Join(cfg.NitterBases, ","), "version", version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
}

func runHealthcheck() int {
	cfg, err := config.FromEnv(version)
	if err != nil {
		return 1
	}
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return 1
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s%s/health", port, cfg.BasePath))
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
