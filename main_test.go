package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
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
			if resp.StatusCode == http.StatusOK && string(body) == "ok" {
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
	waitForHealth(t, base+"/healthz")
	waitForLog(t, p, "listening")
	waitForLog(t, p, upstream)
	waitForLog(t, p, "version=dev")

	for _, path := range []string{"/", "/healthz", "/feeds/twitter/", "/feeds/twitter/healthz"} {
		status, body := httpGet(t, base+path)
		if status != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, status)
		}
		if path == "/feeds/twitter/" && !strings.Contains(body, "/feeds/twitter/u/{handle}") {
			t.Errorf("the index under the base path does not advertise it:\n%s", body)
		}
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
