package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/xsaveopt/twitter-rss/internal/config"
	"github.com/xsaveopt/twitter-rss/internal/feed"
	"github.com/xsaveopt/twitter-rss/internal/nitter"
)

type Server struct {
	cfg     config.Config
	version string
	client  *nitter.Client
	builder *feed.Builder
	log     *slog.Logger
}

func New(cfg config.Config, version string, log *slog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		version: version,
		client:  nitter.NewClient(cfg),
		builder: feed.NewBuilder(cfg),
		log:     log,
	}
}

func (s *Server) Handler() http.Handler {
	full := s.routes(true)
	if s.cfg.BasePath == "" {
		return full
	}

	outer := http.NewServeMux()
	outer.Handle("/", s.routes(false))
	outer.Handle(s.cfg.BasePath+"/", http.StripPrefix(s.cfg.BasePath, full))
	return outer
}

func (s *Server) routes(includeHealth bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	if includeHealth {
		mux.HandleFunc("GET /health", s.handleHealth)
	}
	mux.HandleFunc("GET /u/{handle}", s.handleUser)
	mux.HandleFunc("GET /combined", s.handleCombined)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	base := s.cfg.BasePath
	_, _ = fmt.Fprintf(w, `twitter-rss %s

Per-user feed:   %s/u/{handle}
Combined feed:   %s/combined?users=handle1,handle2,handle3
Health:          %s/health

Backed by Nitter at %s
`, s.version, base, base, base, strings.Join(s.cfg.NitterBases, ", "))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.client.Healthy() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("degraded"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("up"))
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if !nitter.ValidHandle(handle) {
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

func (s *Server) handleCombined(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("users")
	if raw == "" {
		http.Error(w, "missing ?users=handle1,handle2", http.StatusBadRequest)
		return
	}

	var feedsList []*nitter.Feed
	var failed []string
	for _, h := range strings.Split(raw, ",") {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if !nitter.ValidHandle(h) {
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
