package server

import (
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xsaveopt/twitter-rss/internal/config"
)

const fixtureRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Gopher / @gopher</title>
    <link>https://nitter.example.com/gopher</link>
    <description>Twitter feed for: @gopher</description>
    <item>
      <title>A tweet</title>
      <description>&lt;p&gt;A tweet&lt;/p&gt;</description>
      <pubDate>Tue, 16 Sep 2025 12:30:00 GMT</pubDate>
      <guid>https://nitter.example.com/gopher/status/222#m</guid>
      <link>https://nitter.example.com/gopher/status/222#m</link>
    </item>
  </channel>
</rss>`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func nitterStub(t *testing.T, handles map[string]bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/rss")
		if !handles[handle] {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(strings.ReplaceAll(fixtureRSS, "gopher", handle)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newServer(t *testing.T, basePath string, bases ...string) *Server {
	t.Helper()
	return New(config.Config{
		Addr:         ":0",
		BasePath:     basePath,
		NitterBases:  bases,
		CacheTTL:     time.Minute,
		RewriteLinks: true,
		UserAgent:    "twitter-rss/test",
		HTTPTimeout:  5 * time.Second,
	}, "1.2.3", discardLogger())
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestHandleIndex(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL, "https://second.example.com").Handler()

	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"twitter-rss 1.2.3",
		"/u/{handle}",
		"/combined?users=handle1,handle2,handle3",
		"/health",
		up.URL + ", https://second.example.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index body does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleIndexAdvertisesBasePath(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "/twitter", up.URL).Handler()

	body := get(t, h, "/twitter/").Body.String()
	for _, want := range []string{"/twitter/u/{handle}", "/twitter/combined?users=", "/twitter/health"} {
		if !strings.Contains(body, want) {
			t.Errorf("index body does not contain %q:\n%s", want, body)
		}
	}
}

func TestHandleIndexOnlyMatchesExactRoot(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	if rec := get(t, h, "/nonsense"); rec.Code != http.StatusNotFound {
		t.Errorf("status for /nonsense = %d, want 404", rec.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "up" {
		t.Errorf("body = %q, want up", rec.Body.String())
	}
}

func TestHandleHealthDegradedWhenAllInstancesAreDown(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(up.Close)
	s := newServer(t, "", up.URL)
	h := s.Handler()

	if rec := get(t, h, "/u/gopher"); rec.Code != http.StatusBadGateway {
		t.Fatalf("priming failure: status = %d, want 502", rec.Code)
	}

	rec := get(t, h, "/health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Body.String() != "degraded" {
		t.Errorf("body = %q, want degraded", rec.Body.String())
	}
}

func TestHandleUser(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/u/GoPher")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var parsed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
				Link  string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("response is not well-formed XML: %v\n%s", err, rec.Body.String())
	}
	if parsed.Channel.Title != "@gopher on Twitter" {
		t.Errorf("channel title = %q", parsed.Channel.Title)
	}
	if len(parsed.Channel.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(parsed.Channel.Items))
	}
	if parsed.Channel.Items[0].Link != "https://x.com/gopher/status/222" {
		t.Errorf("item link = %q, want the rewritten x.com link", parsed.Channel.Items[0].Link)
	}
}

func TestHandleUserRejectsInvalidHandle(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	for _, handle := range []string{"go-pher", "abcdefghijklmnop", "go.pher", "%20"} {
		rec := get(t, h, "/u/"+handle)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status for /u/%s = %d, want 400", handle, rec.Code)
		}
	}
}

func TestHandleUserUpstreamFailure(t *testing.T) {
	up := nitterStub(t, map[string]bool{})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/u/gopher")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch feed") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestHandleCombined(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true, "rustlang": true})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/combined?users=gopher,+%20,rustlang")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var parsed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("response is not well-formed XML: %v\n%s", err, rec.Body.String())
	}
	if parsed.Channel.Title != "Tracked Twitter accounts" {
		t.Errorf("channel title = %q", parsed.Channel.Title)
	}
	if len(parsed.Channel.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(parsed.Channel.Items))
	}

	body := rec.Body.String()
	for _, want := range []string{"@gopher: A tweet", "@rustlang: A tweet"} {
		if !strings.Contains(body, want) {
			t.Errorf("combined feed does not contain %q", want)
		}
	}
}

func TestHandleCombinedKeepsWorkingFeedsWhenOneFails(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/combined?users=gopher,missing")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "@gopher: A tweet") {
		t.Error("combined feed dropped the healthy handle")
	}
	if strings.Contains(rec.Body.String(), "@missing") {
		t.Error("combined feed contains the failing handle")
	}
}

func TestHandleCombinedErrors(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	tests := []struct {
		name   string
		target string
		status int
		body   string
	}{
		{"missing users", "/combined", http.StatusBadRequest, "missing ?users="},
		{"empty users", "/combined?users=", http.StatusBadRequest, "missing ?users="},
		{"invalid handle", "/combined?users=gopher,go-pher", http.StatusBadRequest, "invalid Twitter handle: go-pher"},
		{"all fail", "/combined?users=missing,alsomissing", http.StatusBadGateway, "no feeds could be fetched"},
		{"only separators", "/combined?users=,,", http.StatusBadGateway, "no feeds could be fetched"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, h, tt.target)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("body = %q, want it to mention %q", rec.Body.String(), tt.body)
			}
		})
	}
}

func TestHandleCombinedFailingHandleNamedInError(t *testing.T) {
	up := nitterStub(t, map[string]bool{})
	h := newServer(t, "", up.URL).Handler()

	rec := get(t, h, "/combined?users=one,two")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "failed: one, two") {
		t.Errorf("body = %q, want it to name both failing handles", rec.Body.String())
	}
}

func TestHandleUserCachesAcrossRequests(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(fixtureRSS))
	}))
	t.Cleanup(srv.Close)

	h := newServer(t, "", srv.URL).Handler()
	for range 3 {
		if rec := get(t, h, "/u/gopher"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hits = %d, want 1", hits.Load())
	}
}

func TestHandlerRejectsNonGET(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "", up.URL).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/u/gopher", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandlerBasePathMounting(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		prefix string
	}{
		{"empty", "", ""},
		{"leading slash", "/twitter", "/twitter"},
		{"trailing slash", "twitter/", "/twitter"},
		{"both slashes", "/twitter/", "/twitter"},
		{"nested", "/feeds/twitter/", "/feeds/twitter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := nitterStub(t, map[string]bool{"gopher": true})
			t.Setenv("TWITTER_RSS_NITTER", up.URL)
			t.Setenv("TWITTER_RSS_BASE_PATH", tt.env)

			cfg, err := config.FromEnv("1.2.3")
			if err != nil {
				t.Fatalf("FromEnv: %v", err)
			}
			if cfg.BasePath != tt.prefix {
				t.Fatalf("BasePath = %q, want %q", cfg.BasePath, tt.prefix)
			}
			h := New(cfg, "1.2.3", discardLogger()).Handler()

			if rec := get(t, h, tt.prefix+"/"); rec.Code != http.StatusOK {
				t.Errorf("index at %q = %d, want 200", tt.prefix+"/", rec.Code)
			}
			if rec := get(t, h, tt.prefix+"/health"); rec.Code != http.StatusOK || rec.Body.String() != "up" {
				t.Errorf("health at %q = %d %q", tt.prefix+"/health", rec.Code, rec.Body.String())
			}
			if rec := get(t, h, tt.prefix+"/u/gopher"); rec.Code != http.StatusOK {
				t.Errorf("user feed at %q = %d, want 200", tt.prefix+"/u/gopher", rec.Code)
			}
			if rec := get(t, h, tt.prefix+"/combined?users=gopher"); rec.Code != http.StatusOK {
				t.Errorf("combined at %q = %d, want 200", tt.prefix+"/combined", rec.Code)
			}

			if tt.prefix == "" {
				return
			}

			if rec := get(t, h, "/health"); rec.Code != http.StatusNotFound {
				t.Errorf("root health with base path %q = %d, want 404", tt.prefix, rec.Code)
			}

			rec := get(t, h, tt.prefix)
			if rec.Code < 300 || rec.Code > 399 {
				t.Errorf("status for the bare base path = %d, want a redirect", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != tt.prefix+"/" {
				t.Errorf("Location = %q, want %q", loc, tt.prefix+"/")
			}
		})
	}
}

func TestHandlerServesRootAlongsideBasePath(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "/twitter", up.URL).Handler()

	for _, target := range []string{"/", "/u/gopher", "/combined?users=gopher"} {
		if rec := get(t, h, target); rec.Code != http.StatusOK {
			t.Errorf("status for %q = %d, want 200; the root mount must keep working", target, rec.Code)
		}
	}

	if rec := get(t, h, "/health"); rec.Code != http.StatusNotFound {
		t.Errorf("root health with a base path set = %d, want 404", rec.Code)
	}
	if rec := get(t, h, "/twitter/health"); rec.Code != http.StatusOK || rec.Body.String() != "up" {
		t.Errorf("health under the base path = %d %q, want 200 up", rec.Code, rec.Body.String())
	}
}

func TestHandlerUnknownPathsUnderBasePath(t *testing.T) {
	up := nitterStub(t, map[string]bool{"gopher": true})
	h := newServer(t, "/twitter", up.URL).Handler()

	for _, target := range []string{"/twitter/nonsense", "/twitter/u/", "/other/health"} {
		if rec := get(t, h, target); rec.Code != http.StatusNotFound {
			t.Errorf("status for %q = %d, want 404", target, rec.Code)
		}
	}
}

func TestWriteRSS(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRSS(rec, "<rss/>")

	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Body.String() != "<rss/>" {
		t.Errorf("body = %q", rec.Body.String())
	}
}
