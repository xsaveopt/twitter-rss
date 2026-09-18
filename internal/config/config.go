package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	BasePath     string
	NitterBases  []string
	CacheTTL     time.Duration
	RewriteLinks bool
	UserAgent    string
	HTTPTimeout  time.Duration
}

func FromEnv(version string) (Config, error) {
	c := Config{
		Addr:         envOr("TWITTER_RSS_ADDR", ":8080"),
		BasePath:     envPath("TWITTER_RSS_BASE_PATH"),
		CacheTTL:     envDuration("TWITTER_RSS_CACHE_TTL", 5*time.Minute),
		RewriteLinks: envBool("TWITTER_RSS_REWRITE_LINKS", true),
		UserAgent:    envOr("TWITTER_RSS_USER_AGENT", "twitter-rss/"+version+" (+https://github.com/xsaveopt/twitter-rss)"),
		HTTPTimeout:  envDuration("TWITTER_RSS_HTTP_TIMEOUT", 15*time.Second),
	}

	seen := make(map[string]bool)
	for raw := range strings.SplitSeq(os.Getenv("TWITTER_RSS_NITTER"), ",") {
		base := strings.TrimRight(strings.TrimSpace(raw), "/")
		if base == "" || seen[base] {
			continue
		}
		if _, err := url.ParseRequestURI(base); err != nil {
			return c, fmt.Errorf("TWITTER_RSS_NITTER contains an invalid URL %q: %w", base, err)
		}
		seen[base] = true
		c.NitterBases = append(c.NitterBases, base)
	}

	if len(c.NitterBases) == 0 {
		return c, fmt.Errorf("TWITTER_RSS_NITTER is required (e.g. https://nitter-one.example.com,https://nitter-two.example.com)")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envPath(key string) string {
	p := strings.Trim(strings.TrimSpace(os.Getenv(key)), "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
