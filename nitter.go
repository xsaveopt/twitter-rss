package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
)

var validHandle = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

func ValidHandle(s string) bool { return validHandle.MatchString(s) }

type Feed struct {
	Handle  string
	Title   string
	Link    string
	Items   []*gofeed.Item
	Fetched time.Time
}

type Client struct {
	base   string
	ua     string
	http   *http.Client
	parser *gofeed.Parser

	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]*Feed
}

func NewClient(cfg Config) *Client {
	return &Client{
		base:   cfg.NitterBase,
		ua:     cfg.UserAgent,
		http:   &http.Client{Timeout: cfg.HTTPTimeout},
		parser: gofeed.NewParser(),
		ttl:    cfg.CacheTTL,
		cache:  make(map[string]*Feed),
	}
}

func (c *Client) Fetch(ctx context.Context, handle string) (*Feed, error) {
	handle = strings.ToLower(handle)

	c.mu.Lock()
	if f, ok := c.cache[handle]; ok && time.Since(f.Fetched) < c.ttl {
		c.mu.Unlock()
		return f, nil
	}
	c.mu.Unlock()

	f, err := c.fetch(ctx, handle)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[handle] = f
	c.mu.Unlock()
	return f, nil
}

func (c *Client) fetch(ctx context.Context, handle string) (*Feed, error) {
	u := fmt.Sprintf("%s/%s/rss", c.base, handle)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting nitter: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("nitter returned %s for @%s: %s", resp.Status, handle, strings.TrimSpace(string(body)))
	}

	parsed, err := c.parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing nitter feed for @%s: %w", handle, err)
	}

	f := &Feed{
		Handle:  handle,
		Title:   parsed.Title,
		Items:   parsed.Items,
		Fetched: time.Now(),
	}
	if parsed.Link != "" {
		f.Link = parsed.Link
	}
	return f, nil
}
