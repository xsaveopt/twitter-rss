package nitter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/xsaveopt/twitter-rss/internal/config"
)

const instanceCooldown = 5 * time.Minute

var validHandle = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

func ValidHandle(s string) bool { return validHandle.MatchString(s) }

type Feed struct {
	Handle  string
	Title   string
	Link    string
	Origins []string
	Items   []*gofeed.Item
	Fetched time.Time
}

type Client struct {
	bases  []string
	ua     string
	http   *http.Client
	parser *gofeed.Parser
	next   atomic.Uint64
	now    func() time.Time

	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]*Feed
	down  map[string]time.Time
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		bases:  cfg.NitterBases,
		ua:     cfg.UserAgent,
		http:   &http.Client{Timeout: cfg.HTTPTimeout},
		parser: gofeed.NewParser(),
		now:    time.Now,
		ttl:    cfg.CacheTTL,
		cache:  make(map[string]*Feed),
		down:   make(map[string]time.Time),
	}
}

func (c *Client) Fetch(ctx context.Context, handle string) (*Feed, error) {
	handle = strings.ToLower(handle)

	c.mu.Lock()
	if f, ok := c.cache[handle]; ok && c.now().Sub(f.Fetched) < c.ttl {
		c.mu.Unlock()
		return f, nil
	}
	c.mu.Unlock()

	var errs []error
	for _, base := range c.order() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := c.fetch(ctx, base, handle)
		if err == nil {
			c.mu.Lock()
			delete(c.down, base)
			c.cache[handle] = f
			c.mu.Unlock()
			return f, nil
		}
		errs = append(errs, err)
		var nf notFoundError
		if !errors.As(err, &nf) && ctx.Err() == nil {
			c.mu.Lock()
			c.down[base] = c.now().Add(instanceCooldown)
			c.mu.Unlock()
		}
	}
	return nil, errors.Join(errs...)
}

func (c *Client) order() []string {
	n := len(c.bases)
	start := int(c.next.Add(1)-1) % n

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	up := make([]string, 0, n)
	var cooling []string
	for i := range n {
		base := c.bases[(start+i)%n]
		if until, ok := c.down[base]; ok && now.Before(until) {
			cooling = append(cooling, base)
			continue
		}
		up = append(up, base)
	}
	return append(up, cooling...)
}

type notFoundError struct{ error }

func (c *Client) fetch(ctx context.Context, base, handle string) (*Feed, error) {
	u := fmt.Sprintf("%s/%s/rss", base, handle)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err := fmt.Errorf("%s returned %s for @%s: %s", base, resp.Status, handle, strings.TrimSpace(string(body)))
		if resp.StatusCode == http.StatusNotFound {
			return nil, notFoundError{err}
		}
		return nil, err
	}

	parsed, err := c.parser.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing feed from %s for @%s: %w", base, handle, err)
	}

	f := &Feed{
		Handle:  handle,
		Title:   parsed.Title,
		Link:    parsed.Link,
		Origins: []string{base},
		Items:   parsed.Items,
		Fetched: c.now(),
	}
	if p, err := url.Parse(parsed.Link); err == nil && p.Scheme != "" && p.Host != "" {
		if origin := p.Scheme + "://" + p.Host; origin != base {
			f.Origins = append(f.Origins, origin)
		}
	}
	return f, nil
}
