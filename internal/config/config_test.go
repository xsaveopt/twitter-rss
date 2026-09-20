package config

import (
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"TWITTER_RSS_ADDR",
		"TWITTER_RSS_BASE_PATH",
		"TWITTER_RSS_CACHE_TTL",
		"TWITTER_RSS_REWRITE_LINKS",
		"TWITTER_RSS_USER_AGENT",
		"TWITTER_RSS_HTTP_TIMEOUT",
		"TWITTER_RSS_NITTER",
	} {
		t.Setenv(key, "")
	}
}

func TestFromEnvDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("TWITTER_RSS_NITTER", "https://nitter.example.com")

	c, err := FromEnv("1.2.3")
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.BasePath != "" {
		t.Errorf("BasePath = %q, want empty", c.BasePath)
	}
	if c.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5m", c.CacheTTL)
	}
	if !c.RewriteLinks {
		t.Error("RewriteLinks = false, want true")
	}
	if c.HTTPTimeout != 15*time.Second {
		t.Errorf("HTTPTimeout = %v, want 15s", c.HTTPTimeout)
	}
	if !strings.Contains(c.UserAgent, "twitter-rss/1.2.3") {
		t.Errorf("UserAgent = %q, want it to carry the version", c.UserAgent)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("TWITTER_RSS_NITTER", "https://one.example.com")
	t.Setenv("TWITTER_RSS_ADDR", "127.0.0.1:9000")
	t.Setenv("TWITTER_RSS_BASE_PATH", "twitter")
	t.Setenv("TWITTER_RSS_CACHE_TTL", "90s")
	t.Setenv("TWITTER_RSS_REWRITE_LINKS", "off")
	t.Setenv("TWITTER_RSS_USER_AGENT", "custom-agent/9")
	t.Setenv("TWITTER_RSS_HTTP_TIMEOUT", "3s")

	c, err := FromEnv("dev")
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if c.Addr != "127.0.0.1:9000" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.BasePath != "/twitter" {
		t.Errorf("BasePath = %q, want /twitter", c.BasePath)
	}
	if c.CacheTTL != 90*time.Second {
		t.Errorf("CacheTTL = %v", c.CacheTTL)
	}
	if c.RewriteLinks {
		t.Error("RewriteLinks = true, want false")
	}
	if c.UserAgent != "custom-agent/9" {
		t.Errorf("UserAgent = %q", c.UserAgent)
	}
	if c.HTTPTimeout != 3*time.Second {
		t.Errorf("HTTPTimeout = %v", c.HTTPTimeout)
	}
}

func TestFromEnvNitterBases(t *testing.T) {
	clearEnv(t)
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"single", "https://one.example.com", []string{"https://one.example.com"}},
		{
			"multiple",
			"https://one.example.com,https://two.example.com",
			[]string{"https://one.example.com", "https://two.example.com"},
		},
		{
			"whitespace trimmed",
			"  https://one.example.com ,\thttps://two.example.com\n",
			[]string{"https://one.example.com", "https://two.example.com"},
		},
		{
			"trailing slashes stripped",
			"https://one.example.com/,https://two.example.com///",
			[]string{"https://one.example.com", "https://two.example.com"},
		},
		{
			"duplicates dropped, order kept",
			"https://two.example.com,https://one.example.com,https://two.example.com/",
			[]string{"https://two.example.com", "https://one.example.com"},
		},
		{
			"empty entries skipped",
			"https://one.example.com,,  ,https://two.example.com",
			[]string{"https://one.example.com", "https://two.example.com"},
		},
		{
			"subpath preserved",
			"https://proxy.example.com/nitter",
			[]string{"https://proxy.example.com/nitter"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TWITTER_RSS_NITTER", tt.env)
			c, err := FromEnv("dev")
			if err != nil {
				t.Fatalf("FromEnv: %v", err)
			}
			if len(c.NitterBases) != len(tt.want) {
				t.Fatalf("NitterBases = %v, want %v", c.NitterBases, tt.want)
			}
			for i := range tt.want {
				if c.NitterBases[i] != tt.want[i] {
					t.Fatalf("NitterBases = %v, want %v", c.NitterBases, tt.want)
				}
			}
		})
	}
}

func TestFromEnvRejectsMissingNitter(t *testing.T) {
	clearEnv(t)
	for _, env := range []string{"", "   ", ",,", " , "} {
		t.Setenv("TWITTER_RSS_NITTER", env)
		if _, err := FromEnv("dev"); err == nil {
			t.Errorf("FromEnv(%q) succeeded, want an error", env)
		} else if !strings.Contains(err.Error(), "TWITTER_RSS_NITTER is required") {
			t.Errorf("FromEnv(%q) error = %q", env, err)
		}
	}
}

func TestFromEnvRejectsInvalidNitterURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("TWITTER_RSS_NITTER", "https://one.example.com,not-a-url")

	_, err := FromEnv("dev")
	if err == nil {
		t.Fatal("FromEnv succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "not-a-url") {
		t.Errorf("error = %q, want it to name the offending value", err)
	}
}

func TestEnvOr(t *testing.T) {
	const key = "TWITTER_RSS_TEST_STRING"

	if got := envOr(key, "fallback"); got != "fallback" {
		t.Errorf("unset: envOr = %q, want fallback", got)
	}

	t.Setenv(key, "")
	if got := envOr(key, "fallback"); got != "fallback" {
		t.Errorf("empty: envOr = %q, want fallback", got)
	}

	t.Setenv(key, "value")
	if got := envOr(key, "fallback"); got != "value" {
		t.Errorf("set: envOr = %q, want value", got)
	}
}

func TestEnvPath(t *testing.T) {
	const key = "TWITTER_RSS_TEST_PATH"

	tests := []struct {
		name string
		env  string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"slash only", "/", ""},
		{"many slashes", "///", ""},
		{"bare segment", "twitter", "/twitter"},
		{"leading slash", "/twitter", "/twitter"},
		{"trailing slash", "twitter/", "/twitter"},
		{"both slashes", "/twitter/", "/twitter"},
		{"nested", "feeds/twitter", "/feeds/twitter"},
		{"nested with both slashes", "/feeds/twitter/", "/feeds/twitter"},
		{"padded with spaces", "  /twitter/  ", "/twitter"},
		{"inner slashes kept", "/a/b/c/", "/a/b/c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(key, tt.env)
			if got := envPath(key); got != tt.want {
				t.Errorf("envPath(%q) = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

func TestEnvBool(t *testing.T) {
	const key = "TWITTER_RSS_TEST_BOOL"

	tests := []struct {
		env  string
		def  bool
		want bool
	}{
		{"1", false, true},
		{"true", false, true},
		{"TRUE", false, true},
		{"Yes", false, true},
		{"on", false, true},
		{"0", true, false},
		{"false", true, false},
		{"FALSE", true, false},
		{"No", true, false},
		{"off", true, false},
		{"", true, true},
		{"", false, false},
		{"maybe", true, true},
		{"maybe", false, false},
		{" true ", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.env+"/"+boolName(tt.def), func(t *testing.T) {
			t.Setenv(key, tt.env)
			if got := envBool(key, tt.def); got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.env, tt.def, got, tt.want)
			}
		})
	}
}

func boolName(b bool) string {
	if b {
		return "default-true"
	}
	return "default-false"
}

func TestEnvDuration(t *testing.T) {
	const key = "TWITTER_RSS_TEST_DURATION"
	def := 7 * time.Second

	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset", "", def},
		{"seconds", "30s", 30 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		{"compound", "1h30m", 90 * time.Minute},
		{"zero", "0s", 0},
		{"negative", "-5s", -5 * time.Second},
		{"unparseable", "soon", def},
		{"missing unit", "30", def},
		{"padded", " 30s ", def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(key, tt.env)
			if got := envDuration(key, def); got != tt.want {
				t.Errorf("envDuration(%q, %v) = %v, want %v", tt.env, def, got, tt.want)
			}
		})
	}
}
