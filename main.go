package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := FromEnv()
	if err != nil {
		log.Error("configuration error", "err", err)
		os.Exit(1)
	}

	srv := &server{
		cfg:     cfg,
		client:  NewClient(cfg),
		builder: newBuilder(cfg),
		log:     log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", srv.handleIndex)
	mux.HandleFunc("GET /healthz", srv.handleHealth)
	mux.HandleFunc("GET /u/{handle}", srv.handleUser)
	mux.HandleFunc("GET /combined", srv.handleCombined)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.Addr, "nitter", cfg.NitterBase, "version", version)
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

type server struct {
	cfg     Config
	client  *Client
	builder *builder
	log     *slog.Logger
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, `twitter-rss %s

Per-user feed:   /u/{handle}
Combined feed:   /combined?users=handle1,handle2,handle3
Health:          /healthz

Backed by Nitter at %s
`, version, s.cfg.NitterBase)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if !ValidHandle(handle) {
		http.Error(w, "invalid Twitter handle", http.StatusBadRequest)
		return
	}

	f, err := s.client.Fetch(r.Context(), handle)
	if err != nil {
		s.log.Warn("fetch failed", "handle", handle, "err", err)
		http.Error(w, "failed to fetch feed: "+err.Error(), http.StatusBadGateway)
		return
	}

	rss, err := s.builder.Single(f)
	if err != nil {
		s.log.Error("render failed", "handle", handle, "err", err)
		http.Error(w, "failed to render feed", http.StatusInternalServerError)
		return
	}
	writeRSS(w, rss)
}

func (s *server) handleCombined(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("users")
	if raw == "" {
		http.Error(w, "missing ?users=handle1,handle2", http.StatusBadRequest)
		return
	}

	var feedsList []*Feed
	var failed []string
	for _, h := range strings.Split(raw, ",") {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if !ValidHandle(h) {
			http.Error(w, "invalid Twitter handle: "+h, http.StatusBadRequest)
			return
		}
		f, err := s.client.Fetch(r.Context(), h)
		if err != nil {
			s.log.Warn("fetch failed in combined", "handle", h, "err", err)
			failed = append(failed, h)
			continue
		}
		feedsList = append(feedsList, f)
	}

	if len(feedsList) == 0 {
		http.Error(w, "no feeds could be fetched (failed: "+strings.Join(failed, ", ")+")", http.StatusBadGateway)
		return
	}

	rss, err := s.builder.Combined("twitter-rss", feedsList)
	if err != nil {
		s.log.Error("combined render failed", "err", err)
		http.Error(w, "failed to render feed", http.StatusInternalServerError)
		return
	}
	writeRSS(w, rss)
}

func writeRSS(w http.ResponseWriter, rss string) {
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(rss))
}
