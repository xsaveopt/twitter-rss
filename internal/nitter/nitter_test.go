package nitter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xsaveopt/twitter-rss/internal/config"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2025, 9, 20, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

func newTestClient(t *testing.T, clock *fakeClock, bases ...string) *Client {
	t.Helper()
	c := NewClient(config.Config{
		NitterBases: bases,
		CacheTTL:    5 * time.Minute,
		UserAgent:   "twitter-rss/test",
		HTTPTimeout: 5 * time.Second,
	})
	c.now = clock.Now
	return c
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/gopher.xml")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return b
}

func rssServer(t *testing.T, body []byte, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if r.URL.Path != "/gopher/rss" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func statusServer(t *testing.T, status int, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		http.Error(w, http.StatusText(status), status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestValidHandle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple", "gopher", true},
		{"underscore", "_go_pher_", true},
		{"digits", "user1234", true},
		{"single char", "a", true},
		{"fifteen chars", "abcdefghijklmno", true},
		{"uppercase", "GoPher", true},
		{"empty", "", false},
		{"sixteen chars", "abcdefghijklmnop", false},
		{"at sign", "@gopher", false},
		{"dash", "go-pher", false},
		{"dot", "go.pher", false},
		{"slash", "go/pher", false},
		{"space", "go pher", false},
		{"path traversal", "../etc", false},
		{"unicode", "göpher", false},
		{"newline suffix", "gopher\n", false},
		{"embedded newline", "gopher\nevil", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidHandle(tt.input); got != tt.want {
				t.Errorf("ValidHandle(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFetchParsesFixture(t *testing.T) {
	clock := newFakeClock()
	srv := rssServer(t, fixture(t), nil)
	c := newTestClient(t, clock, srv.URL)

	f, err := c.Fetch(context.Background(), "GoPher")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if f.Handle != "gopher" {
		t.Errorf("Handle = %q, want %q", f.Handle, "gopher")
	}
	if f.Title != "Gopher / @gopher" {
		t.Errorf("Title = %q", f.Title)
	}
	if f.Link != "https://nitter.example.com/gopher" {
		t.Errorf("Link = %q", f.Link)
	}
	if !f.Fetched.Equal(clock.Now()) {
		t.Errorf("Fetched = %v, want %v", f.Fetched, clock.Now())
	}
	if len(f.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(f.Items))
	}
	if f.Items[0].Title != "Second post with <b>markup</b>" {
		t.Errorf("Items[0].Title = %q", f.Items[0].Title)
	}
	if f.Items[0].PublishedParsed == nil {
		t.Error("Items[0].PublishedParsed is nil")
	}

	wantOrigins := []string{srv.URL, "https://nitter.example.com"}
	if len(f.Origins) != len(wantOrigins) {
		t.Fatalf("Origins = %v, want %v", f.Origins, wantOrigins)
	}
	for i, want := range wantOrigins {
		if f.Origins[i] != want {
			t.Errorf("Origins[%d] = %q, want %q", i, f.Origins[i], want)
		}
	}
}

func TestFetchOriginsSkipDuplicateBase(t *testing.T) {
	clock := newFakeClock()
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	body := `<?xml version="1.0"?><rss version="2.0"><channel>` +
		`<title>Gopher</title><link>` + base + `/gopher</link></channel></rss>`
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	})
	srv.Start()
	t.Cleanup(srv.Close)

	c := newTestClient(t, clock, srv.URL)
	f, err := c.Fetch(context.Background(), "gopher")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(f.Origins) != 1 || f.Origins[0] != srv.URL {
		t.Errorf("Origins = %v, want [%s]", f.Origins, srv.URL)
	}
}

func TestFetchCachesWithinTTL(t *testing.T) {
	clock := newFakeClock()
	var hits atomic.Int64
	srv := rssServer(t, fixture(t), &hits)
	c := newTestClient(t, clock, srv.URL)

	first, err := c.Fetch(context.Background(), "gopher")
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	clock.Advance(4 * time.Minute)
	second, err := c.Fetch(context.Background(), "gopher")
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if second != first {
		t.Error("second Fetch did not return the cached feed")
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hits = %d, want 1", hits.Load())
	}

	clock.Advance(time.Minute)
	third, err := c.Fetch(context.Background(), "gopher")
	if err != nil {
		t.Fatalf("third Fetch: %v", err)
	}
	if third == first {
		t.Error("third Fetch returned the stale cached feed")
	}
	if hits.Load() != 2 {
		t.Errorf("upstream hits = %d, want 2", hits.Load())
	}
}

func TestFetchFailsOverAndCoolsDown(t *testing.T) {
	clock := newFakeClock()
	var badHits, goodHits atomic.Int64
	bad := statusServer(t, http.StatusInternalServerError, &badHits)
	good := rssServer(t, fixture(t), &goodHits)
	c := newTestClient(t, clock, bad.URL, good.URL)

	f, err := c.Fetch(context.Background(), "gopher")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if f.Origins[0] != good.URL {
		t.Errorf("served by %q, want %q", f.Origins[0], good.URL)
	}
	if badHits.Load() != 1 || goodHits.Load() != 1 {
		t.Errorf("hits bad=%d good=%d, want 1 and 1", badHits.Load(), goodHits.Load())
	}

	c.mu.Lock()
	until, cooling := c.down[bad.URL]
	_, goodCooling := c.down[good.URL]
	c.mu.Unlock()
	if !cooling {
		t.Fatal("failing instance was not put on cooldown")
	}
	if want := clock.Now().Add(5 * time.Minute); !until.Equal(want) {
		t.Errorf("cooldown until %v, want %v", until, want)
	}
	if goodCooling {
		t.Error("healthy instance was put on cooldown")
	}
}

func TestFetchNotFoundDoesNotCoolDown(t *testing.T) {
	clock := newFakeClock()
	missing := statusServer(t, http.StatusNotFound, nil)
	good := rssServer(t, fixture(t), nil)
	c := newTestClient(t, clock, missing.URL, good.URL)

	if _, err := c.Fetch(context.Background(), "gopher"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	c.mu.Lock()
	n := len(c.down)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("len(down) = %d, want 0; a 404 must not cool an instance down", n)
	}
}

func TestFetchSuccessClearsCooldown(t *testing.T) {
	clock := newFakeClock()
	good := rssServer(t, fixture(t), nil)
	c := newTestClient(t, clock, good.URL)

	c.mu.Lock()
	c.down[good.URL] = clock.Now().Add(time.Minute)
	c.mu.Unlock()

	if _, err := c.Fetch(context.Background(), "gopher"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	c.mu.Lock()
	_, cooling := c.down[good.URL]
	c.mu.Unlock()
	if cooling {
		t.Error("a successful fetch did not clear the cooldown")
	}
}

func TestFetchAllInstancesFail(t *testing.T) {
	clock := newFakeClock()
	one := statusServer(t, http.StatusBadGateway, nil)
	two := statusServer(t, http.StatusServiceUnavailable, nil)
	c := newTestClient(t, clock, one.URL, two.URL)

	f, err := c.Fetch(context.Background(), "gopher")
	if err == nil {
		t.Fatal("Fetch succeeded, want an error")
	}
	if f != nil {
		t.Error("Fetch returned a feed alongside an error")
	}
	for _, base := range []string{one.URL, two.URL} {
		if !strings.Contains(err.Error(), base) {
			t.Errorf("error %q does not mention %q", err, base)
		}
	}
}

func TestFetchParseErrorIsReported(t *testing.T) {
	clock := newFakeClock()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("this is not a feed"))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, clock, srv.URL)
	if _, err := c.Fetch(context.Background(), "gopher"); err == nil {
		t.Fatal("Fetch succeeded on a malformed body, want an error")
	} else if !strings.Contains(err.Error(), "parsing feed") {
		t.Errorf("error = %q, want it to mention parsing", err)
	}
}

func TestFetchHonoursCancelledContext(t *testing.T) {
	clock := newFakeClock()
	srv := rssServer(t, fixture(t), nil)
	c := newTestClient(t, clock, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Fetch(ctx, "gopher"); err == nil {
		t.Fatal("Fetch succeeded with a cancelled context")
	}

	c.mu.Lock()
	n := len(c.down)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("len(down) = %d, want 0; a cancelled context must not cool instances down", n)
	}
}

func TestFetchSendsHeaders(t *testing.T) {
	clock := newFakeClock()
	body := fixture(t)

	var mu sync.Mutex
	var gotUA, gotAccept, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		mu.Unlock()
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, clock, srv.URL)
	if _, err := c.Fetch(context.Background(), "GOPHER"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotUA != "twitter-rss/test" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if !strings.Contains(gotAccept, "application/rss+xml") {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotPath != "/gopher/rss" {
		t.Errorf("path = %q, want /gopher/rss", gotPath)
	}
}

func TestOrderRotates(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b", "c")

	want := [][]string{
		{"a", "b", "c"},
		{"b", "c", "a"},
		{"c", "a", "b"},
		{"a", "b", "c"},
	}
	for i, w := range want {
		got := c.order()
		if len(got) != len(w) {
			t.Fatalf("call %d: order() = %v, want %v", i, got, w)
		}
		for j := range w {
			if got[j] != w[j] {
				t.Fatalf("call %d: order() = %v, want %v", i, got, w)
			}
		}
	}
}

func TestOrderSingleInstance(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a")
	for i := range 3 {
		got := c.order()
		if len(got) != 1 || got[0] != "a" {
			t.Fatalf("call %d: order() = %v, want [a]", i, got)
		}
	}
}

func TestOrderDemotesCoolingInstances(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b", "c")

	c.mu.Lock()
	c.down["a"] = clock.Now().Add(instanceCooldown)
	c.mu.Unlock()

	got := c.order()
	want := []string{"b", "c", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order() = %v, want %v; a cooling instance must sort last", got, want)
		}
	}
}

func TestOrderKeepsRelativeOrderOfCoolingInstances(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b", "c", "d")

	c.mu.Lock()
	c.down["a"] = clock.Now().Add(instanceCooldown)
	c.down["c"] = clock.Now().Add(instanceCooldown)
	c.mu.Unlock()

	got := c.order()
	want := []string{"b", "d", "a", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order() = %v, want %v", got, want)
		}
	}
}

func TestOrderCooldownExpiresAfterFiveMinutes(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b")

	c.mu.Lock()
	c.down["a"] = clock.Now().Add(instanceCooldown)
	c.mu.Unlock()

	clock.Advance(instanceCooldown - time.Nanosecond)
	if got := c.order(); got[0] != "b" {
		t.Fatalf("order() = %v just before the cooldown expires, want b first", got)
	}

	clock.Advance(time.Nanosecond)
	got := c.order()
	if got[0] != "b" || got[1] != "a" {
		t.Fatalf("order() = %v at the cooldown boundary, want [b a] from rotation", got)
	}

	got = c.order()
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("order() = %v after the cooldown expired, want a back in rotation", got)
	}
}

func TestOrderAllCoolingStillReturnsEveryInstance(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b", "c")

	c.mu.Lock()
	for _, base := range []string{"a", "b", "c"} {
		c.down[base] = clock.Now().Add(instanceCooldown)
	}
	c.mu.Unlock()

	got := c.order()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("order() = %v, want all three instances", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order() = %v, want %v", got, want)
		}
	}
}

func TestOrderIsRaceFree(t *testing.T) {
	clock := newFakeClock()
	c := newTestClient(t, clock, "a", "b", "c")

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 32 {
				if got := c.order(); len(got) != 3 {
					t.Errorf("order() = %v, want three instances", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}
