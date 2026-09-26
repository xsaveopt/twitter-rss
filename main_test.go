package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const runMainEnv = "TWITTER_RSS_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return addr
}

type mainProcess struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	output strings.Builder
}

func (p *mainProcess) log() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.String()
}

func startMain(t *testing.T, env ...string) *mainProcess {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestMain")
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = io.Discard

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	p := &mainProcess{cmd: cmd}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			p.mu.Lock()
			p.output.WriteString(scanner.Text())
			p.output.WriteString("\n")
			p.mu.Unlock()
		}
	}()

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-drained
	})
	return p
}

func waitForLog(t *testing.T, p *mainProcess, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.log(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in the log:\n%s", want, p.log())
}

func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response from %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

func waitForHealth(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && string(body) == "up" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to come up", url)
}

func TestMainExitsWhenNitterIsMissing(t *testing.T) {
	p := startMain(t, "TWITTER_RSS_NITTER=", "TWITTER_RSS_ADDR="+freeAddr(t))

	err := p.cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the binary exited with %v, want a failing exit status", err)
	}
	if exit.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exit.ExitCode())
	}
	waitForLog(t, p, "configuration error")
	waitForLog(t, p, "TWITTER_RSS_NITTER is required")
}

func TestMainServesAndShutsDownGracefully(t *testing.T) {
	const upstream = "https://nitter.example.invalid"
	addr := freeAddr(t)
	p := startMain(t,
		"TWITTER_RSS_NITTER="+upstream,
		"TWITTER_RSS_ADDR="+addr,
		"TWITTER_RSS_BASE_PATH=feeds/twitter",
	)

	base := "http://" + addr
	waitForHealth(t, base+"/feeds/twitter/health")
	waitForLog(t, p, "listening")
	waitForLog(t, p, upstream)
	waitForLog(t, p, "version=dev")

	for _, path := range []string{"/", "/feeds/twitter/", "/feeds/twitter/health"} {
		status, body := httpGet(t, base+path)
		if status != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, status)
		}
		if path == "/feeds/twitter/" && !strings.Contains(body, "/feeds/twitter/u/{handle}") {
			t.Errorf("the index under the base path does not advertise it:\n%s", body)
		}
	}

	if status, _ := httpGet(t, base+"/health"); status != http.StatusNotFound {
		t.Errorf("GET /health at the root with a base path set = %d, want 404", status)
	}

	if status, _ := httpGet(t, base+"/u/go-pher"); status != http.StatusBadRequest {
		t.Errorf("GET an invalid handle = %d, want 400", status)
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the binary: %v", err)
	}
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("the binary did not exit cleanly on SIGTERM: %v\n%s", err, p.log())
	}
	waitForLog(t, p, "shutting down")
}

func TestMainReportsAnUnusableAddress(t *testing.T) {
	addr := freeAddr(t)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("occupying %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	p := startMain(t, "TWITTER_RSS_NITTER=https://nitter.example.invalid", "TWITTER_RSS_ADDR="+addr)
	waitForLog(t, p, "server failed")
	_ = p.cmd.Wait()
}

func healthEnv(t *testing.T, addr, basePath string) {
	t.Helper()
	t.Setenv("TWITTER_RSS_NITTER", "https://nitter.example.invalid")
	t.Setenv("TWITTER_RSS_ADDR", addr)
	t.Setenv("TWITTER_RSS_BASE_PATH", basePath)
}

func healthServer(t *testing.T, path string, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("splitting the test server address: %v", err)
	}
	return port
}

func TestRunHealthcheckSucceedsWhenHealthy(t *testing.T) {
	port := healthServer(t, "/health", http.StatusOK)
	healthEnv(t, ":"+port, "")

	if got := runHealthcheck(); got != 0 {
		t.Errorf("runHealthcheck() = %d, want 0", got)
	}
}

func TestRunHealthcheckUsesBasePath(t *testing.T) {
	port := healthServer(t, "/feeds/twitter/health", http.StatusOK)
	healthEnv(t, "0.0.0.0:"+port, "/feeds/twitter/")

	if got := runHealthcheck(); got != 0 {
		t.Errorf("runHealthcheck() = %d, want 0 when the health route lives under the base path", got)
	}
}

func TestRunHealthcheckFailsOnDegradedStatus(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			port := healthServer(t, "/health", status)
			healthEnv(t, ":"+port, "")

			if got := runHealthcheck(); got != 1 {
				t.Errorf("runHealthcheck() = %d with a %d response, want 1", got, status)
			}
		})
	}
}

func TestRunHealthcheckFailsWhenBasePathDiffers(t *testing.T) {
	port := healthServer(t, "/health", http.StatusOK)
	healthEnv(t, ":"+port, "twitter")

	if got := runHealthcheck(); got != 1 {
		t.Errorf("runHealthcheck() = %d, want 1 when nothing answers under the base path", got)
	}
}

func TestRunHealthcheckFailsWhenNothingListens(t *testing.T) {
	_, port, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatalf("splitting the free address: %v", err)
	}
	healthEnv(t, ":"+port, "")

	if got := runHealthcheck(); got != 1 {
		t.Errorf("runHealthcheck() = %d, want 1", got)
	}
}

func TestRunHealthcheckFailsOnBadConfig(t *testing.T) {
	tests := []struct {
		name   string
		nitter string
		addr   string
	}{
		{"missing nitter", "", ":8080"},
		{"address without port", "https://nitter.example.invalid", "8080"},
		{"address with too many colons", "https://nitter.example.invalid", "a:b:c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			healthEnv(t, tt.addr, "")
			t.Setenv("TWITTER_RSS_NITTER", tt.nitter)

			if got := runHealthcheck(); got != 1 {
				t.Errorf("runHealthcheck() = %d, want 1", got)
			}
		})
	}
}

func TestRunHealthcheckTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("splitting the test server address: %v", err)
	}
	healthEnv(t, ":"+port, "")

	start := time.Now()
	got := runHealthcheck()
	elapsed := time.Since(start)
	if got != 1 {
		t.Errorf("runHealthcheck() = %d against a hanging server, want 1", got)
	}
	if elapsed > 6*time.Second {
		t.Errorf("runHealthcheck() took %v, want it bounded by its own timeout", elapsed)
	}
}
