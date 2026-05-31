package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	NitterBase   string
	CacheTTL     time.Duration
	RewriteLinks bool
	UserAgent    string
	HTTPTimeout  time.Duration
}

func FromEnv() (Config, error) {
	c := Config{
		Addr:         envOr("TWITTER_RSS_ADDR", ":8080"),
		NitterBase:   strings.TrimRight(os.Getenv("TWITTER_RSS_NITTER"), "/"),
		CacheTTL:     envDuration("TWITTER_RSS_CACHE_TTL", 5*time.Minute),
		RewriteLinks: envBool("TWITTER_RSS_REWRITE_LINKS", true),
		UserAgent:    envOr("TWITTER_RSS_USER_AGENT", "twitter-rss/"+version+" (+https://github.com/sratabix/twitter-rss)"),
		HTTPTimeout:  envDuration("TWITTER_RSS_HTTP_TIMEOUT", 15*time.Second),
	}

	if c.NitterBase == "" {
		return c, fmt.Errorf("TWITTER_RSS_NITTER is required (e.g. https://nitter.example.com)")
	}
	if _, err := url.ParseRequestURI(c.NitterBase); err != nil {
		return c, fmt.Errorf("TWITTER_RSS_NITTER is not a valid URL: %w", err)
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
