package feed

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/xsaveopt/twitter-rss/internal/config"
	"github.com/xsaveopt/twitter-rss/internal/nitter"
)

type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Items       []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			GUID        string `xml:"guid"`
			Author      string `xml:"author"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func parseRSS(t *testing.T, s string) rss {
	t.Helper()
	var out rss
	if err := xml.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("output is not well-formed XML: %v\n%s", err, s)
	}
	if out.Version != "2.0" {
		t.Errorf("rss version = %q, want 2.0", out.Version)
	}
	return out
}

func at(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parsing %q: %v", value, err)
	}
	return &parsed
}

func builder(rewrite bool) *Builder {
	return NewBuilder(config.Config{RewriteLinks: rewrite})
}

func sampleFeed(t *testing.T) *nitter.Feed {
	t.Helper()
	return &nitter.Feed{
		Handle:  "gopher",
		Title:   "Gopher / @gopher",
		Link:    "https://nitter.example.com/gopher",
		Origins: []string{"https://nitter.example.com"},
		Fetched: *at(t, "2025-09-20T12:00:00Z"),
		Items: []*gofeed.Item{
			{
				Title:           "Newer tweet",
				Link:            "https://nitter.example.com/gopher/status/222#m",
				Description:     "<p>Newer tweet</p>",
				GUID:            "https://nitter.example.com/gopher/status/222#m",
				PublishedParsed: at(t, "2025-09-16T12:30:00Z"),
			},
			{
				Title:           "@gopher: older tweet",
				Link:            "https://nitter.example.com/gopher/status/111#m",
				Description:     "<p>older tweet</p>",
				GUID:            "https://nitter.example.com/gopher/status/111#m",
				PublishedParsed: at(t, "2025-09-15T09:00:00Z"),
			},
		},
	}
}

func TestSingle(t *testing.T) {
	out, err := builder(true).Single(sampleFeed(t))
	if err != nil {
		t.Fatalf("Single: %v", err)
	}

	got := parseRSS(t, out)
	if got.Channel.Title != "@gopher on Twitter" {
		t.Errorf("channel title = %q", got.Channel.Title)
	}
	if got.Channel.Link != "https://x.com/gopher" {
		t.Errorf("channel link = %q, want the rewritten x.com link", got.Channel.Link)
	}
	if got.Channel.Description != "Gopher / @gopher" {
		t.Errorf("channel description = %q", got.Channel.Description)
	}
	if len(got.Channel.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(got.Channel.Items))
	}

	first := got.Channel.Items[0]
	if first.Title != "@gopher: Newer tweet" {
		t.Errorf("item title = %q, want the handle prefixed", first.Title)
	}
	if first.Link != "https://x.com/gopher/status/222" {
		t.Errorf("item link = %q, want the rewritten link without the #m anchor", first.Link)
	}
	if first.Description != "<p>Newer tweet</p>" {
		t.Errorf("item description = %q", first.Description)
	}
	if !strings.Contains(first.Author, "@gopher") {
		t.Errorf("item author = %q", first.Author)
	}
	if first.PubDate == "" {
		t.Error("item pubDate is empty")
	}
}

func TestSingleWithoutRewrite(t *testing.T) {
	out, err := builder(false).Single(sampleFeed(t))
	if err != nil {
		t.Fatalf("Single: %v", err)
	}

	got := parseRSS(t, out)
	if got.Channel.Link != "https://nitter.example.com/gopher" {
		t.Errorf("channel link = %q, want the Nitter link untouched", got.Channel.Link)
	}
	if got.Channel.Items[0].Link != "https://nitter.example.com/gopher/status/222#m" {
		t.Errorf("item link = %q, want the Nitter link untouched including #m", got.Channel.Items[0].Link)
	}
}

func TestSingleFallsBackToXComWhenLinkEmpty(t *testing.T) {
	f := sampleFeed(t)
	f.Link = ""

	out, err := builder(true).Single(f)
	if err != nil {
		t.Fatalf("Single: %v", err)
	}
	if got := parseRSS(t, out).Channel.Link; got != "https://x.com/gopher" {
		t.Errorf("channel link = %q, want the x.com fallback", got)
	}
}

func TestSingleUsesSecondOrigin(t *testing.T) {
	f := sampleFeed(t)
	f.Origins = []string{"http://127.0.0.1:1234", "https://nitter.example.com"}

	out, err := builder(true).Single(f)
	if err != nil {
		t.Fatalf("Single: %v", err)
	}
	if got := parseRSS(t, out).Channel.Items[0].Link; got != "https://x.com/gopher/status/222" {
		t.Errorf("item link = %q, want the second origin rewritten", got)
	}
}

func TestSingleEscapesMarkupInTitles(t *testing.T) {
	f := sampleFeed(t)
	f.Items[0].Title = `Tweet with <b>bold</b> & "quotes"`
	f.Items[0].Description = `<p>Tweet with <b>bold</b> &amp; "quotes"</p>`

	out, err := builder(true).Single(f)
	if err != nil {
		t.Fatalf("Single: %v", err)
	}

	got := parseRSS(t, out)
	if want := `@gopher: Tweet with <b>bold</b> & "quotes"`; got.Channel.Items[0].Title != want {
		t.Errorf("item title = %q, want %q", got.Channel.Items[0].Title, want)
	}
}

func TestCombinedSortsNewestFirstAcrossFeeds(t *testing.T) {
	one := sampleFeed(t)
	two := &nitter.Feed{
		Handle:  "rustlang",
		Title:   "Rust / @rustlang",
		Link:    "https://nitter.example.com/rustlang",
		Origins: []string{"https://nitter.example.com"},
		Fetched: *at(t, "2025-09-20T12:00:00Z"),
		Items: []*gofeed.Item{
			{
				Title:           "Newest of all",
				Link:            "https://nitter.example.com/rustlang/status/999#m",
				GUID:            "https://nitter.example.com/rustlang/status/999#m",
				PublishedParsed: at(t, "2025-09-17T08:00:00Z"),
			},
			{
				Title:           "Oldest of all",
				Link:            "https://nitter.example.com/rustlang/status/888#m",
				GUID:            "https://nitter.example.com/rustlang/status/888#m",
				PublishedParsed: at(t, "2025-09-14T08:00:00Z"),
			},
		},
	}

	out, err := builder(true).Combined("twitter-rss", []*nitter.Feed{one, two})
	if err != nil {
		t.Fatalf("Combined: %v", err)
	}

	got := parseRSS(t, out)
	if got.Channel.Title != "Tracked Twitter accounts" {
		t.Errorf("channel title = %q", got.Channel.Title)
	}
	if got.Channel.Link != "https://x.com" {
		t.Errorf("channel link = %q", got.Channel.Link)
	}

	want := []string{
		"@rustlang: Newest of all",
		"@gopher: Newer tweet",
		"@gopher: older tweet",
		"@rustlang: Oldest of all",
	}
	if len(got.Channel.Items) != len(want) {
		t.Fatalf("got %d items, want %d", len(got.Channel.Items), len(want))
	}
	for i, w := range want {
		if got.Channel.Items[i].Title != w {
			t.Errorf("item %d title = %q, want %q", i, got.Channel.Items[i].Title, w)
		}
	}
}

func TestCombinedWithNoFeeds(t *testing.T) {
	out, err := builder(true).Combined("twitter-rss", nil)
	if err != nil {
		t.Fatalf("Combined: %v", err)
	}
	if got := parseRSS(t, out); len(got.Channel.Items) != 0 {
		t.Errorf("got %d items, want 0", len(got.Channel.Items))
	}
}

func TestItemsFallBackForMissingFields(t *testing.T) {
	before := time.Now()
	f := &nitter.Feed{
		Handle:  "gopher",
		Origins: []string{"https://nitter.example.com"},
		Items: []*gofeed.Item{
			{
				Title: "No guid, no date",
				Link:  "https://nitter.example.com/gopher/status/1#m",
			},
		},
	}

	items := builder(true).items(f)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Id != "https://x.com/gopher/status/1" {
		t.Errorf("Id = %q, want the rewritten link as the fallback id", items[0].Id)
	}
	if items[0].Created.Before(before) {
		t.Errorf("Created = %v, want a time at or after %v", items[0].Created, before)
	}
}

func TestItemsKeepExistingGUID(t *testing.T) {
	f := sampleFeed(t)
	f.Items[0].GUID = "tag:nitter.example.com,2025:222"

	items := builder(true).items(f)
	if items[0].Id != "tag:nitter.example.com,2025:222" {
		t.Errorf("Id = %q, want the upstream GUID kept", items[0].Id)
	}
}

func TestItemsEmptyFeed(t *testing.T) {
	items := builder(true).items(&nitter.Feed{Handle: "gopher"})
	if items == nil {
		t.Fatal("items returned nil, want an empty slice")
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want 0", len(items))
	}
}

func TestRewrite(t *testing.T) {
	origins := []string{"http://127.0.0.1:9999", "https://nitter.example.com"}

	tests := []struct {
		name    string
		rewrite bool
		link    string
		want    string
	}{
		{"first origin", true, "http://127.0.0.1:9999/gopher/status/1#m", "https://x.com/gopher/status/1"},
		{"second origin", true, "https://nitter.example.com/gopher", "https://x.com/gopher"},
		{"anchor stripped", true, "https://nitter.example.com/gopher#m", "https://x.com/gopher"},
		{"anchor stripped on unknown host", true, "https://elsewhere.example.com/x#m", "https://elsewhere.example.com/x"},
		{"unknown origin kept", true, "https://elsewhere.example.com/gopher", "https://elsewhere.example.com/gopher"},
		{"empty stays empty", true, "", ""},
		{"disabled keeps link", false, "https://nitter.example.com/gopher/status/1#m", "https://nitter.example.com/gopher/status/1#m"},
		{"disabled keeps empty", false, "", ""},
		{"origin with no path", true, "https://nitter.example.com", "https://x.com"},
		{"only the first match applies", true, "http://127.0.0.1:9999/a/https://nitter.example.com/b", "https://x.com/a/https://nitter.example.com/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := builder(tt.rewrite).rewrite(origins, tt.link); got != tt.want {
				t.Errorf("rewrite(%q) = %q, want %q", tt.link, got, tt.want)
			}
		})
	}
}

func TestRewriteWithNoOrigins(t *testing.T) {
	got := builder(true).rewrite(nil, "https://nitter.example.com/gopher#m")
	if got != "https://nitter.example.com/gopher" {
		t.Errorf("rewrite = %q, want only the anchor stripped", got)
	}
}

func TestPrefixHandle(t *testing.T) {
	tests := []struct {
		name   string
		handle string
		title  string
		want   string
	}{
		{"adds prefix", "gopher", "Hello", "@gopher: Hello"},
		{"already prefixed", "gopher", "@gopher: Hello", "@gopher: Hello"},
		{"empty title", "gopher", "", "@gopher: "},
		{"other handle prefixed", "gopher", "@rustlang: Hello", "@gopher: @rustlang: Hello"},
		{"case sensitive", "gopher", "@Gopher: Hello", "@gopher: @Gopher: Hello"},
		{"missing space", "gopher", "@gopher:Hello", "@gopher: @gopher:Hello"},
		{"handle only", "gopher", "@gopher", "@gopher: @gopher"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixHandle(tt.handle, tt.title); got != tt.want {
				t.Errorf("prefixHandle(%q, %q) = %q, want %q", tt.handle, tt.title, got, tt.want)
			}
		})
	}
}

func TestTitleOrDefault(t *testing.T) {
	if got := titleOrDefault(&nitter.Feed{Handle: "gopher", Title: "Gopher / @gopher"}); got != "Gopher / @gopher" {
		t.Errorf("titleOrDefault = %q, want the feed title", got)
	}
	if got := titleOrDefault(&nitter.Feed{Handle: "gopher"}); got != "Tweets from @gopher" {
		t.Errorf("titleOrDefault = %q, want the default", got)
	}
}
